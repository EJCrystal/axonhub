package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/intelligencerun"
	"github.com/looplj/axonhub/internal/ent/system"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
)

// OnChannelsDeleted removes intelligence targets and history for channels that
// were just deleted. It runs inside the channel deletion transaction so the
// configuration and history disappear atomically with the channel.
func (s *IntelligenceService) OnChannelsDeleted(ctx context.Context, channelIDs []int) error {
	if len(channelIDs) == 0 {
		return nil
	}

	client := ent.FromContext(ctx)
	if client == nil {
		client = s.ent
	}

	if err := deleteIntelligenceRuns(ctx, client, channelIDs); err != nil {
		return err
	}

	return s.removeIntelligenceTargets(ctx, client, channelIDs)
}

// pruneDeletedChannelState removes intelligence targets and history whose
// channel no longer exists. It closes the gap left by channels deleted before
// the deletion hook existed, and by deletions performed outside the channel
// service.
func (s *IntelligenceService) pruneDeletedChannelState(ctx context.Context) error {
	ctx = authz.WithSystemBypass(ctx, "intelligence-check-prune-deleted-channels")

	client := s.ent
	config, err := intelligenceConfigFromClient(ctx, client)
	if err != nil {
		return err
	}

	candidates := make(map[int]struct{})
	if config != nil {
		for _, target := range config.Targets {
			candidates[target.ChannelID] = struct{}{}
		}
	}

	runs, err := client.IntelligenceRun.Query().Select(intelligencerun.FieldChannelID).All(ctx)
	if err != nil {
		return fmt.Errorf("failed to list intelligence run channels: %w", err)
	}
	for _, run := range runs {
		candidates[run.ChannelID] = struct{}{}
	}
	if len(candidates) == 0 {
		return nil
	}

	ids := make([]int, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}

	existing, err := client.Channel.Query().Where(channel.IDIn(ids...)).IDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list existing channels: %w", err)
	}
	existingSet := make(map[int]struct{}, len(existing))
	for _, id := range existing {
		existingSet[id] = struct{}{}
	}

	missing := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := existingSet[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("failed to start intelligence cleanup transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	txCtx := ent.NewContext(ctx, tx.Client())
	if err := deleteIntelligenceRuns(txCtx, tx.Client(), missing); err != nil {
		return err
	}
	if err := s.removeIntelligenceTargets(txCtx, tx.Client(), missing); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit intelligence cleanup: %w", err)
	}
	committed = true

	log.Info(ctx, "intelligence check: pruned deleted channels", log.Int("channels", len(missing)))
	return nil
}

func deleteIntelligenceRuns(ctx context.Context, client *ent.Client, channelIDs []int) error {
	if _, err := client.IntelligenceRun.Delete().
		Where(intelligencerun.ChannelIDIn(channelIDs...)).
		Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete intelligence runs: %w", err)
	}

	return nil
}

func (s *IntelligenceService) removeIntelligenceTargets(ctx context.Context, client *ent.Client, channelIDs []int) error {
	config, err := intelligenceConfigFromClient(ctx, client)
	if err != nil {
		return err
	}
	if config == nil {
		return nil
	}

	kept := make([]objects.IntelligenceTarget, 0, len(config.Targets))
	for _, target := range config.Targets {
		if slices.Contains(channelIDs, target.ChannelID) {
			continue
		}
		kept = append(kept, target)
	}
	if len(kept) == len(config.Targets) {
		return nil
	}

	removed := len(config.Targets) - len(kept)
	config.Targets = kept
	encoded, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to encode intelligence config: %w", err)
	}
	if err := s.systemService.SetSystemValue(ctx, IntelligenceConfigKey, string(encoded)); err != nil {
		return fmt.Errorf("failed to update intelligence config: %w", err)
	}

	log.Info(ctx, "intelligence check: removed deleted channels from configuration",
		log.Int("removed", removed))
	return nil
}

func intelligenceConfigFromClient(ctx context.Context, client *ent.Client) (*objects.IntelligenceConfig, error) {
	sys, err := client.System.Query().Where(system.KeyEQ(IntelligenceConfigKey)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read intelligence config: %w", err)
	}
	if strings.TrimSpace(sys.Value) == "" {
		return nil, nil
	}

	config := &objects.IntelligenceConfig{}
	if err := json.Unmarshal([]byte(sys.Value), config); err != nil {
		return nil, fmt.Errorf("failed to parse intelligence config: %w", err)
	}
	normalizeIntelligenceConfig(config)

	return config, nil
}
