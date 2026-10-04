package gql

import (
	"context"
	"fmt"

	"github.com/samber/lo"

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

		targets = append(targets, &IntelligenceTarget{
			ChannelID:   objects.GUID{Type: "channel", ID: target.ChannelID},
			ChannelName: name,
			ModelID:     target.ModelID,
		})
	}

	return &IntelligenceConfig{
		Enabled:         config.Enabled,
		IntervalMinutes: config.IntervalMinutes,
		Targets:         targets,
	}, nil
}

// intelHTMLOrNil keeps an absent source represented as null rather than an
// empty string, so the UI can tell "nothing to preview" from an empty document.
func intelHTMLOrNil(html string) *string {
	if html == "" {
		return nil
	}

	return lo.ToPtr(html)
}
