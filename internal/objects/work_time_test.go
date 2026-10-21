package objects

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	require.NoError(t, err)

	return loc
}

func TestWorkTimeSchedule_Validate(t *testing.T) {
	t.Run("empty schedule is valid", func(t *testing.T) {
		require.NoError(t, (*WorkTimeSchedule)(nil).Validate())
		require.NoError(t, (&WorkTimeSchedule{}).Validate())
	})

	t.Run("valid weekly schedule", func(t *testing.T) {
		s := &WorkTimeSchedule{
			Timezone: "Asia/Shanghai",
			Windows: []WorkTimeWindow{
				{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"},
				{Days: []int{5}, Start: "22:00", End: "06:00"},
			},
		}
		require.NoError(t, s.Validate())
	})

	t.Run("normalizes weekdays", func(t *testing.T) {
		s := &WorkTimeSchedule{
			Timezone: " Asia/Shanghai ",
			Windows:  []WorkTimeWindow{{Days: []int{5, 1, 5, 3}, Start: " 09:00 ", End: "18:00"}},
		}
		require.NoError(t, s.Validate())
		require.Equal(t, "Asia/Shanghai", s.Timezone)
		require.Equal(t, []int{1, 3, 5}, s.Windows[0].Days)
		require.Equal(t, "09:00", s.Windows[0].Start)
	})

	t.Run("rejects unknown timezone", func(t *testing.T) {
		s := &WorkTimeSchedule{Timezone: "Mars/Olympus", Windows: []WorkTimeWindow{{Days: []int{1}, Start: "09:00", End: "10:00"}}}
		require.Error(t, s.Validate())
	})

	t.Run("rejects empty days", func(t *testing.T) {
		s := &WorkTimeSchedule{Windows: []WorkTimeWindow{{Start: "09:00", End: "10:00"}}}
		require.Error(t, s.Validate())
	})

	t.Run("rejects out of range weekday", func(t *testing.T) {
		s := &WorkTimeSchedule{Windows: []WorkTimeWindow{{Days: []int{0}, Start: "09:00", End: "10:00"}}}
		require.Error(t, s.Validate())
	})

	t.Run("rejects malformed clock", func(t *testing.T) {
		s := &WorkTimeSchedule{Windows: []WorkTimeWindow{{Days: []int{1}, Start: "9:00", End: "10:00"}}}
		require.Error(t, s.Validate())
	})

	t.Run("rejects equal start and end", func(t *testing.T) {
		s := &WorkTimeSchedule{Windows: []WorkTimeWindow{{Days: []int{1}, Start: "09:00", End: "09:00"}}}
		require.Error(t, s.Validate())
	})
}

func TestWorkTimeSchedule_IsActive(t *testing.T) {
	loc := mustLoadLocation(t, "Asia/Shanghai")

	schedule := &WorkTimeSchedule{
		Timezone: "Asia/Shanghai",
		Windows: []WorkTimeWindow{
			{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"},
		},
	}
	require.NoError(t, schedule.Validate())

	// 2026-10-21 is a Wednesday.
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 10, 21, hour, minute, 0, 0, loc)
	}

	require.False(t, schedule.IsActive(at(8, 59)))
	require.True(t, schedule.IsActive(at(9, 0)))
	require.True(t, schedule.IsActive(at(12, 30)))
	require.True(t, schedule.IsActive(at(17, 59)))
	require.False(t, schedule.IsActive(at(18, 0)))
	require.False(t, schedule.IsActive(at(23, 0)))
}

func TestWorkTimeSchedule_IsActiveWeekend(t *testing.T) {
	loc := mustLoadLocation(t, "Asia/Shanghai")
	schedule := &WorkTimeSchedule{
		Timezone: "Asia/Shanghai",
		Windows:  []WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}},
	}
	require.NoError(t, schedule.Validate())

	// 2026-10-24 is a Saturday.
	saturday := time.Date(2026, 10, 24, 12, 0, 0, 0, loc)
	require.False(t, schedule.IsActive(saturday))
}

func TestWorkTimeSchedule_IsActiveCrossesMidnight(t *testing.T) {
	loc := mustLoadLocation(t, "Asia/Shanghai")
	schedule := &WorkTimeSchedule{
		Timezone: "Asia/Shanghai",
		// Friday 22:00 until Saturday 06:00.
		Windows: []WorkTimeWindow{{Days: []int{5}, Start: "22:00", End: "06:00"}},
	}
	require.NoError(t, schedule.Validate())

	// 2026-10-23 is a Friday.
	friday := func(hour int) time.Time { return time.Date(2026, 10, 23, hour, 0, 0, 0, loc) }
	saturday := func(hour int) time.Time { return time.Date(2026, 10, 24, hour, 0, 0, 0, loc) }

	require.False(t, schedule.IsActive(friday(21)))
	require.True(t, schedule.IsActive(friday(22)))
	require.True(t, schedule.IsActive(saturday(3)))
	require.True(t, schedule.IsActive(saturday(5)))
	require.False(t, schedule.IsActive(saturday(6)))
	require.False(t, schedule.IsActive(saturday(23)))
}

func TestWorkTimeSchedule_IsActiveUsesTimezone(t *testing.T) {
	schedule := &WorkTimeSchedule{
		Timezone: "Asia/Shanghai",
		Windows:  []WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "09:00", End: "10:00"}},
	}
	require.NoError(t, schedule.Validate())

	// 02:00 UTC == 10:00 Asia/Shanghai, just past the window.
	utc := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	require.False(t, schedule.IsActive(utc))
	// 01:30 UTC == 09:30 Asia/Shanghai.
	require.True(t, schedule.IsActive(time.Date(2026, 10, 21, 1, 30, 0, 0, time.UTC)))
}

func TestWorkTimeSchedule_IsActiveEmpty(t *testing.T) {
	now := time.Now()
	require.True(t, (*WorkTimeSchedule)(nil).IsActive(now))
	require.True(t, (&WorkTimeSchedule{}).IsActive(now))
}

func TestWorkTimeSchedule_NextStart(t *testing.T) {
	loc := mustLoadLocation(t, "Asia/Shanghai")
	schedule := &WorkTimeSchedule{
		Timezone: "Asia/Shanghai",
		Windows: []WorkTimeWindow{
			{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"},
			{Days: []int{6, 7}, Start: "10:00", End: "12:00"},
		},
	}
	require.NoError(t, schedule.Validate())

	// Wednesday 20:00 -> next start is Thursday 09:00.
	now := time.Date(2026, 10, 21, 20, 0, 0, 0, loc)
	next, ok := schedule.NextStart(now)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 10, 22, 9, 0, 0, 0, loc), next.In(loc))

	// Wednesday 08:00 -> today 09:00.
	now = time.Date(2026, 10, 21, 8, 0, 0, 0, loc)
	next, ok = schedule.NextStart(now)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 10, 21, 9, 0, 0, 0, loc), next.In(loc))

	// Friday 20:00 -> Saturday 10:00.
	now = time.Date(2026, 10, 23, 20, 0, 0, 0, loc)
	next, ok = schedule.NextStart(now)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 10, 24, 10, 0, 0, 0, loc), next.In(loc))

	_, ok = (*WorkTimeSchedule)(nil).NextStart(now)
	require.False(t, ok)
}

func TestWorkTimeSchedule_IsActiveInUsesFallbackLocation(t *testing.T) {
	loc := mustLoadLocation(t, "Asia/Shanghai")
	schedule := &WorkTimeSchedule{
		Windows: []WorkTimeWindow{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "09:00", End: "10:00"}},
	}
	require.NoError(t, schedule.Validate())
	require.Empty(t, schedule.Timezone)

	// 01:30 UTC == 09:30 Asia/Shanghai, inside the window only when the fallback
	// location is applied.
	now := time.Date(2026, 10, 21, 1, 30, 0, 0, time.UTC)
	require.False(t, schedule.IsActive(now))
	require.True(t, schedule.IsActiveIn(now, loc))

	next, ok := schedule.NextStartIn(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), loc)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 10, 21, 9, 0, 0, 0, loc), next.In(loc))
}
