package orchestrator

import (
	"context"
	"strings"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
)

// RotateAPIKeys moves every target that pinned a replaced credential onto the
// key that took its place.
//
// A target identifies itself by channel and key, because one channel can be
// configured once per key with its own model and thinking level. Replacing the
// key on the channel therefore has to rewrite the target rather than drop it:
// dropping the row would throw away the model, effort, timeout and benchmark the
// operator chose, and keeping the old key would make the run fail to find the
// channel credential.
func (s *IntelligenceService) RotateAPIKeys(ctx context.Context, channelID int, replacements map[string]string) {
	if len(replacements) == 0 {
		return
	}

	// The update runs from a channel mutation, which carries a principal, but the
	// configuration is a system value and the write must succeed whatever scope
	// the editor had.
	rotateCtx := authz.WithSystemBypass(context.WithoutCancel(ctx), "intelligence-check-key-rotation")

	config, err := s.intelligenceConfigExact(rotateCtx)
	if err != nil {
		log.Warn(rotateCtx, "intelligence check: failed to read configuration for key rotation", log.Cause(err))
		return
	}
	// No stored configuration means no target pinned a key in the first place.
	if config == nil {
		return
	}

	rotated := false
	seen := make(map[intelligenceTargetKey]struct{}, len(config.Targets))
	targets := make([]objects.IntelligenceTarget, 0, len(config.Targets))

	for _, target := range config.Targets {
		key := strings.TrimSpace(target.APIKey)
		if target.ChannelID == channelID {
			if replacement, ok := replacements[key]; ok {
				target.APIKey = replacement
				rotated = true
			}
		}

		// A rotation can make two rows collide, which the save path rejects. The
		// first row keeps the slot and the second is dropped instead of failing the
		// channel update: the channel credential is the source of truth, and a
		// duplicate target would run the same key twice.
		identity := intelligenceTargetKey{ChannelID: target.ChannelID, APIKey: strings.TrimSpace(target.APIKey)}
		if _, ok := seen[identity]; ok {
			log.Warn(rotateCtx, "intelligence check: dropped a duplicate target after key rotation",
				log.Int("channel_id", target.ChannelID),
				log.String("key", maskIntelligenceTargetKey(identity.APIKey)))
			continue
		}
		seen[identity] = struct{}{}

		targets = append(targets, target)
	}

	if !rotated {
		return
	}

	config.Targets = targets
	if err := s.SetIntelligenceConfig(rotateCtx, config); err != nil {
		log.Warn(rotateCtx, "intelligence check: failed to save rotated keys", log.Cause(err))
		return
	}

	log.Info(rotateCtx, "intelligence check: aligned targets with rotated channel keys",
		log.Int("channel_id", channelID),
		log.Int("rotated", len(replacements)),
		log.Int("targets", len(targets)))
}
