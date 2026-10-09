package gql

import (
	"context"
	"fmt"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
)

// intelligenceConfigPayload renders the stored configuration with the channel
// names resolved, so the settings screen does not need a second round trip.
func (r *mutationResolver) intelligenceConfigPayload(ctx context.Context) (*IntelligenceConfig, error) {
	config, err := r.intelligenceService.IntelligenceConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load intelligence config: %w", err)
	}

	targets := make([]*IntelligenceTarget, 0, len(config.Targets))
	for _, target := range config.Targets {
		name := ""
		if channel, err := r.channelService.GetChannel(ctx, target.ChannelID); err == nil {
			name = channel.Name
		}

		entry := &IntelligenceTarget{
			ChannelID:       objects.GUID{Type: ent.TypeChannel, ID: target.ChannelID},
			ChannelName:     name,
			ModelID:         target.ModelID,
			ReasoningEffort: intelStringOrNil(target.ReasoningEffort),
			TimeoutMinutes:  intelIntOrNil(target.TimeoutMinutes),
		}
		if target.APIKey != "" {
			entry.APIKey = lo.ToPtr(target.APIKey)
		}
		targets = append(targets, entry)
	}

	return &IntelligenceConfig{
		Enabled:         config.Enabled,
		IntervalMinutes: config.IntervalMinutes,
		Targets:         targets,
	}, nil
}

// intelStringOrNil keeps an absent optional string represented as null rather
// than an empty string, so the UI can tell "not set" from "set to empty".
func intelStringOrNil(value string) *string {
	if value == "" {
		return nil
	}

	return lo.ToPtr(value)
}

// intelIntOrNil keeps an absent number represented as null rather than zero, so
// the UI can tell "not set" from "set to the minimum".
func intelIntOrNil(value int) *int {
	if value == 0 {
		return nil
	}

	return lo.ToPtr(value)
}

// intelHTMLOrNil keeps an absent source represented as null rather than an
// empty string, so the UI can tell "nothing to preview" from an empty document.
func intelHTMLOrNil(html string) *string {
	if html == "" {
		return nil
	}

	return lo.ToPtr(html)
}
