package biz

import (
	"context"
	"hash/fnv"
	"math/rand/v2"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm/auth"
)

// traceStickyLRUSize is the default LRU cache size for trace-to-key mappings.
const traceStickyLRUSize = 1024

type channelAPIKeyContextProvider struct {
	inner auth.APIKeyProvider
}

func NewChannelAPIKeyContextProvider(inner auth.APIKeyProvider) auth.APIKeyProvider {
	return &channelAPIKeyContextProvider{inner: inner}
}

func (p *channelAPIKeyContextProvider) Get(ctx context.Context) string {
	key := p.inner.Get(ctx)
	if key != "" {
		contexts.WithChannelAPIKey(ctx, key)
	}
	return key
}

// TraceStickyKeyProvider selects an API key deterministically per traceID (if present),
// using cached enabled keys from the channel snapshot.
//
// An LRU cache remembers previous traceID→key selections so that, as long as
// the previously chosen key is still enabled, the same key is returned even when
// the enabled-key set changes (e.g. a new key is added). This improves sticky
// stability compared to pure rendezvous hashing alone.
//
//nolint:revive // exported for use in transformers via interface.
type TraceStickyKeyProvider struct {
	channel *Channel
	cache   *lru.Cache[string, string]
}

func NewTraceStickyKeyProvider(channel *Channel) *TraceStickyKeyProvider {
	cache, _ := lru.New[string, string](traceStickyLRUSize)

	return &TraceStickyKeyProvider{
		channel: channel,
		cache:   cache,
	}
}

// modelFilteredKeyProvider keeps the single-key fast path while still rejecting a key
// that explicitly does not support the upstream model selected for this attempt.
type modelFilteredKeyProvider struct {
	channel *Channel
	keys    []string
}

func newModelFilteredKeyProvider(channel *Channel, keys []string) *modelFilteredKeyProvider {
	return &modelFilteredKeyProvider{channel: channel, keys: keys}
}

func (p *modelFilteredKeyProvider) Get(ctx context.Context) string {
	keys := p.channel.filterAPIKeysForRequest(ctx, p.keys)
	if len(keys) == 0 {
		return ""
	}

	return keys[0]
}

func (c *Channel) enabledAPIKeysForRequest(ctx context.Context) []string {
	if c == nil {
		return nil
	}

	return c.filterAPIKeysForRequest(ctx, c.cachedEnabledAPIKeys)
}

func (c *Channel) filterAPIKeysForRequest(ctx context.Context, keys []string) []string {
	if c == nil || len(keys) == 0 || len(c.cachedAPIKeyModels) == 0 {
		return keys
	}

	model, ok := contexts.GetChannelRequestModel(ctx)
	if !ok || model == "" {
		return keys
	}

	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		models, explicit := c.cachedAPIKeyModels[key]
		if !explicit || lo.Contains(models, model) {
			filtered = append(filtered, key)
		}
	}

	return filtered
}

func (p *TraceStickyKeyProvider) Get(ctx context.Context) string {
	enabled := p.channel.enabledAPIKeysForRequest(ctx)
	if len(enabled) == 0 {
		if _, explicit := contexts.GetChannelRequestModel(ctx); explicit && len(p.channel.cachedAPIKeyModels) > 0 {
			return ""
		}
		if len(p.channel.Credentials.APIKeys) == 0 {
			return ""
		}

		return p.channel.Credentials.APIKeys[0]
	}

	if len(enabled) == 1 {
		return enabled[0]
	}

	var selectedKey string

	if trace, ok := contexts.GetTrace(ctx); ok && trace != nil {
		model, _ := contexts.GetChannelRequestModel(ctx)
		cacheKey := trace.TraceID + "\x00" + model
		if cached, ok := p.cache.Get(cacheKey); ok && lo.Contains(enabled, cached) {
			selectedKey = cached
		} else {
			selectedKey = rendezvousSelect(enabled, cacheKey)
			p.cache.Add(cacheKey, selectedKey)
		}

		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "Trace sticky key selected",
				log.String("trace_id", trace.TraceID),
				log.String("key_prefix", safeAPIKeyPrefix(selectedKey)),
			)
		}
	} else {
		//nolint:gosec // not a security issue, just a random selection.
		selectedKey = enabled[rand.IntN(len(enabled))]
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "Random key selected",
				log.String("key_prefix", safeAPIKeyPrefix(selectedKey)),
			)
		}
	}

	contexts.WithChannelAPIKey(ctx, selectedKey)

	return selectedKey
}

// rendezvousSelect picks a key using Highest Random Weight (Rendezvous) hashing.
// This is stable when the key set changes (minimal remapping compared to modulo).
func rendezvousSelect(keys []string, seed string) string {
	bestKey := keys[0]
	bestScore := hashAPIKey(seed + "|" + bestKey)

	for i := 1; i < len(keys); i++ {
		k := keys[i]

		s := hashAPIKey(seed + "|" + k)
		if s > bestScore {
			bestScore = s
			bestKey = k
		}
	}

	return bestKey
}

func hashAPIKey(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))

	return h.Sum64()
}

func safeAPIKeyPrefix(key string) string {
	if len(key) >= 2 {
		return key[:2]
	}

	return key
}
