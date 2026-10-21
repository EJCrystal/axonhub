package biz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func workTimeSchedule(t *testing.T, start, end string) *objects.WorkTimeSchedule {
	t.Helper()

	schedule := &objects.WorkTimeSchedule{
		Timezone: "Asia/Shanghai",
		Windows:  []objects.WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: start, End: end}},
	}
	require.NoError(t, schedule.Validate())

	return schedule
}

func TestBuildChannel_CachesWorkTime(t *testing.T) {
	entity := &ent.Channel{
		Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1"}},
		Settings: &objects.ChannelSettings{
			WorkTime: workTimeSchedule(t, "09:00", "18:00"),
		},
		APIKeyWorkTimes: []objects.APIKeyWorkTime{
			{Key: "key-1", WorkTime: workTimeSchedule(t, "10:00", "11:00")},
			{Key: "removed-key", WorkTime: workTimeSchedule(t, "10:00", "11:00")},
		},
	}

	ch := buildChannel(entity, nil)
	require.NotNil(t, ch.cachedWorkTime)
	require.Contains(t, ch.cachedAPIKeyWorkTimes, "key-1")
	require.Contains(t, ch.cachedAPIKeyWorkTimes, "removed-key", "pruning happens on write, not when the snapshot is built")
}

func TestChannel_AvailableAt(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	// 2026-10-21 is a Wednesday.
	inside := time.Date(2026, 10, 21, 12, 0, 0, 0, loc)
	outside := time.Date(2026, 10, 21, 20, 0, 0, 0, loc)

	t.Run("no schedule is always available", func(t *testing.T) {
		ch := &Channel{Channel: &ent.Channel{Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1"}}}}
		require.True(t, ch.AvailableAt(context.Background(), outside))
		require.Equal(t, []string{"key-1"}, ch.EnabledAPIKeysAt(context.Background(), outside))
	})

	t.Run("channel window closed hides the channel", func(t *testing.T) {
		ch := &Channel{
			Channel: &ent.Channel{Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1"}}},
		}
		ch.cachedWorkTime = workTimeSchedule(t, "09:00", "18:00")

		require.True(t, ch.AvailableAt(context.Background(), inside))
		require.False(t, ch.AvailableAt(context.Background(), outside))
		require.Empty(t, ch.EnabledAPIKeysAt(context.Background(), outside))
	})

	t.Run("per-key window filters keys", func(t *testing.T) {
		ch := &Channel{
			Channel: &ent.Channel{Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1", "key-2"}}},
		}
		ch.cachedWorkTime = workTimeSchedule(t, "09:00", "18:00")
		ch.cachedAPIKeyWorkTimes = map[string]*objects.WorkTimeSchedule{
			"key-1": workTimeSchedule(t, "09:00", "10:00"),
		}

		require.Equal(t, []string{"key-2"}, ch.EnabledAPIKeysAt(context.Background(), inside))
		require.True(t, ch.AvailableAt(context.Background(), inside))
		require.False(t, ch.AvailableAt(context.Background(), time.Date(2026, 10, 21, 20, 0, 0, 0, loc)))
	})

	t.Run("all keys outside their window disables the channel", func(t *testing.T) {
		ch := &Channel{
			Channel: &ent.Channel{Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1"}}},
		}
		ch.cachedAPIKeyWorkTimes = map[string]*objects.WorkTimeSchedule{
			"key-1": workTimeSchedule(t, "09:00", "10:00"),
		}

		require.False(t, ch.AvailableAt(context.Background(), inside))
	})

	t.Run("empty timezone follows the system location", func(t *testing.T) {
		schedule := &objects.WorkTimeSchedule{
			Windows: []objects.WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "09:00", End: "18:00"}},
		}
		require.NoError(t, schedule.Validate())

		ch := &Channel{
			Channel: &ent.Channel{Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1"}}},
		}
		ch.cachedWorkTime = schedule
		ch.systemLocationFn = func(context.Context) *time.Location { return loc }

		require.True(t, ch.AvailableAt(context.Background(), inside))
		require.False(t, ch.AvailableAt(context.Background(), outside))
	})

	t.Run("disabled keys stay unavailable", func(t *testing.T) {
		ch := &Channel{
			Channel: &ent.Channel{
				Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1"}},
				DisabledAPIKeys: []objects.DisabledAPIKey{{
					Key:        "key-1",
					DisabledAt: time.Now(),
				}},
			},
		}

		require.False(t, ch.AvailableAt(context.Background(), inside))
	})

	t.Run("oauth credential honours disable and work time", func(t *testing.T) {
		ch := &Channel{
			Channel: &ent.Channel{
				Credentials: objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "token"}},
			},
		}
		require.True(t, ch.AvailableAt(context.Background(), inside))

		ch.Channel.DisabledAPIKeys = []objects.DisabledAPIKey{{
			Key:        objects.OAuthCredentialRef,
			DisabledAt: time.Now(),
		}}
		require.False(t, ch.AvailableAt(context.Background(), inside))

		ch.Channel.DisabledAPIKeys = nil
		ch.cachedAPIKeyWorkTimes = map[string]*objects.WorkTimeSchedule{
			objects.OAuthCredentialRef: workTimeSchedule(t, "09:00", "10:00"),
		}
		require.False(t, ch.AvailableAt(context.Background(), inside))
	})
}

func TestPruneAPIKeyWorkTimes(t *testing.T) {
	schedule := workTimeSchedule(t, "09:00", "18:00")
	credentials := objects.ChannelCredentials{APIKeys: []string{"key-1"}}
	schedules := []objects.APIKeyWorkTime{
		{Key: "key-1", WorkTime: schedule},
		{Key: "key-2", WorkTime: schedule},
		{Key: "", WorkTime: schedule},
	}

	pruned := pruneAPIKeyWorkTimes(credentials, schedules)
	require.Len(t, pruned, 1)
	require.Equal(t, "key-1", pruned[0].Key)
}

func TestChannelService_SetChannelAPIKeyWorkTime(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()

	ctx := ent.NewContext(context.Background(), client)
	ctx = authz.WithTestBypass(ctx)

	entity := client.Channel.Create().
		SetName("work-time-channel").
		SetType(channel.TypeOpenai).
		SetCredentials(objects.ChannelCredentials{APIKeys: []string{"key-1"}}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SaveX(ctx)

	schedule := workTimeSchedule(t, "09:00", "18:00")
	require.NoError(t, svc.SetChannelAPIKeyWorkTime(ctx, entity.ID, "key-1", schedule))

	stored := client.Channel.GetX(ctx, entity.ID)
	require.Len(t, stored.APIKeyWorkTimes, 1)
	require.Equal(t, "key-1", stored.APIKeyWorkTimes[0].Key)
	require.Equal(t, "Asia/Shanghai", stored.APIKeyWorkTimes[0].WorkTime.Timezone)

	// Replacing keeps a single entry per credential.
	require.NoError(t, svc.SetChannelAPIKeyWorkTime(ctx, entity.ID, "key-1", workTimeSchedule(t, "10:00", "12:00")))
	stored = client.Channel.GetX(ctx, entity.ID)
	require.Len(t, stored.APIKeyWorkTimes, 1)
	require.Equal(t, "10:00", stored.APIKeyWorkTimes[0].WorkTime.Windows[0].Start)

	// Unknown credentials are rejected.
	require.Error(t, svc.SetChannelAPIKeyWorkTime(ctx, entity.ID, "unknown", schedule))

	// Nil clears the restriction.
	require.NoError(t, svc.SetChannelAPIKeyWorkTime(ctx, entity.ID, "key-1", nil))
	require.Empty(t, client.Channel.GetX(ctx, entity.ID).APIKeyWorkTimes)
}

func TestNormalizeWorkTimeSettings_KeepsEmptyTimezoneForSystemDefault(t *testing.T) {
	settings := &objects.ChannelSettings{
		WorkTime: &objects.WorkTimeSchedule{
			Windows: []objects.WorkTimeWindow{{Days: []int{1}, Start: "09:00", End: "18:00"}},
		},
	}

	require.NoError(t, NormalizeWorkTimeSettings(settings))
	require.Empty(t, settings.WorkTime.Timezone, "an empty timezone follows the system setting at evaluation time")

	empty := &objects.ChannelSettings{WorkTime: &objects.WorkTimeSchedule{}}
	require.NoError(t, NormalizeWorkTimeSettings(empty))
	require.Nil(t, empty.WorkTime)
}

func TestChannelService_DuplicateChannelCopiesAPIKeyWorkTimes(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()

	ctx := ent.NewContext(context.Background(), client)
	ctx = authz.WithTestBypass(ctx)

	source := client.Channel.Create().
		SetName("work-time-source").
		SetType(channel.TypeOpenai).
		SetCredentials(objects.ChannelCredentials{APIKeys: []string{"key-1", "key-2"}}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SaveX(ctx)

	schedule := workTimeSchedule(t, "09:00", "18:00")
	require.NoError(t, svc.SetChannelAPIKeyWorkTime(ctx, source.ID, "key-1", schedule))
	require.NoError(t, svc.SetChannelAPIKeyWorkTime(ctx, source.ID, "key-2", schedule))

	duplicated, err := svc.DuplicateChannel(ctx, source.ID, ent.CreateChannelInput{
		Type:             channel.TypeOpenai,
		Name:             "work-time-copy",
		Credentials:      objects.ChannelCredentials{APIKeys: []string{"key-1"}},
		SupportedModels:  []string{"gpt-4"},
		DefaultTestModel: "gpt-4",
	})
	require.NoError(t, err)
	require.Len(t, duplicated.APIKeyWorkTimes, 1)
	require.Equal(t, "key-1", duplicated.APIKeyWorkTimes[0].Key)
}

func TestChannelService_ChannelAvailableAtUsesSystemTimezone(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()

	ctx := ent.NewContext(context.Background(), client)
	ctx = authz.WithTestBypass(ctx)

	require.NoError(t, svc.SystemService.SetGeneralSettings(ctx, SystemGeneralSettings{Timezone: "Asia/Shanghai"}))

	entity := client.Channel.Create().
		SetName("system-timezone-channel").
		SetType(channel.TypeOpenai).
		SetCredentials(objects.ChannelCredentials{APIKeys: []string{"key-1"}}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetSettings(&objects.ChannelSettings{
			WorkTime: &objects.WorkTimeSchedule{
				Windows: []objects.WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "09:00", End: "10:00"}},
			},
		}).
		SaveX(ctx)

	// 01:30 UTC == 09:30 Asia/Shanghai (inside); 02:30 UTC == 10:30 (outside).
	require.True(t, svc.ChannelAvailableAt(ctx, entity, time.Date(2026, 10, 21, 1, 30, 0, 0, time.UTC)))
	require.False(t, svc.ChannelAvailableAt(ctx, entity, time.Date(2026, 10, 21, 2, 30, 0, 0, time.UTC)))
}
