package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"entgo.io/contrib/entgql"
	"github.com/samber/lo"
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/intelligencerun"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/scheduler"
)

const (
	// IntelligenceConfigKey is the system key holding the scheduled check
	// configuration.
	IntelligenceConfigKey = "intelligence_check_config"

	// intelligenceMaxRunsPerChannel bounds the history kept per channel.
	intelligenceMaxRunsPerChannel = 100

	// intelligenceSchedulerTask is the name of the scheduled task.
	intelligenceSchedulerTask = "intelligence-check"
)

// allowedIntelligenceIntervals are the intervals the configuration accepts.
var allowedIntelligenceIntervals = []int{10, 30, 60}

// defaultIntelligenceConfig is used until the admin saves a configuration.
func defaultIntelligenceConfig() *objects.IntelligenceConfig {
	return &objects.IntelligenceConfig{
		Enabled:         false,
		IntervalMinutes: 60,
		Targets:         []objects.IntelligenceTarget{},
	}
}

// IntelligenceService owns the intelligence check configuration, the scheduled
// runs, and the per-channel history.
type IntelligenceService struct {
	ent           *ent.Client
	systemService *biz.SystemService
	channelSvc    *biz.ChannelService
	testSvc       *TestChannelOrchestrator
	scheduler     *scheduler.Scheduler
}

type IntelligenceServiceParams struct {
	fx.In

	Ent           *ent.Client
	SystemService *biz.SystemService
	ChannelSvc    *biz.ChannelService
	TestSvc       *TestChannelOrchestrator
	Scheduler     *scheduler.Scheduler
}

func NewIntelligenceService(params IntelligenceServiceParams) *IntelligenceService {
	return &IntelligenceService{
		ent:           params.Ent,
		systemService: params.SystemService,
		channelSvc:    params.ChannelSvc,
		testSvc:       params.TestSvc,
		scheduler:     params.Scheduler,
	}
}

// RegisterScheduledTasks wires the check into the scheduler. The task always
// registers; the handler itself honours the Enabled flag so toggling the
// configuration does not need a restart.
func (s *IntelligenceService) RegisterScheduledTasks(ctx context.Context, sched *scheduler.Scheduler) error {
	return sched.Register(ctx, scheduler.TaskSpec{
		Name:        intelligenceSchedulerTask,
		Description: "Run the configured intelligence checks",
		FixRate:     10 * time.Minute,
	}, s.runScheduled)
}

// IntelligenceConfig returns the stored configuration, falling back to the
// default when nothing has been saved yet.
func (s *IntelligenceService) IntelligenceConfig(ctx context.Context) (*objects.IntelligenceConfig, error) {
	value, err := s.systemService.SystemValue(ctx, IntelligenceConfigKey)
	if err != nil {
		// Nothing saved yet is the normal first-run state, so fall back to the
		// defaults instead of surfacing a missing system row as a failure.
		if ent.IsNotFound(err) {
			return defaultIntelligenceConfig(), nil
		}
		return nil, err
	}
	if strings.TrimSpace(value) == "" {
		return defaultIntelligenceConfig(), nil
	}

	config := defaultIntelligenceConfig()
	if err := json.Unmarshal([]byte(value), config); err != nil {
		log.Warn(ctx, "failed to decode intelligence config, using defaults", log.Cause(err))
		return defaultIntelligenceConfig(), nil
	}

	return config, nil
}

// SetIntelligenceConfig validates and persists the configuration, then aligns
// the scheduler with the new interval.
func (s *IntelligenceService) SetIntelligenceConfig(ctx context.Context, config *objects.IntelligenceConfig) error {
	if config == nil {
		return fmt.Errorf("configuration is required")
	}

	if !isAllowedInterval(config.IntervalMinutes) {
		return fmt.Errorf("interval must be one of %v minutes", allowedIntelligenceIntervals)
	}

	seen := make(map[int]struct{}, len(config.Targets))
	for _, target := range config.Targets {
		if target.ChannelID <= 0 {
			return fmt.Errorf("every target needs a channel")
		}
		if strings.TrimSpace(target.ModelID) == "" {
			return fmt.Errorf("every target needs a model")
		}
		if _, ok := seen[target.ChannelID]; ok {
			return fmt.Errorf("channel %d is configured twice", target.ChannelID)
		}
		seen[target.ChannelID] = struct{}{}
	}

	body, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to encode configuration: %w", err)
	}

	if err := s.systemService.SetSystemValue(ctx, IntelligenceConfigKey, string(body)); err != nil {
		return err
	}

	// The task runs on a fixed tick; the interval is enforced inside the run so
	// a configuration change takes effect without rescheduling.
	return nil
}

// runScheduled executes a configured run when the schedule is due.
func (s *IntelligenceService) runScheduled(ctx context.Context) {
	config, err := s.IntelligenceConfig(ctx)
	if err != nil {
		log.Error(ctx, "intelligence check: failed to read configuration", log.Cause(err))
		return
	}
	if !config.Enabled || len(config.Targets) == 0 {
		return
	}

	if !s.dueForRun(ctx, config.IntervalMinutes) {
		return
	}

	runCtx := authz.WithSystemBypass(context.WithoutCancel(ctx), "intelligence-check")
	s.runConfiguredTargets(runCtx, config, "scheduled")
}

// dueForRun reports whether enough time has passed since the newest recorded
// run. The scheduler ticks every 10 minutes, so this keeps 30 and 60 minute
// intervals honest.
func (s *IntelligenceService) dueForRun(ctx context.Context, intervalMinutes int) bool {
	latest, err := s.ent.IntelligenceRun.Query().
		Order(ent.Desc(intelligencerun.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return true
		}
		log.Warn(ctx, "intelligence check: failed to read last run", log.Cause(err))
		return true
	}

	return time.Since(latest.CreatedAt) >= time.Duration(intervalMinutes)*time.Minute
}

// RunManual evaluates every configured target immediately, whatever the
// schedule says.
func (s *IntelligenceService) RunManual(ctx context.Context) (int, error) {
	config, err := s.IntelligenceConfig(ctx)
	if err != nil {
		return 0, err
	}
	if len(config.Targets) == 0 {
		return 0, fmt.Errorf("no channel is configured for the intelligence check")
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error(context.Background(), "intelligence check: manual run panicked", log.Any("panic", r))
			}
		}()

		runCtx := authz.WithSystemBypass(context.WithoutCancel(ctx), "intelligence-check-manual")
		s.runConfiguredTargets(runCtx, config, "manual")
	}()

	return len(config.Targets), nil
}

// runConfiguredTargets evaluates every target, one channel at a time so a run
// does not fan out past the upstream concurrency the check already uses.
func (s *IntelligenceService) runConfiguredTargets(ctx context.Context, config *objects.IntelligenceConfig, trigger string) {
	for _, target := range config.Targets {
		if err := s.runOneChannel(ctx, target, trigger); err != nil {
			log.Error(ctx, "intelligence check: channel run failed",
				log.Int("channel_id", target.ChannelID), log.Cause(err))
		}
	}
}

// runOneChannel evaluates every enabled key of one channel and records the run.
func (s *IntelligenceService) runOneChannel(ctx context.Context, target objects.IntelligenceTarget, trigger string) error {
	startedAt := time.Now()

	channel, err := s.channelSvc.GetChannel(ctx, target.ChannelID)
	if err != nil {
		return fmt.Errorf("failed to load channel %d: %w", target.ChannelID, err)
	}

	runCtx := contexts.WithSource(ctx, request.SourceTest)
	result, err := s.testSvc.EvaluateChannelIntelligence(runCtx, objects.GUID{Type: "channel", ID: channel.ID}, lo.ToPtr(target.ModelID), nil, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to evaluate channel %d: %w", channel.ID, err)
	}

	results := make([]objects.IntelligenceKeyResult, 0, len(result.Results))
	for _, item := range result.Results {
		entry := objects.IntelligenceKeyResult{
			KeyPrefix:    item.KeyPrefix,
			Success:      item.Success,
			Quality:      item.Quality,
			Label:        item.Label,
			Reason:       item.Reason,
			TaskID:       item.TaskID,
			GenerationMs: item.GenerationMs,
			DurationMs:   item.DurationMs,
			Error:        item.Error,
		}
		if item.HTML != nil {
			entry.HTML = *item.HTML
		}
		results = append(results, entry)
	}

	status := runStatus(result.Total, result.SuccessCount)

	saved, err := s.ent.IntelligenceRun.Create().
		SetChannelID(channel.ID).
		SetChannelName(channel.Name).
		SetModelID(target.ModelID).
		SetTrigger(trigger).
		SetStatus(intelligencerun.Status(status)).
		SetTotalKeys(result.Total).
		SetSuccessKeys(result.SuccessCount).
		SetFailedKeys(result.FailedCount).
		SetDurationMs(int(time.Since(startedAt).Milliseconds())).
		SetResults(results).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to record run: %w", err)
	}

	if err := s.trimHistory(ctx, channel.ID); err != nil {
		log.Warn(ctx, "intelligence check: failed to trim history", log.Cause(err))
	}

	log.Info(ctx, "intelligence check: run recorded",
		log.Int("channel_id", channel.ID),
		log.Int("run_id", saved.ID),
		log.String("trigger", trigger),
		log.Int("total", result.Total),
		log.Int("success", result.SuccessCount))

	return nil
}

// trimHistory keeps only the newest runs of a channel.
func (s *IntelligenceService) trimHistory(ctx context.Context, channelID int) error {
	ids, err := s.ent.IntelligenceRun.Query().
		Where(intelligencerun.ChannelIDEQ(channelID)).
		Order(ent.Desc(intelligencerun.FieldCreatedAt)).
		Offset(intelligenceMaxRunsPerChannel).
		IDs(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	_, err = s.ent.IntelligenceRun.Delete().Where(intelligencerun.IDIn(ids...)).Exec(ctx)
	return err
}

// History returns one channel's most recent runs, newest first.
func (s *IntelligenceService) History(ctx context.Context, channelID int, first *int) (*ent.IntelligenceRunConnection, error) {
	limit := intelligenceMaxRunsPerChannel
	if first != nil && *first > 0 && *first < limit {
		limit = *first
	}

	query := s.ent.IntelligenceRun.Query().Where(intelligencerun.ChannelIDEQ(channelID))

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, err
	}

	runs, err := query.
		Order(ent.Desc(intelligencerun.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	edges := make([]*ent.IntelligenceRunEdge, 0, len(runs))
	for _, run := range runs {
		edges = append(edges, &ent.IntelligenceRunEdge{
			Node:   run,
			Cursor: entgql.Cursor[int]{ID: run.ID},
		})
	}

	connection := &ent.IntelligenceRunConnection{
		Edges:      edges,
		TotalCount: total,
		PageInfo:   ent.PageInfo{},
	}
	if len(edges) > 0 {
		connection.PageInfo.StartCursor = &edges[0].Cursor
		connection.PageInfo.EndCursor = &edges[len(edges)-1].Cursor
		connection.PageInfo.HasNextPage = total > len(edges)
	}

	return connection, nil
}

// runStatus collapses the per-key outcomes into the run-level verdict. A run
// with no keys at all counts as failed, since nothing was verified.
func runStatus(total, success int) objects.IntelligenceRunStatus {
	switch {
	case total > 0 && success == total:
		return objects.IntelligenceRunSucceeded
	case success > 0:
		return objects.IntelligenceRunPartial
	default:
		return objects.IntelligenceRunFailed
	}
}

// isAllowedInterval reports whether the configuration accepts the interval.
func isAllowedInterval(minutes int) bool {
	return slices.Contains(allowedIntelligenceIntervals, minutes)
}
