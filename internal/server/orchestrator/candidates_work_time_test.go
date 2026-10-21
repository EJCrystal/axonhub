package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestFilterCandidatesByWorkTime(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	closed := &objects.WorkTimeSchedule{
		Timezone: "UTC",
		Windows:  []objects.WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "09:00", End: "10:00"}},
	}
	open := &objects.WorkTimeSchedule{
		Timezone: "UTC",
		Windows:  []objects.WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "11:00", End: "13:00"}},
	}

	t.Run("drops a channel outside its window", func(t *testing.T) {
		candidates := []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{
				ID:          1,
				Credentials: objects.ChannelCredentials{APIKeys: []string{"key-1"}},
				Settings:    &objects.ChannelSettings{WorkTime: closed},
			}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{
				ID:          2,
				Credentials: objects.ChannelCredentials{APIKeys: []string{"key-2"}},
				Settings:    &objects.ChannelSettings{WorkTime: open},
			}}},
		}

		filtered := filterCandidatesByWorkTime(context.Background(), candidates, now)
		require.Len(t, filtered, 1)
		require.Equal(t, 2, filtered[0].Channel.ID)
	})

	t.Run("keeps a channel with at least one key in its window", func(t *testing.T) {
		channel := &biz.Channel{Channel: &ent.Channel{
			ID:          3,
			Credentials: objects.ChannelCredentials{APIKeys: []string{"key-closed", "key-open"}},
			APIKeyWorkTimes: []objects.APIKeyWorkTime{
				{Key: "key-closed", WorkTime: closed},
				{Key: "key-open", WorkTime: open},
			},
		}}

		filtered := filterCandidatesByWorkTime(context.Background(), []*ChannelModelsCandidate{{Channel: channel}}, now)
		require.Len(t, filtered, 1)
	})

	t.Run("drops a channel when every key is outside its window", func(t *testing.T) {
		channel := &biz.Channel{Channel: &ent.Channel{
			ID:          4,
			Credentials: objects.ChannelCredentials{APIKeys: []string{"key-closed"}},
			APIKeyWorkTimes: []objects.APIKeyWorkTime{
				{Key: "key-closed", WorkTime: closed},
			},
		}}

		filtered := filterCandidatesByWorkTime(context.Background(), []*ChannelModelsCandidate{{Channel: channel}}, now)
		require.Empty(t, filtered)
	})
}
