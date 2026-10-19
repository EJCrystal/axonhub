package biz

import (
	"context"

	"github.com/looplj/axonhub/internal/log"
)

// ChannelAPIKeyRotator lets the channel service tell the intelligence check that
// a credential was replaced in place.
//
// The intelligence check stores the exact key each target exercises, because the
// key is what identifies the target: a channel may be configured once per key.
// Replacing a key on the channel therefore leaves the stored target pointing at a
// credential that no longer exists, and the next run fails to build the channel.
// Rotating it here keeps the two in step without an operator re-adding the row.
//
// The interface lives in biz to avoid an import cycle with orchestrator.
type ChannelAPIKeyRotator interface {
	RotateAPIKeys(ctx context.Context, channelID int, replacements map[string]string)
}

// SetChannelAPIKeyRotator wires the optional rotation hook. Called once at
// start-up from the orchestrator fx module.
func (svc *ChannelService) SetChannelAPIKeyRotator(rotator ChannelAPIKeyRotator) {
	svc.apiKeyRotator = rotator
}

// rotateAPIKeys applies a key replacement to whatever stores keys per channel.
// Safe to call when no rotator has been registered.
func (svc *ChannelService) rotateAPIKeys(ctx context.Context, channelID int, replacements map[string]string) {
	if svc.apiKeyRotator == nil || len(replacements) == 0 {
		return
	}

	svc.apiKeyRotator.RotateAPIKeys(ctx, channelID, replacements)

	log.Info(ctx, "channel api keys rotated",
		log.Int("channel_id", channelID),
		log.Int("count", len(replacements)))
}

// rotatedAPIKeys maps each key that disappeared from the channel to the key that
// took its place.
//
// Replacement is positional: the channel editor rewrites the key at the slot the
// operator edited, and the remark beside it stays put, so position is the only
// signal that says which new key stands in for which old one.
//
// Two shapes are handled. A same-length edit is a one-for-one swap. A shrinking
// edit collapses the keys into the shorter list, which is what happens when two
// credentials of one provider are merged into a single account key; the removed
// slots then point at the key that now occupies their position, clamped to the
// new last slot.
//
// A key that still exists after the edit is a reorder, not a replacement, and a
// growing edit only added keys. Neither is guessed at, because rewriting a
// target onto the wrong credential would silently test something else.
func rotatedAPIKeys(oldKeys, newKeys []string) map[string]string {
	if len(oldKeys) == 0 || len(newKeys) == 0 || len(newKeys) > len(oldKeys) {
		return nil
	}

	remaining := make(map[string]struct{}, len(newKeys))
	for _, key := range newKeys {
		remaining[key] = struct{}{}
	}

	replacements := make(map[string]string, len(oldKeys))
	for index, oldKey := range oldKeys {
		if oldKey == "" {
			continue
		}
		// The old key is still on the channel, so this is a reorder.
		if _, ok := remaining[oldKey]; ok {
			continue
		}

		successor := newKeys[min(index, len(newKeys)-1)]
		if successor == "" || successor == oldKey {
			continue
		}

		replacements[oldKey] = successor
	}

	return replacements
}
