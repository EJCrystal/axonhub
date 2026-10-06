package orchestrator

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/intelligencerun"
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
	key := func(success bool, quality string) objects.IntelligenceKeyResult {
		return objects.IntelligenceKeyResult{Success: success, Quality: quality}
	}

	tests := []struct {
		name    string
		results []objects.IntelligenceKeyResult
		want    objects.IntelligenceRunStatus
	}{
		{"all keys normal", []objects.IntelligenceKeyResult{key(true, "normal"), key(true, "normal")}, objects.IntelligenceRunSucceeded},
		{"one normal and one degraded", []objects.IntelligenceKeyResult{key(true, "normal"), key(true, "degraded")}, objects.IntelligenceRunPartial},
		{"all keys degraded", []objects.IntelligenceKeyResult{key(true, "degraded"), key(true, "degraded")}, objects.IntelligenceRunFailed},
		// A key that never produced an answer has no verdict, so it cannot count
		// as a pass even though the run completed.
		{"a failed key drags the run down", []objects.IntelligenceKeyResult{key(true, "normal"), key(false, "")}, objects.IntelligenceRunPartial},
		{"an inconclusive key counts as no verdict", []objects.IntelligenceKeyResult{key(true, "unknown")}, objects.IntelligenceRunFailed},
		// Nothing verified is a failure rather than a pass.
		{"no keys at all", nil, objects.IntelligenceRunFailed},
		{"only failed keys", []objects.IntelligenceKeyResult{key(false, ""), key(false, "")}, objects.IntelligenceRunFailed},
		// An operator's verdict is what lets a scoring failure be closed out.
		{"a manual normal lifts the run", []objects.IntelligenceKeyResult{{Success: false, ManualVerdict: "normal"}}, objects.IntelligenceRunSucceeded},
		{"a manual degraded marks the run", []objects.IntelligenceKeyResult{{Success: false, ManualVerdict: "degraded"}}, objects.IntelligenceRunFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, runStatus(tt.results))
		})
	}
}

// TestSetManualVerdictRecordsOperatorDecision covers the case automatic scoring
// cannot: the detection service was unreachable, so an operator judges the
// source and the run has to end up reflecting that decision.
func TestSetManualVerdictRecordsOperatorDecision(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:manual-verdict?mode=memory&_fk=1")
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))

	run, err := client.IntelligenceRun.Create().
		SetChannelID(1).
		SetChannelName("chan").
		SetModelID("gpt-6.1-sol").
		SetTrigger("manual").
		SetStatus(intelligencerun.StatusFailed).
		SetTotalKeys(1).
		SetSuccessKeys(0).
		SetFailedKeys(1).
		SetResults([]objects.IntelligenceKeyResult{{
			KeyPrefix: "sk-9****9c31",
			Success:   false,
			HTML:      "<html>x</html>",
			Error:     lo.ToPtr("failed to create evaluation task"),
		}}).
		Save(ctx)
	require.NoError(t, err)

	service := &IntelligenceService{ent: client}

	updated, err := service.SetManualVerdict(ctx, run.ID, "sk-9****9c31", objects.IntelligenceManualVerdictDegraded)
	require.NoError(t, err)
	require.Equal(t, intelligencerun.StatusFailed, updated.Status)
	require.Equal(t, 0, updated.SuccessKeys)
	require.Equal(t, objects.IntelligenceManualVerdictDegraded, updated.Results[0].ManualVerdict)

	// A manual "normal" is what actually flips the run, and the counters follow.
	updated, err = service.SetManualVerdict(ctx, run.ID, "sk-9****9c31", objects.IntelligenceManualVerdictNormal)
	require.NoError(t, err)
	require.Equal(t, intelligencerun.StatusSucceeded, updated.Status)
	require.Equal(t, 1, updated.SuccessKeys)
	require.Equal(t, 0, updated.FailedKeys)

	// Clearing it puts the run back where automatic scoring left it.
	updated, err = service.SetManualVerdict(ctx, run.ID, "sk-9****9c31", "")
	require.NoError(t, err)
	require.Equal(t, intelligencerun.StatusFailed, updated.Status)
	require.Empty(t, updated.Results[0].ManualVerdict)
}

func TestSetManualVerdictRejectsBadInput(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:manual-verdict-bad?mode=memory&_fk=1")
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))

	run, err := client.IntelligenceRun.Create().
		SetChannelID(1).
		SetModelID("m").
		SetStatus(intelligencerun.StatusFailed).
		SetResults([]objects.IntelligenceKeyResult{{KeyPrefix: "sk-a", Success: false}}).
		Save(ctx)
	require.NoError(t, err)

	service := &IntelligenceService{ent: client}

	_, err = service.SetManualVerdict(ctx, run.ID, "sk-a", "nonsense")
	require.Error(t, err, "an unknown verdict must be rejected")

	_, err = service.SetManualVerdict(ctx, run.ID, "sk-missing", objects.IntelligenceManualVerdictNormal)
	require.Error(t, err, "an unknown key must be rejected")

	_, err = service.SetManualVerdict(ctx, run.ID, "   ", objects.IntelligenceManualVerdictNormal)
	require.Error(t, err, "an empty key prefix must be rejected")
}
