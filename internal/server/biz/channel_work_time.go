package biz

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
)

// WorkTimeActiveAt reports whether the channel-level work-time schedule allows
// traffic at now. A channel without a schedule is always active.
func (ch *Channel) WorkTimeActiveAt(ctx context.Context, now time.Time) bool {
	if ch == nil {
		return false
	}

	schedule := ch.workTimeSchedule()

	return !schedule.IsConfigured() || schedule.IsActiveIn(now, ch.defaultLocation(ctx, schedule))
}

// NextWorkTimeStart returns the next channel-level window start, when one is
// configured.
func (ch *Channel) NextWorkTimeStart(ctx context.Context, now time.Time) (time.Time, bool) {
	if ch == nil {
		return time.Time{}, false
	}

	schedule := ch.workTimeSchedule()
	if !schedule.IsConfigured() {
		return time.Time{}, false
	}

	return schedule.NextStartIn(now, ch.defaultLocation(ctx, schedule))
}

// systemLocation resolves the system timezone at call time so schedules that do
// not carry their own timezone follow changes in General Settings.
func (ch *Channel) systemLocation(ctx context.Context) *time.Location {
	if ch == nil || ch.systemLocationFn == nil {
		return nil
	}

	return ch.systemLocationFn(ctx)
}

// defaultLocation returns the fallback location only when the schedule relies on
// it, avoiding a system-settings lookup for schedules with an explicit timezone.
func (ch *Channel) defaultLocation(ctx context.Context, schedule *objects.WorkTimeSchedule) *time.Location {
	if schedule != nil && strings.TrimSpace(schedule.Timezone) != "" {
		return nil
	}

	return ch.systemLocation(ctx)
}

func (ch *Channel) hasWorkTimeRestriction() bool {
	if ch == nil {
		return false
	}

	return ch.workTimeSchedule().IsConfigured() || len(ch.apiKeyWorkTimes()) > 0
}

// workTimeSchedule prefers the snapshot cache and falls back to the entity for
// channels constructed outside buildChannel (tests, one-off snapshots).
func (ch *Channel) workTimeSchedule() *objects.WorkTimeSchedule {
	if ch == nil {
		return nil
	}
	if ch.cachedWorkTime != nil {
		return ch.cachedWorkTime
	}
	if ch.Channel != nil && ch.Settings != nil {
		return ch.Settings.WorkTime
	}

	return nil
}

func (ch *Channel) apiKeyWorkTimes() map[string]*objects.WorkTimeSchedule {
	if ch == nil {
		return nil
	}
	if ch.cachedAPIKeyWorkTimes != nil {
		return ch.cachedAPIKeyWorkTimes
	}

	return apiKeyWorkTimesFromEntity(ch.Channel)
}

// APIKeyWorkTime returns the configured schedule for one credential reference.
func (ch *Channel) APIKeyWorkTime(key string) *objects.WorkTimeSchedule {
	if ch == nil {
		return nil
	}

	return ch.apiKeyWorkTimes()[key]
}

// APIKeyActiveAt reports whether a credential may be used at now. It accounts
// for both the channel-level window and the credential's own schedule.
func (ch *Channel) APIKeyActiveAt(ctx context.Context, key string, now time.Time) bool {
	if !ch.WorkTimeActiveAt(ctx, now) {
		return false
	}

	schedule := ch.APIKeyWorkTime(key)

	return !schedule.IsConfigured() || schedule.IsActiveIn(now, ch.defaultLocation(ctx, schedule))
}

// EnabledAPIKeysAt returns the credentials that are neither disabled nor
// outside their work-time window at now.
func (ch *Channel) EnabledAPIKeysAt(ctx context.Context, now time.Time) []string {
	if ch == nil {
		return nil
	}

	// Prefer the snapshot taken when the channel was loaded: it is the same view
	// the rest of the request uses, and channels built for tests may not have it.
	keys := ch.cachedEnabledAPIKeys
	if keys == nil {
		keys = ch.Credentials.GetEnabledAPIKeys(ch.DisabledAPIKeys)
	}

	return ch.filterAPIKeysByWorkTime(ctx, keys, now)
}

// filterAPIKeysByWorkTime narrows a key list to the credentials whose own
// schedule is open at now. It also enforces the channel-level window so callers
// can compose it with model/failed-key filtering.
func (ch *Channel) filterAPIKeysByWorkTime(ctx context.Context, keys []string, now time.Time) []string {
	if ch == nil || !ch.WorkTimeActiveAt(ctx, now) {
		return nil
	}

	keySchedules := ch.apiKeyWorkTimes()
	if len(keySchedules) == 0 {
		return keys
	}

	active := make([]string, 0, len(keys))
	for _, key := range keys {
		schedule := keySchedules[key]
		if !schedule.IsConfigured() || schedule.IsActiveIn(now, ch.defaultLocation(ctx, schedule)) {
			active = append(active, key)
		}
	}

	return active
}

// AvailableAt reports whether the channel can serve a request at now: its
// channel window must be open and at least one credential must be usable.
// Channels that authenticate without credentials (for example a local Ollama)
// stay available as long as their channel window is open.
func (ch *Channel) AvailableAt(ctx context.Context, now time.Time) bool {
	if ch == nil || ch.Channel == nil {
		return false
	}
	if !ch.WorkTimeActiveAt(ctx, now) {
		return false
	}

	if len(ch.Credentials.GetAllCredentialRefs()) == 0 {
		return true
	}

	// OAuth channels authenticate with a single credential reference rather than
	// an API key, so the per-key list does not apply to them. That reference can
	// still be disabled or carry its own work-time window.
	if ch.Credentials.IsOAuth() {
		for _, disabled := range ch.DisabledAPIKeys {
			if disabled.Key == objects.OAuthCredentialRef && !disabled.IsExpired() {
				return false
			}
		}

		schedule := ch.apiKeyWorkTimes()[objects.OAuthCredentialRef]

		return !schedule.IsConfigured() || schedule.IsActiveIn(now, ch.defaultLocation(ctx, schedule))
	}

	return len(ch.EnabledAPIKeysAt(ctx, now)) > 0
}

// ChannelAvailableAt evaluates availability for a channel entity loaded outside
// the enabled-channel cache (for example a GraphQL resolver). It attaches the
// system timezone resolver so schedules without an explicit timezone follow
// General Settings.
func (svc *ChannelService) ChannelAvailableAt(ctx context.Context, entity *ent.Channel, now time.Time) bool {
	if entity == nil {
		return false
	}

	ch := &Channel{Channel: entity}
	if svc != nil && svc.SystemService != nil {
		ch.systemLocationFn = svc.SystemService.TimeLocation
	}

	return ch.AvailableAt(ctx, now)
}

// ChannelNextWorkTimeStart is the entity-based counterpart of
// Channel.NextWorkTimeStart.
func (svc *ChannelService) ChannelNextWorkTimeStart(ctx context.Context, entity *ent.Channel, now time.Time) (time.Time, bool) {
	if entity == nil {
		return time.Time{}, false
	}

	ch := &Channel{Channel: entity}
	if svc != nil && svc.SystemService != nil {
		ch.systemLocationFn = svc.SystemService.TimeLocation
	}

	return ch.NextWorkTimeStart(ctx, now)
}

// NormalizeWorkTimeSettings validates a channel's work-time settings in place.
// An empty schedule is stored as nil so that "no windows" has one
// representation. A schedule without a timezone follows the system timezone at
// evaluation time.
func NormalizeWorkTimeSettings(settings *objects.ChannelSettings) error {
	if settings == nil {
		return nil
	}

	if err := settings.WorkTime.Validate(); err != nil {
		return fmt.Errorf("invalid work time: %w", err)
	}
	if !settings.WorkTime.IsConfigured() {
		settings.WorkTime = nil
	}

	return nil
}

// SetChannelAPIKeyWorkTime replaces one credential's work-time schedule. A nil
// or empty schedule removes the restriction.
func (svc *ChannelService) SetChannelAPIKeyWorkTime(
	ctx context.Context,
	channelID int,
	key string,
	workTime *objects.WorkTimeSchedule,
) error {
	if key == "" {
		return fmt.Errorf("api key cannot be empty")
	}
	if err := workTime.Validate(); err != nil {
		return fmt.Errorf("invalid work time: %w", err)
	}

	svc.apiKeyOpsLock.Lock()
	defer svc.apiKeyOpsLock.Unlock()

	ch, err := svc.entFromContext(ctx).Channel.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	if !slices.Contains(ch.Credentials.GetAllCredentialRefs(), key) {
		return fmt.Errorf("api key is not part of channel %d", channelID)
	}

	next := make([]objects.APIKeyWorkTime, 0, len(ch.APIKeyWorkTimes)+1)
	for _, item := range ch.APIKeyWorkTimes {
		if item.Key != key {
			next = append(next, item)
		}
	}
	if workTime.IsConfigured() {
		next = append(next, objects.APIKeyWorkTime{Key: key, WorkTime: workTime})
	}

	if _, err := svc.entFromContext(ctx).Channel.UpdateOneID(channelID).
		SetAPIKeyWorkTimes(next).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to update api key work time: %w", err)
	}

	svc.reloadChannelsAfterCommit(ctx)

	return nil
}

// pruneAPIKeyWorkTimes drops schedules whose credential is no longer present on
// the channel.
func pruneAPIKeyWorkTimes(
	credentials objects.ChannelCredentials,
	schedules []objects.APIKeyWorkTime,
) []objects.APIKeyWorkTime {
	if len(schedules) == 0 {
		return schedules
	}

	refs := credentials.GetAllCredentialRefs()
	pruned := make([]objects.APIKeyWorkTime, 0, len(schedules))
	for _, item := range schedules {
		if item.Key == "" || !slices.Contains(refs, item.Key) {
			continue
		}
		pruned = append(pruned, item)
	}

	return pruned
}

// apiKeyWorkTimesFromEntity copies the stored schedules into the runtime cache.
func apiKeyWorkTimesFromEntity(entity *ent.Channel) map[string]*objects.WorkTimeSchedule {
	if entity == nil || len(entity.APIKeyWorkTimes) == 0 {
		return nil
	}

	cached := make(map[string]*objects.WorkTimeSchedule, len(entity.APIKeyWorkTimes))
	for _, item := range entity.APIKeyWorkTimes {
		if item.Key == "" || !item.WorkTime.IsConfigured() {
			continue
		}
		cached[item.Key] = item.WorkTime
	}
	if len(cached) == 0 {
		return nil
	}

	return cached
}
