package biz

import (
	"context"
	"fmt"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xregexp"
)

// syncChannelModels syncs supported models for all channels with auto_sync_supported_models enabled.
// This function is called periodically (every hour) to keep model lists up to date.
func (svc *ChannelService) syncChannelModels(ctx context.Context) {
	// Query all enabled channels with auto_sync_supported_models = true
	channels, err := svc.entFromContext(ctx).Channel.
		Query().
		Where(
			channel.StatusEQ(channel.StatusEnabled),
			channel.AutoSyncSupportedModelsEQ(true),
		).
		All(ctx)
	if err != nil {
		log.Error(ctx, "failed to query channels for model sync", log.Cause(err))
		return
	}

	if len(channels) == 0 {
		log.Debug(ctx, "no channels with auto_sync_supported_models enabled")
		return
	}

	log.Info(ctx, "starting model sync for channels", log.Int("count", len(channels)))

	successCount := 0
	failureCount := 0
	changedCount := 0

	for _, ch := range channels {
		_, changed, err := svc.syncChannelModelsForChannel(ctx, ch, nil)
		if err != nil {
			log.Warn(ctx, "failed to sync models for channel",
				log.Int("channel_id", ch.ID),
				log.String("channel_name", ch.Name),
				log.Cause(err))

			failureCount++
		} else {
			successCount++
			if changed {
				changedCount++
			}
		}
	}

	if changedCount > 0 {
		svc.asyncReloadChannels()
	}

	log.Info(ctx, "completed model sync for channels",
		log.Int("success", successCount),
		log.Int("failure", failureCount),
		log.Int("changed", changedCount))
}

// syncChannelModelsForChannel syncs supported models for a single channel.
func (svc *ChannelService) syncChannelModelsForChannel(ctx context.Context, ch *ent.Channel, patternOverride *string) (*ent.Channel, bool, error) {
	modelFetcher := NewModelFetcher(svc.httpClient, svc)

	fetchedByKey, err := fetchModelsByAPIKey(ctx, modelFetcher, ch)
	if err != nil {
		return nil, false, err
	}

	fetchedModelIDs := unionFetchedAPIKeyModels(ch.Credentials.GetAllAPIKeys(), fetchedByKey)

	pattern := ch.AutoSyncModelPattern
	if patternOverride != nil {
		pattern = *patternOverride
	}

	// Filter by auto_sync_model_pattern if set
	if pattern != "" {
		if err := xregexp.ValidateRegex(pattern); err != nil {
			log.Warn(ctx, "invalid auto_sync_model_pattern, skipping filter",
				log.Int("channel_id", ch.ID),
				log.String("pattern", pattern),
				log.Cause(err))
		} else {
			before := len(fetchedModelIDs)
			fetchedModelIDs = xregexp.Filter(fetchedModelIDs, pattern)
			for key, models := range fetchedByKey {
				fetchedByKey[key] = xregexp.Filter(models, pattern)
			}
			log.Info(ctx, "filtered models by pattern",
				log.Int("channel_id", ch.ID),
				log.String("pattern", pattern),
				log.Int("before", before),
				log.Int("after", len(fetchedModelIDs)))
		}
	}

	var (
		updatedCh     *ent.Channel
		changed       bool
		modelsChanged bool
		manualCount   int
		totalCount    int
	)

	err = svc.RunInTransaction(ctx, func(ctx context.Context) error {
		db := svc.entFromContext(ctx)

		// Re-read the channel inside the transaction: the provider fetch above can
		// take seconds, so manual models saved meanwhile must not be overwritten by
		// the stale snapshot passed into this function.
		current, err := db.Channel.Get(ctx, ch.ID)
		if err != nil {
			return fmt.Errorf("failed to reload channel for model sync: %w", err)
		}

		updatedCh = current

		manualModels := current.ManualModels
		if manualModels == nil {
			manualModels = []string{}
		}
		manualCount = len(manualModels)

		// A key that was fetched keeps only the models that key returned. Keys that
		// could not be fetched keep their previous assignment instead of inheriting
		// another key's models.
		previousAPIKeyModels := append([]objects.APIKeyModels(nil), current.Credentials.APIKeyModels...)
		if len(fetchedByKey) > 0 {
			current.Credentials.ApplyFetchedAPIKeyModels(fetchedByKey, current.DisabledAPIKeys)
			fetchedModelIDs = current.Credentials.UnionEnabledAPIKeyModels(current.SupportedModels, current.DisabledAPIKeys)
		}
		credentialsChanged := !sameAPIKeyModelAssignments(previousAPIKeyModels, current.Credentials.APIKeyModels)

		mergedModels := lo.Uniq(append(manualModels, fetchedModelIDs...))
		totalCount = len(mergedModels)

		if len(mergedModels) == 0 {
			log.Warn(ctx, "no models to sync for channel (both fetched and manual are empty)",
				log.Int("channel_id", ch.ID),
				log.String("channel_name", ch.Name))

			return nil
		}

		addedModels := lo.Without(mergedModels, current.SupportedModels...)
		removedModels := lo.Without(current.SupportedModels, mergedModels...)
		modelsChanged = len(addedModels) > 0 || len(removedModels) > 0
		modelProtocolsChanged := modelsChanged && RemoveRemovedModelProtocolOverrides(current.Settings, mergedModels)

		if modelsChanged || credentialsChanged {
			update := db.Channel.
				UpdateOneID(ch.ID).
				SetCredentials(current.Credentials).
				SetSupportedModels(mergedModels)
			if modelProtocolsChanged {
				update.SetSettings(current.Settings)
			}
			updated, err := update.Save(ctx)
			if err != nil {
				return fmt.Errorf("failed to update channel supported models: %w", err)
			}

			updatedCh = updated
		}

		pricesChanged, err := svc.ensureChannelModelPrices(ctx, ch.ID, mergedModels)
		if err != nil {
			return err
		}

		changed = modelsChanged || modelProtocolsChanged || pricesChanged

		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if ent.TxFromContext(ctx) == nil && updatedCh != nil {
		updatedCh.Unwrap()
	}

	log.Info(ctx, "successfully synced models for channel",
		log.Int("channel_id", ch.ID),
		log.String("channel_name", ch.Name),
		log.Int("fetched_count", len(fetchedModelIDs)),
		log.Int("manual_count", manualCount),
		log.Int("total_count", totalCount))

	return updatedCh, changed, nil
}

func (svc *ChannelService) SyncChannelModels(ctx context.Context, channelID int, patternOverride *string) (*ent.Channel, error) {
	ch, err := svc.entFromContext(ctx).Channel.Get(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel: %w", err)
	}

	updated, changed, err := svc.syncChannelModelsForChannel(ctx, ch, patternOverride)
	if err != nil {
		return nil, err
	}

	if changed {
		svc.reloadChannelsAfterCommit(ctx)
	}

	return updated, nil
}

func fetchModelsByAPIKey(ctx context.Context, modelFetcher *ModelFetcher, ch *ent.Channel) (map[string][]string, error) {
	keys := ch.Credentials.GetEnabledAPIKeys(ch.DisabledAPIKeys)
	if len(keys) == 0 {
		keys = []string{""}
	}

	fetched := make(map[string][]string, len(keys))
	var firstErr error
	for _, key := range keys {
		input := FetchModelsInput{
			ChannelType: ch.Type.String(),
			BaseURL:     ch.BaseURL,
			ChannelID:   lo.ToPtr(ch.ID),
		}
		if key != "" {
			input.APIKey = lo.ToPtr(key)
		}

		result, err := modelFetcher.FetchModels(ctx, input)
		if err != nil {
			err = fmt.Errorf("failed to fetch models: %w", err)
		} else if result.Error != nil {
			err = fmt.Errorf("model fetch returned error: %s", *result.Error)
		} else if result.Fallback {
			err = fmt.Errorf("model fetch returned fallback models")
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Warn(ctx, "failed to fetch models for api key",
				log.Int("channel_id", ch.ID),
				log.String("key_prefix", safeAPIKeyPrefix(key)),
				log.Cause(err))
			continue
		}

		fetched[key] = lo.Map(result.Models, func(m ModelIdentify, _ int) string { return m.ID })
	}
	if len(fetched) == 0 && firstErr != nil {
		return nil, firstErr
	}

	return fetched, nil
}

func sameAPIKeyModelAssignments(left, right []objects.APIKeyModels) bool {
	leftMap := apiKeyModelAssignmentMap(left)
	rightMap := apiKeyModelAssignmentMap(right)
	if len(leftMap) != len(rightMap) {
		return false
	}
	for key, models := range leftMap {
		if !sameStringSet(models, rightMap[key]) {
			return false
		}
	}

	return true
}

func apiKeyModelAssignmentMap(items []objects.APIKeyModels) map[string][]string {
	assigned := make(map[string][]string, len(items))
	for _, item := range items {
		if len(item.Models) == 0 {
			continue
		}
		assigned[item.APIKey] = item.Models
	}

	return assigned
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, item := range left {
		seen[item]++
	}
	for _, item := range right {
		seen[item]--
		if seen[item] < 0 {
			return false
		}
	}

	return true
}

func unionFetchedAPIKeyModels(keys []string, fetched map[string][]string) []string {
	if len(fetched) == 0 {
		return nil
	}
	if len(keys) == 0 {
		return append([]string(nil), fetched[""]...)
	}

	union := make([]string, 0)
	for _, key := range keys {
		union = append(union, fetched[key]...)
	}

	return lo.Uniq(union)
}
