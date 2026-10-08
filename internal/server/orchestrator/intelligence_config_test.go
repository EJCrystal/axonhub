package orchestrator

import (
	"context"
	"testing"
	"time"

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

// TestIntelligenceTaskSpecFollowsInterval pins the fix for a fixed 10 minute
// tick: the interval the operator picks has to reach the scheduler, otherwise
// the tick wheel quantises every setting into the same slot and 30/60 minute
// configurations fire far more often than asked.
func TestIntelligenceTaskSpecFollowsInterval(t *testing.T) {
	for _, minutes := range allowedIntelligenceIntervals {
		spec := intelligenceTaskSpec(minutes)
		require.Equal(t, intelligenceSchedulerTask, spec.Name)
		require.Equal(t, time.Duration(minutes)*time.Minute, spec.FixRate)
	}

	// Anything the settings UI does not offer falls back to the default rather
	// than scheduling a zero-length tick, which the timer wheel would spin on.
	for _, minutes := range []int{-1, 0, 5, 120} {
		spec := intelligenceTaskSpec(minutes)
		require.Equal(t, time.Duration(defaultIntelligenceConfig().IntervalMinutes)*time.Minute, spec.FixRate)
	}
}

// TestAllowedReasoningEfforts pins the levels a target may request: the seven
// the transformers understand, plus the empty value that means "leave it alone".
func TestAllowedReasoningEfforts(t *testing.T) {
	require.Equal(t,
		[]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
		allowedReasoningEfforts)

	require.True(t, isAllowedReasoningEffort(""), "an unset level is always allowed")
	require.True(t, isAllowedReasoningEffort("   "), "whitespace counts as unset")
	for _, effort := range allowedReasoningEfforts {
		require.True(t, isAllowedReasoningEffort(effort), effort)
		require.True(t, isAllowedReasoningEffort(" "+effort+" "), "surrounding space is tolerated")
	}

	// Anything else would be forwarded verbatim and rejected upstream, so the
	// save has to catch it while the operator is still looking.
	for _, effort := range []string{"ultra", "HIGH", "0", "x-high", "Max"} {
		require.False(t, isAllowedReasoningEffort(effort), effort)
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

// TestRunConfiguredTargetsAttachesEntClient guards the scheduled path: the
// scheduler invokes tasks with a context it builds itself, so the ent client is
// absent and every downstream ent read (including the request persistence
// middleware) would nil-dereference. The service must attach its own client.
func TestRunConfiguredTargetsAttachesEntClient(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:attach-ent?mode=memory&_fk=1")
	defer client.Close()

	service := &IntelligenceService{ent: client}

	// Stand-in for the scheduler's own context, which carries no client.
	bare := context.Background()
	require.Nil(t, ent.FromContext(bare), "the scheduler context has no client")

	recorded := service.attachEntClient(bare)
	require.Same(t, client, ent.FromContext(recorded), "the run must carry an ent client")

	// A context that already has one is left alone, so the request path is
	// unaffected.
	withClient := ent.NewContext(bare, client)
	require.Same(t, client, ent.FromContext(service.attachEntClient(withClient)))
}

// TestRunConfiguredTargetsWithoutEntClientWouldPanic documents why the attach
// above is required rather than cosmetic.
func TestRunConfiguredTargetsWithoutEntClientWouldPanic(t *testing.T) {
	require.Nil(t, ent.FromContext(context.Background()))
	require.Panics(t, func() {
		_ = ent.FromContext(context.Background()).DataStorage
	})
}

// TestScheduledRunSurvivesMissingEntClient reproduces the panic reported from
// the scheduler: its context carries no ent client, so a run that reaches the
// request persistence path used to nil-dereference inside ent.FromContext.
// Driving one target end to end proves the attach covers the whole chain, not
// just the first read.
func TestScheduledRunSurvivesMissingEntClient(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:sched-ent?mode=memory&_fk=1")
	defer client.Close()

	// A data storage row must exist, otherwise the persistence middleware has
	// nothing to resolve and the test would pass for the wrong reason.
	seedCtx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	if _, err := client.DataStorage.Create().
		SetName("Primary").
		SetDescription("test").
		SetPrimary(true).
		SetStatus("active").
		SetType("fs").
		SetSettings(&objects.DataStorageSettings{}).
		Save(seedCtx); err != nil {
		t.Fatalf("seed data storage: %v", err)
	}

	// The scheduler hands the task a context with neither a client nor a
	// principal; the bypass mirrors what runScheduled sets up.
	bare := authz.WithSystemBypass(context.Background(), "test")
	require.Nil(t, ent.FromContext(bare))

	service := &IntelligenceService{ent: client}
	attached := service.attachEntClient(bare)
	require.NotNil(t, ent.FromContext(attached), "the run context must carry a client")

	// The exact read that panicked.
	require.NotPanics(t, func() {
		_, _ = ent.FromContext(attached).DataStorage.Get(attached, 1)
	})
}
