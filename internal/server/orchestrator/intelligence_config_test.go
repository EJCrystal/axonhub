package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
)

func TestDefaultIntelligenceConfig(t *testing.T) {
	config := defaultIntelligenceConfig()
	require.False(t, config.Enabled)
	require.Equal(t, 60, config.IntervalMinutes)
	require.Empty(t, config.Targets)
}

// TestAllowedIntelligenceIntervals pins the intervals the settings UI offers;
// anything else has to be rejected before it reaches the scheduler.
func TestAllowedIntelligenceIntervals(t *testing.T) {
	require.Equal(t, []int{10, 30, 60}, allowedIntelligenceIntervals)

	for _, minutes := range allowedIntelligenceIntervals {
		require.True(t, isAllowedInterval(minutes))
	}

	for _, minutes := range []int{-1, 0, 5, 15, 45, 120} {
		require.False(t, isAllowedInterval(minutes))
	}
}

func TestRunStatus(t *testing.T) {
	tests := []struct {
		name    string
		total   int
		success int
		want    objects.IntelligenceRunStatus
	}{
		{"all keys passed", 2, 2, objects.IntelligenceRunSucceeded},
		{"some keys passed", 2, 1, objects.IntelligenceRunPartial},
		{"no key passed", 2, 0, objects.IntelligenceRunFailed},
		// A run with no evaluated key verified nothing, so it is a failure
		// rather than a pass.
		{"no keys at all", 0, 0, objects.IntelligenceRunFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, runStatus(tt.total, tt.success))
		})
	}
}
