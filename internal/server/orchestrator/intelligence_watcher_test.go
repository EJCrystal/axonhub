package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhenzou/executors"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/scheduler"
)

const watcherCronFiveMinutes = `{"enabled":true,"cronExpression":"0 */5 * * *","targets":[]}`

// newIntelligenceServiceWithScheduler builds the service the way the app does,
// with a live scheduler, so the configuration watcher can be driven without a
// running server.
func newIntelligenceServiceWithScheduler(t *testing.T, file string) (*IntelligenceService, *scheduler.Scheduler, context.Context) {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", "file:"+file+"?mode=memory&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	ctx := ent.NewContext(context.Background(), client)
	ctx = authz.WithTestBypass(ctx)

	exec := executors.NewPoolScheduleExecutor(executors.WithMaxConcurrent(1))
	t.Cleanup(func() { _ = exec.Shutdown(context.Background()) })

	sched := scheduler.New(exec)
	t.Cleanup(func() { sched.Shutdown(context.Background()) })

	sysSvc := biz.NewSystemService(biz.SystemServiceParams{
		CacheConfig: xcache.Config{Mode: xcache.ModeMemory},
		Ent:         client,
	})

	service := &IntelligenceService{
		ent:           client,
		systemService: sysSvc,
		scheduler:     sched,
	}

	return service, sched, ctx
}

// registeredCronOf reports the cron the scheduler currently holds for the
// intelligence task, which is what the console shows as the next execution.
func registeredCronOf(t *testing.T, sched *scheduler.Scheduler) string {
	t.Helper()

	for _, info := range sched.List() {
		if info.Spec.Name == intelligenceSchedulerTask {
			return info.Spec.CronExpr
		}
	}

	return ""
}

// TestConfigWatcherAppliesCronWrittenOutsideTheConsole is the regression this
// watcher exists for: editing the configuration row directly (a restore, a
// migration, another replica, an operator in psql) used to leave the live task
// on the old cron until the process restarted, so the running schedule silently
// disagreed with the stored value.
func TestConfigWatcherAppliesCronWrittenOutsideTheConsole(t *testing.T) {
	service, sched, ctx := newIntelligenceServiceWithScheduler(t, "watcher-applies")

	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey, watcherCronFiveMinutes))
	require.NoError(t, service.RegisterScheduledTasks(ctx, sched))
	require.Equal(t, "0 */5 * * *", registeredCronOf(t, sched), "the task starts on the stored cron")

	// Rewritten in the store with nothing telling the scheduler directly, so the
	// watcher is the only thing that can notice.
	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey,
		`{"enabled":true,"cronExpression":"*/7 * * * *","targets":[]}`))

	require.Equal(t, "0 */5 * * *", registeredCronOf(t, sched), "the live task is untouched until the watcher runs")

	service.syncIntelligenceCron(ctx)

	require.Equal(t, "*/7 * * * *", registeredCronOf(t, sched), "the watcher must adopt the stored cron")
}

// TestConfigWatcherLeavesAMatchingTaskAlone guards the cheap path: an unchanged
// configuration must not reschedule the task on every tick.
func TestConfigWatcherLeavesAMatchingTaskAlone(t *testing.T) {
	service, sched, ctx := newIntelligenceServiceWithScheduler(t, "watcher-idle")

	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey, watcherCronFiveMinutes))
	require.NoError(t, service.RegisterScheduledTasks(ctx, sched))

	service.syncIntelligenceCron(ctx)

	require.Equal(t, "0 */5 * * *", registeredCronOf(t, sched), "an unchanged cron stays put")
}

// TestConfigWatcherIgnoresAnUnreadableConfiguration keeps a corrupt row from
// taking the running schedule down with it. Falling back to the default would
// move a live schedule nobody asked to move.
func TestConfigWatcherIgnoresAnUnreadableConfiguration(t *testing.T) {
	service, sched, ctx := newIntelligenceServiceWithScheduler(t, "watcher-bad")

	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey, watcherCronFiveMinutes))
	require.NoError(t, service.RegisterScheduledTasks(ctx, sched))

	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey, `{not json`))

	service.syncIntelligenceCron(ctx)

	require.Equal(t, "0 */5 * * *", registeredCronOf(t, sched), "a parse failure must not reschedule")
}

// TestConfigWatcherStartsOnceAndStops pins the lifecycle: registration starts the
// loop, a repeated start does not multiply it, and stopping ends it.
func TestConfigWatcherStartsOnceAndStops(t *testing.T) {
	service, sched, ctx := newIntelligenceServiceWithScheduler(t, "watcher-lifecycle")

	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey, watcherCronFiveMinutes))
	require.NoError(t, service.RegisterScheduledTasks(ctx, sched))
	require.NotNil(t, service.watcherCancel, "registration starts the watcher")

	service.startConfigWatcher(ctx)
	require.NotNil(t, service.watcherCancel, "the watcher is still running after a second start")

	service.stopConfigWatcher()
	require.Nil(t, service.watcherCancel, "stopping clears the cancel func")

	// A value written after the stop must not be picked up; the next process start
	// is what applies it.
	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey,
		`{"enabled":true,"cronExpression":"*/9 * * * *","targets":[]}`))
	require.Equal(t, "0 */5 * * *", registeredCronOf(t, sched), "a stopped watcher must not reschedule")
}

// TestRegisteredCronToleratesAnUnknownTask makes the watcher's read safe before
// the task exists: an empty answer is what makes it register the stored value.
func TestRegisteredCronToleratesAnUnknownTask(t *testing.T) {
	service, _, _ := newIntelligenceServiceWithScheduler(t, "watcher-unknown")

	require.Empty(t, service.registeredCron())
}

// TestIntelligenceConfigKeyIsStable pins the storage key the watcher reads: a
// typo here would silently stop the alignment from ever firing.
func TestIntelligenceConfigKeyIsStable(t *testing.T) {
	require.Equal(t, "intelligence_check_config", IntelligenceConfigKey)
}
