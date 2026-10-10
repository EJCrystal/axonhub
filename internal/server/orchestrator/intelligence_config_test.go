package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
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

// TestRetriableGenerationErrors pins which upstream failures get a second
// attempt. A dropped stream leaves nothing to submit, so it is worth another
// try; a rejected request or a spent deadline is not, because it repeats.
func TestRetriableGenerationErrors(t *testing.T) {
	retriable := []string{
		"No content in stream response",
		"stream error: stream ID 1; INTERNAL_ERROR; received from peer",
		// The signature the relays actually return when they cut the stream short.
		"error: stream_timeout, code: stream_timeout, type: upstream_error",
		"error: The service is temporarily unavailable. Please retry later.",
		"The service is busy. Please retry later.",
		"Upstream request failed",
		"failed to do request: HTTP request failed: connection reset by peer",
		io.ErrUnexpectedEOF.Error(),
	}
	for _, message := range retriable {
		require.True(t, isRetriableGenerationError(errors.New(message)), message)
	}

	notRetriable := []string{
		"",
		"HTTP error 401: invalid api key",
		`{"code":"ACCESS_DENIED","message":"Access denied. Your IP is 204.1.108.244"}`,
		"您的 IP 不在令牌允许访问的列表中",
		"model returned empty HTML",
	}
	for _, message := range notRetriable {
		require.False(t, isRetriableGenerationError(errors.New(message)), message)
	}

	require.False(t, isRetriableGenerationError(nil), "no error is not retried")

	// The run's own deadline is not an upstream problem: the budget is spent, so
	// a retry would be cancelled the same way.
	require.False(t, isRetriableGenerationError(context.DeadlineExceeded))
	require.False(t, isRetriableGenerationError(fmt.Errorf("context deadline exceeded")))
	require.False(t, isRetriableGenerationError(context.Canceled))
}

// TestIntelligenceTargetTimeout covers the per-target budget: a target that
// names its own timeout wins, and anything unset or out of range falls back to
// the built-in default rather than silently shortening or extending the run.
func TestIntelligenceTargetTimeout(t *testing.T) {
	require.Equal(t, intelligenceTotalTimeout, intelligenceTargetTimeout(nil))
	require.Equal(t, intelligenceTotalTimeout, intelligenceTargetTimeout(lo.ToPtr(0)))
	require.Equal(t, intelligenceTotalTimeout, intelligenceTargetTimeout(lo.ToPtr(-5)))
	require.Equal(t, intelligenceTotalTimeout, intelligenceTargetTimeout(lo.ToPtr(intelligenceMaxTimeoutMinutes+1)))

	// A named budget is what the run actually waits for.
	require.Equal(t, 15*time.Minute, intelligenceTargetTimeout(lo.ToPtr(15)))
	require.Equal(t, time.Duration(intelligenceMaxTimeoutMinutes)*time.Minute,
		intelligenceTargetTimeout(lo.ToPtr(intelligenceMaxTimeoutMinutes)))
}

// TestValidateIntelligenceTargetTimeout pins the save-time guard: a budget the
// run could never honour is rejected before it is stored, and the boundary
// values stay accepted.
func TestValidateIntelligenceTargetTimeout(t *testing.T) {
	valid := []objects.IntelligenceTarget{
		{ChannelID: 6, ModelID: "gpt-6.1-sol", APIKey: "sk-a"},
		{ChannelID: 6, ModelID: "gpt-6.1-sol", APIKey: "sk-b", TimeoutMinutes: 0},
		{ChannelID: 6, ModelID: "gpt-6.1-sol", APIKey: "sk-c", TimeoutMinutes: 15},
		{ChannelID: 6, ModelID: "gpt-6.1-sol", APIKey: "sk-d", TimeoutMinutes: intelligenceMaxTimeoutMinutes},
	}
	require.NoError(t, validateIntelligenceTargets(valid))

	for _, minutes := range []int{-1, intelligenceMaxTimeoutMinutes + 1} {
		err := validateIntelligenceTargets([]objects.IntelligenceTarget{
			{ChannelID: 6, ModelID: "gpt-6.1-sol", APIKey: "sk-a", TimeoutMinutes: minutes},
		})
		require.Error(t, err, minutes)
		require.Contains(t, err.Error(), "timeout")
	}
}

// TestNarrowIntelligenceConfig covers the split button: naming a channel must
// keep only that channel's target, and naming one that is not configured has to
// fail loudly rather than silently running everything.
func TestNarrowIntelligenceConfig(t *testing.T) {
	config := &objects.IntelligenceConfig{
		Enabled:         true,
		IntervalMinutes: 30,
		Targets: []objects.IntelligenceTarget{
			{ChannelID: 6, ModelID: "gpt-6.1-sol", ReasoningEffort: "high"},
			{ChannelID: 9, ModelID: "other", ReasoningEffort: "low"},
		},
	}

	narrowed, err := narrowIntelligenceConfig(config, 9, "")
	require.NoError(t, err)
	require.Len(t, narrowed.Targets, 1)
	require.Equal(t, 9, narrowed.Targets[0].ChannelID)
	require.Equal(t, "low", narrowed.Targets[0].ReasoningEffort, "the level travels with the target")

	// The schedule settings survive the narrowing untouched.
	require.True(t, narrowed.Enabled)
	require.Equal(t, 30, narrowed.IntervalMinutes)

	// The original is not mutated, so the scheduled path is unaffected.
	require.Len(t, config.Targets, 2)

	// A channel outside the configuration is an error the caller reports.
	_, err = narrowIntelligenceConfig(config, 404, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

// TestNarrowIntelligenceConfigByKey covers the split button on a channel that
// carries several keys: naming the channel alone would run all of them, so the
// key has to pick one out.
func TestNarrowIntelligenceConfigByKey(t *testing.T) {
	config := &objects.IntelligenceConfig{
		Enabled:         true,
		IntervalMinutes: 60,
		Targets: []objects.IntelligenceTarget{
			{ChannelID: 6, APIKey: "sk-aaa", ModelID: "gpt-6.1-sol", ReasoningEffort: "high"},
			{ChannelID: 6, APIKey: "sk-bbb", ModelID: "gpt-6-astra", ReasoningEffort: "low"},
			{ChannelID: 6, APIKey: "sk-ccc", ModelID: "gpt-5.6-sol"},
		},
	}

	// Without a key the whole channel runs, which is the previous behaviour.
	all, err := narrowIntelligenceConfig(config, 6, "")
	require.NoError(t, err)
	require.Len(t, all.Targets, 3)

	one, err := narrowIntelligenceConfig(config, 6, "sk-bbb")
	require.NoError(t, err)
	require.Len(t, one.Targets, 1)
	require.Equal(t, "gpt-6-astra", one.Targets[0].ModelID)
	require.Equal(t, "low", one.Targets[0].ReasoningEffort)

	// An unknown key is reported, and the message must not leak the secret.
	_, err = narrowIntelligenceConfig(config, 6, "sk-missing-1234567890")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
	require.NotContains(t, err.Error(), "sk-missing-1234567890")
}

// TestFailedRunResults covers the failure row: a run seeded with the key it was
// testing must keep that key when it fails, otherwise the history shows an
// unnamed failure and the operator cannot tell which credential broke.
func TestFailedRunResults(t *testing.T) {
	err := errors.New("context deadline exceeded")

	seeded := []objects.IntelligenceKeyResult{{KeyPrefix: "sk-9****9c31"}}
	results := failedRunResults(7, seeded, 1234, err)
	require.Len(t, results, 1)
	require.Equal(t, "sk-9****9c31", results[0].KeyPrefix, "the key under test survives the failure")
	require.False(t, results[0].Success)
	require.Equal(t, 1234, results[0].DurationMs)
	require.NotNil(t, results[0].Error)
	require.Contains(t, *results[0].Error, "context deadline exceeded")

	// A run that named no key keeps the single anonymous entry, so the failure is
	// still recorded rather than dropped.
	anonymous := failedRunResults(8, nil, 10, err)
	require.Len(t, anonymous, 1)
	require.Empty(t, anonymous[0].KeyPrefix)
	require.NotNil(t, anonymous[0].Error)
}

// TestDegradedKeyPolicyDefaultsOff pins the safe default: the automatic disable
// takes a credential out of the channel's live rotation, so it only happens when
// the operator turns it on.
func TestDegradedKeyPolicyDefaultsOff(t *testing.T) {
	config := defaultIntelligenceConfig()
	require.False(t, config.DisableDegradedKeys, "the automatic disable is opt-in")

	// A config stored before the field existed must not enable it either.
	decoded := defaultIntelligenceConfig()
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true,"intervalMinutes":30}`), decoded))
	require.False(t, decoded.DisableDegradedKeys)
}

// TestDegradedDisableReasonIsItsOwnMarker pins the recovery contract: only keys
// carrying this exact reason are re-enabled, so a key an operator disabled by
// hand is never silently restored.
func TestDegradedDisableReasonIsItsOwnMarker(t *testing.T) {
	require.Equal(t, "intelligence check: degraded", intelligenceDegradedDisableReason)
	require.NotEqual(t, "Manually disabled by user", intelligenceDegradedDisableReason)
}

// TestResolveIntelligenceBenchmark pins what a run records: the resolved name,
// so the history never has to know that an empty value meant pelican.
func TestResolveIntelligenceBenchmark(t *testing.T) {
	require.Equal(t, IntelligenceBenchmarkPelican, resolveIntelligenceBenchmark(""))
	require.Equal(t, IntelligenceBenchmarkPelican, resolveIntelligenceBenchmark("   "))
	require.Equal(t, IntelligenceBenchmarkPelican, resolveIntelligenceBenchmark(IntelligenceBenchmarkPelican))
	require.Equal(t, IntelligenceBenchmarkCandy, resolveIntelligenceBenchmark(IntelligenceBenchmarkCandy))
	require.Equal(t, IntelligenceBenchmarkCandy, resolveIntelligenceBenchmark(" candy "))
}

// TestJudgeCandyAnswer pins the published rule: the answer passes when 21 appears
// as an independent number, and nothing else counts.
func TestJudgeCandyAnswer(t *testing.T) {
	passed := []string{
		"21",
		"答案是 21 个",
		"最少需要取出 21 个糖果",
		"答案是 **21** 个",
		"（21）",
		"需要 21。",
		"9 + 12 = 21",
		"如果取 21 个则必然满足",
	}
	for _, answer := range passed {
		ok, _ := judgeCandyAnswer(answer)
		require.True(t, ok, answer)
	}

	// A number that merely contains 21 is not the answer, and neither is a wrong
	// number. This is what keeps a plausible-looking essay from passing.
	failed := []string{
		"",
		"   ",
		"20",
		"22",
		"210",
		"121",
		"取 20 个即可",
		"答案是 211 个",
		"一共 3210 个",
		"没有确定的答案",
	}
	for _, answer := range failed {
		ok, _ := judgeCandyAnswer(answer)
		require.False(t, ok, answer)
	}

	// The reason explains a failure, so the history reads as a wrong answer.
	_, reason := judgeCandyAnswer("20")
	require.Contains(t, reason, "21")
}

// TestAllowedIntelligenceBenchmarks pins the checks a target may run: the two
// published ones, plus the empty value that means the pelican default.
func TestAllowedIntelligenceBenchmarks(t *testing.T) {
	require.Equal(t, []string{"pelican", "candy"}, allowedIntelligenceBenchmarks)

	require.True(t, isAllowedIntelligenceBenchmark(""), "unset means pelican")
	require.True(t, isAllowedIntelligenceBenchmark("   "), "whitespace counts as unset")
	require.True(t, isAllowedIntelligenceBenchmark(IntelligenceBenchmarkPelican))
	require.True(t, isAllowedIntelligenceBenchmark(IntelligenceBenchmarkCandy))
	require.True(t, isAllowedIntelligenceBenchmark(" candy "), "surrounding space is tolerated")

	// An unknown value must be rejected: falling back to pelican would look like a
	// passing check when the operator asked for the candy question.
	for _, benchmark := range []string{"Candy", "candies", "sweeties", "0"} {
		require.False(t, isAllowedIntelligenceBenchmark(benchmark), benchmark)
	}
}

// TestTargetConcurrencyForRun covers how many targets a run starts at once. A
// manual run has to start every configured target the operator selected, up to
// the ceiling, while the scheduled run keeps the lower default so an unattended
// check does not open many slow streams at once.
func TestTargetConcurrencyForRun(t *testing.T) {
	// Manual runs follow the configuration, so "test now" starts everything.
	require.Equal(t, 1, targetConcurrencyForRun(1, intelligenceManualTrigger))
	require.Equal(t, 3, targetConcurrencyForRun(3, intelligenceManualTrigger))
	require.Equal(t, 4, targetConcurrencyForRun(4, intelligenceManualTrigger))
	require.Equal(t, 10, targetConcurrencyForRun(10, intelligenceManualTrigger))

	// The ceiling still applies: a large configuration does not open an unbounded
	// number of simultaneous streams.
	require.Equal(t, intelligenceTargetConcurrencyCeiling,
		targetConcurrencyForRun(50, intelligenceManualTrigger))

	// An empty selection has nothing to scale to, so it falls back to the default.
	require.Equal(t, intelligenceMaxTargetConcurrency,
		targetConcurrencyForRun(0, intelligenceManualTrigger))

	// The scheduled path is deliberately unchanged.
	require.Equal(t, intelligenceMaxTargetConcurrency,
		targetConcurrencyForRun(10, intelligenceScheduledTrigger))

	// The ceiling may not exceed the per-key cap, otherwise one target could not
	// use the budget the run granted it.
	require.LessOrEqual(t, intelligenceTargetConcurrencyCeiling, intelligenceMaxConcurrency)
}

// TestIntelligencePendingResults covers the seeded running row: a target that
// names a key records its masked prefix immediately, so the history can show
// which key is under test instead of an empty cell. A target covering every key
// has nothing single to name and starts empty.
func TestIntelligencePendingResults(t *testing.T) {
	seeded := intelligencePendingResults("sk-1234567890abcdef")
	require.Len(t, seeded, 1)
	require.Equal(t, maskIntelligenceTargetKey("sk-1234567890abcdef"), seeded[0].KeyPrefix)
	require.NotContains(t, seeded[0].KeyPrefix, "567890abcdef", "the seed must not leak the secret")

	require.Empty(t, intelligencePendingResults(""), "an all-keys target has nothing to name")
	require.Empty(t, intelligencePendingResults("   "), "whitespace counts as unnamed")
}

// TestRunsToTrimPerChannel pins the retention rule: the newest limit runs of a
// channel survive and everything older is dropped, whatever key it covered, so
// adding keys to a channel does not multiply the rows kept.
func TestRunsToTrimPerChannel(t *testing.T) {
	// Runs are collected newest first, which is the order the query returns.
	runs := make([]trimRun, 0, 13)
	for id := 13; id >= 1; id-- {
		runs = append(runs, trimRun{ID: id})
	}

	require.Equal(t, []int{3, 2, 1}, runsToTrim(runs, 10))

	// Newer runs are the ones that survive, so the ids dropped are the tail.
	require.Equal(t, []int{3, 2}, runsToTrim(runs[:12], 10))

	// At or below the bound nothing is dropped.
	require.Empty(t, runsToTrim(runs[:10], 10))
	require.Empty(t, runsToTrim([]trimRun{{ID: 1}}, 10))
	require.Empty(t, runsToTrim(nil, 10))

	// A degenerate limit drops everything rather than panicking.
	require.Len(t, runsToTrim(runs, 0), 13)
}

// TestValidateIntelligenceTargets covers the target rules: several rows may
// share a channel so each key gets its own model and thinking level, but a key
// must be named and must not be configured twice.
func TestValidateIntelligenceTargets(t *testing.T) {
	target := func(channelID int, apiKey, model, effort string) objects.IntelligenceTarget {
		return objects.IntelligenceTarget{ChannelID: channelID, APIKey: apiKey, ModelID: model, ReasoningEffort: effort}
	}

	t.Run("one channel may carry several keys, each with its own model and level", func(t *testing.T) {
		err := validateIntelligenceTargets([]objects.IntelligenceTarget{
			target(6, "sk-aaa", "gpt-6.1-sol", "high"),
			target(6, "sk-bbb", "gpt-6-astra", "low"),
			target(6, "sk-ccc", "gpt-5.6-sol", ""),
		})
		require.NoError(t, err)
	})

	t.Run("the same key twice on a channel is rejected", func(t *testing.T) {
		err := validateIntelligenceTargets([]objects.IntelligenceTarget{
			target(6, "sk-aaa", "gpt-6.1-sol", ""),
			target(6, "sk-aaa", "gpt-6-astra", "high"),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "configured twice")
	})

	t.Run("the same key on different channels is fine", func(t *testing.T) {
		err := validateIntelligenceTargets([]objects.IntelligenceTarget{
			target(6, "sk-aaa", "m", ""),
			target(2, "sk-aaa", "m", ""),
		})
		require.NoError(t, err)
	})

	t.Run("an empty key is rejected", func(t *testing.T) {
		for _, key := range []string{"", "   "} {
			err := validateIntelligenceTargets([]objects.IntelligenceTarget{target(6, key, "m", "")})
			require.Error(t, err, "key %q must be rejected", key)
			require.Contains(t, err.Error(), "API key")
		}
	})

	t.Run("the other required fields still apply", func(t *testing.T) {
		require.Error(t, validateIntelligenceTargets([]objects.IntelligenceTarget{target(0, "sk-a", "m", "")}))
		require.Error(t, validateIntelligenceTargets([]objects.IntelligenceTarget{target(6, "sk-a", "", "")}))
		require.Error(t, validateIntelligenceTargets([]objects.IntelligenceTarget{target(6, "sk-a", "m", "ultra")}))
		require.NoError(t, validateIntelligenceTargets(nil))
	})

	t.Run("the error does not echo the whole secret", func(t *testing.T) {
		secret := "sk-1234567890abcdef"
		err := validateIntelligenceTargets([]objects.IntelligenceTarget{
			target(6, secret, "m", ""),
			target(6, secret, "m", ""),
		})
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret)
	})
}

// TestFinalizeRunRecordsOutcome covers the two-step lifecycle: the row is
// created as running so the history can show it, then finalized with the
// outcome. A run that failed before producing results must still end up with a
// terminal status and an explanation, never left running.
func TestFinalizeRunRecordsOutcome(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:finalize-run?mode=memory&_fk=1")
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	service := &IntelligenceService{ent: client}

	start := func() *ent.IntelligenceRun {
		run, err := client.IntelligenceRun.Create().
			SetChannelID(6).
			SetChannelName("chan").
			SetModelID("m").
			SetTrigger("manual").
			SetStatus(intelligencerun.StatusRunning).
			Save(ctx)
		require.NoError(t, err)
		return run
	}

	t.Run("a healthy run ends succeeded with its counters", func(t *testing.T) {
		run := start()
		require.Equal(t, intelligencerun.StatusRunning, run.Status, "a fresh row is running")

		results := []objects.IntelligenceKeyResult{{KeyPrefix: "sk-a", Success: true, Quality: "normal"}}
		require.NoError(t, service.finalizeRun(ctx, run.ID, results, 1234, nil))

		got, err := client.IntelligenceRun.Get(ctx, run.ID)
		require.NoError(t, err)
		require.Equal(t, intelligencerun.StatusSucceeded, got.Status)
		require.Equal(t, 1, got.SuccessKeys)
		require.Equal(t, 0, got.FailedKeys)
		require.Equal(t, 1234, got.DurationMs)
	})

	t.Run("a run that could not start ends failed with a reason", func(t *testing.T) {
		run := start()
		require.NoError(t, service.finalizeRun(ctx, run.ID, nil, 42, errors.New("upstream refused")))

		got, err := client.IntelligenceRun.Get(ctx, run.ID)
		require.NoError(t, err)
		require.Equal(t, intelligencerun.StatusFailed, got.Status)
		require.Len(t, got.Results, 1)
		require.NotNil(t, got.Results[0].Error)
		require.Contains(t, *got.Results[0].Error, "upstream refused")
	})
}

// TestReapInterruptedRuns closes rows left running by a restart, which nothing
// else can finish.
func TestReapInterruptedRuns(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:reap-runs?mode=memory&_fk=1")
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	service := &IntelligenceService{ent: client}

	stale, err := client.IntelligenceRun.Create().SetChannelID(1).SetModelID("m").
		SetStatus(intelligencerun.StatusRunning).Save(ctx)
	require.NoError(t, err)
	done, err := client.IntelligenceRun.Create().SetChannelID(1).SetModelID("m").
		SetStatus(intelligencerun.StatusSucceeded).Save(ctx)
	require.NoError(t, err)

	require.NoError(t, service.reapInterruptedRuns(ctx))

	reaped, err := client.IntelligenceRun.Get(ctx, stale.ID)
	require.NoError(t, err)
	require.Equal(t, intelligencerun.StatusFailed, reaped.Status)
	require.Len(t, reaped.Results, 1, "the reason is recorded")

	kept, err := client.IntelligenceRun.Get(ctx, done.ID)
	require.NoError(t, err)
	require.Equal(t, intelligencerun.StatusSucceeded, kept.Status, "a finished run is untouched")
}

// TestRunTargetsConcurrently covers the fan-out: targets must run side by side
// rather than one after another, the limit must be respected, and a target that
// fails must not stop the rest.
func TestRunTargetsConcurrently(t *testing.T) {
	target := func(id int) objects.IntelligenceTarget {
		return objects.IntelligenceTarget{ChannelID: id, APIKey: "sk-a", ModelID: "m"}
	}

	t.Run("targets overlap instead of queueing", func(t *testing.T) {
		// Every target waits for all of them to have started. A sequential loop
		// would deadlock and time out, which is the behaviour being ruled out.
		var mu sync.Mutex
		started := 0
		allStarted := make(chan struct{})

		done := make(chan struct{})
		go func() {
			defer close(done)
			runTargetsConcurrently(context.Background(), []objects.IntelligenceTarget{target(1), target(2), target(3)}, 3,
				func(_ context.Context, _ objects.IntelligenceTarget) {
					mu.Lock()
					started++
					if started == 3 {
						close(allStarted)
					}
					mu.Unlock()

					<-allStarted
				})
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("targets did not run concurrently")
		}
	})

	t.Run("the limit is not exceeded", func(t *testing.T) {
		var mu sync.Mutex
		inFlight, peak := 0, 0

		runTargetsConcurrently(context.Background(), []objects.IntelligenceTarget{target(1), target(2), target(3), target(4), target(5)}, 2,
			func(_ context.Context, _ objects.IntelligenceTarget) {
				mu.Lock()
				inFlight++
				if inFlight > peak {
					peak = inFlight
				}
				mu.Unlock()

				time.Sleep(30 * time.Millisecond)

				mu.Lock()
				inFlight--
				mu.Unlock()
			})

		require.LessOrEqual(t, peak, 2, "at most limit targets run at once")
		require.Equal(t, 2, peak, "and the limit is actually used")
	})

	t.Run("every target runs even when one fails", func(t *testing.T) {
		var mu sync.Mutex
		seen := map[int]bool{}

		runTargetsConcurrently(context.Background(), []objects.IntelligenceTarget{target(1), target(2), target(3)}, 2,
			func(_ context.Context, target objects.IntelligenceTarget) {
				mu.Lock()
				seen[target.ChannelID] = true
				mu.Unlock()
			})

		require.Len(t, seen, 3, "a failing target must not cancel the others")
	})

	t.Run("an empty configuration is harmless", func(t *testing.T) {
		runTargetsConcurrently(context.Background(), nil, 3, func(context.Context, objects.IntelligenceTarget) {
			t.Fatal("nothing to run")
		})
	})
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
