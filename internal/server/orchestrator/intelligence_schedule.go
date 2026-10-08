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
	"github.com/looplj/axonhub/llm"
)

const (
	// IntelligenceConfigKey is the system key holding the scheduled check
	// configuration.
	IntelligenceConfigKey = "intelligence_check_config"

	// intelligenceMaxRunsPerChannel bounds the rows a single history read may
	// return. The retention limit below is what actually trims storage.
	intelligenceMaxRunsPerChannel = 100

	// intelligenceMaxRunsPerKey bounds the history kept for one API key of one
	// channel, so a busy key cannot crowd out the others.
	intelligenceMaxRunsPerKey = 10

	// intelligenceSchedulerTask is the name of the scheduled task.
	intelligenceSchedulerTask = "intelligence-check"
)

// intelligenceTargetKey identifies one target: a key may run several models on
// the same channel, but never the same key twice.
type intelligenceTargetKey struct {
	ChannelID int
	APIKey    string
}

// maskIntelligenceTargetKey keeps an error message from echoing a full secret.
func maskIntelligenceTargetKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}

	return key[:4] + "****" + key[len(key)-4:]
}

// allowedIntelligenceIntervals are the intervals the configuration accepts.
var allowedIntelligenceIntervals = []int{10, 30, 60}

// allowedReasoningEfforts are the thinking levels a target may request. These
// mirror llm/reasoning.go; an empty value means "use the provider default" and
// is always allowed.
var allowedReasoningEfforts = []string{
	llm.ReasoningEffortNone,
	llm.ReasoningEffortMinimal,
	llm.ReasoningEffortLow,
	llm.ReasoningEffortMedium,
	llm.ReasoningEffortHigh,
	llm.ReasoningEffortXHigh,
	llm.ReasoningEffortMax,
}

// isAllowedReasoningEffort reports whether the target may request this level.
// The empty value is allowed and means the request is left alone.
func isAllowedReasoningEffort(effort string) bool {
	if strings.TrimSpace(effort) == "" {
		return true
	}

	return slices.Contains(allowedReasoningEfforts, strings.TrimSpace(effort))
}

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

// intelligenceTaskSpec builds the scheduler spec for the configured interval, so
// the tick itself follows the setting instead of a fixed rate.
func intelligenceTaskSpec(intervalMinutes int) scheduler.TaskSpec {
	if !isAllowedInterval(intervalMinutes) {
		intervalMinutes = defaultIntelligenceConfig().IntervalMinutes
	}

	return scheduler.TaskSpec{
		Name:        intelligenceSchedulerTask,
		Description: "Run the configured intelligence checks",
		FixRate:     time.Duration(intervalMinutes) * time.Minute,
	}
}

// RegisterScheduledTasks wires the check into the scheduler at the interval the
// stored configuration asks for. The task always registers; the handler itself
// honours the Enabled flag so toggling the configuration does not need a
// restart.
func (s *IntelligenceService) RegisterScheduledTasks(ctx context.Context, sched *scheduler.Scheduler) error {
	// The fx OnStart hook has no principal, so reading the stored interval would
	// be denied by the ent privacy layer and fall back to the default. Take the
	// system bypass, matching the backup and video storage registrations.
	ctx = authz.WithSystemBypass(ctx, "intelligence-check-register")

	config, err := s.IntelligenceConfig(ctx)
	if err != nil {
		log.Warn(ctx, "intelligence check: falling back to the default interval", log.Cause(err))
		config = defaultIntelligenceConfig()
	}

	return sched.Register(ctx, intelligenceTaskSpec(config.IntervalMinutes), s.runScheduled)
}

// validateIntelligenceTargets checks a target list before it is stored. It is
// kept separate from the save so the rules can be exercised on their own.
func validateIntelligenceTargets(targets []objects.IntelligenceTarget) error {
	seen := make(map[intelligenceTargetKey]struct{}, len(targets))
	// A channel may appear more than once so each of its keys can run against a
	// different model and thinking level; the key is what identifies a target.
	for _, target := range targets {
		if target.ChannelID <= 0 {
			return fmt.Errorf("every target needs a channel")
		}
		// Without a key the target would fan out to every enabled key of the
		// channel, which can overlap another target's key and re-run it.
		if strings.TrimSpace(target.APIKey) == "" {
			return fmt.Errorf("every target needs an API key")
		}
		if strings.TrimSpace(target.ModelID) == "" {
			return fmt.Errorf("every target needs a model")
		}
		// An effort the transformers do not know would be forwarded verbatim and
		// rejected upstream, so catch it while the operator is still looking.
		if !isAllowedReasoningEffort(target.ReasoningEffort) {
			return fmt.Errorf("reasoning effort must be one of %v", allowedReasoningEfforts)
		}

		identity := intelligenceTargetKey{ChannelID: target.ChannelID, APIKey: strings.TrimSpace(target.APIKey)}
		if _, ok := seen[identity]; ok {
			return fmt.Errorf("key %s of channel %d is configured twice", maskIntelligenceTargetKey(identity.APIKey), target.ChannelID)
		}
		seen[identity] = struct{}{}
	}
	return nil
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

	if err := validateIntelligenceTargets(config.Targets); err != nil {
		return err
	}

	body, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to encode configuration: %w", err)
	}

	if err := s.systemService.SetSystemValue(ctx, IntelligenceConfigKey, string(body)); err != nil {
		return err
	}

	// The interval drives the scheduler tick, so a change has to be applied to
	// the live task rather than waiting for the next restart to pick it up.
	s.reschedule(ctx, config.IntervalMinutes)

	return nil
}

// reschedule aligns the live task with the configured interval. The task is
// registered during startup, so a missing scheduler or an unregistered task is
// not an error worth failing the save over.
func (s *IntelligenceService) reschedule(ctx context.Context, intervalMinutes int) {
	if s.scheduler == nil {
		return
	}

	if err := s.scheduler.Reschedule(ctx, intelligenceSchedulerTask, intelligenceTaskSpec(intervalMinutes)); err != nil {
		log.Warn(ctx, "intelligence check: failed to reschedule task", log.Cause(err))
	}
}

// runScheduled executes a configured run when the schedule is due.
func (s *IntelligenceService) runScheduled(ctx context.Context) {
	// The scheduler runs with a background context, which carries no principal,
	// so every read and write below needs the system bypass to clear the ent
	// privacy layer.
	runCtx := authz.WithSystemBypass(context.WithoutCancel(ctx), "intelligence-check")

	config, err := s.IntelligenceConfig(runCtx)
	if err != nil {
		log.Error(runCtx, "intelligence check: failed to read configuration", log.Cause(err))
		return
	}
	if !config.Enabled || len(config.Targets) == 0 {
		return
	}

	// The scheduler tick already carries the configured interval, so reaching
	// here means the run is due.
	s.runConfiguredTargets(runCtx, config, "scheduled")
}

// RunManual evaluates the configured targets immediately, whatever the schedule
// says. A non-zero channelID narrows the run to that channel and an apiKey
// narrows it to one target, which is what the split button offers: re-checking
// one suspicious key should not re-run and re-bill the others.
func (s *IntelligenceService) RunManual(ctx context.Context, channelID int, apiKey string) (int, error) {
	config, err := s.IntelligenceConfig(ctx)
	if err != nil {
		return 0, err
	}
	if len(config.Targets) == 0 {
		return 0, fmt.Errorf("no channel is configured for the intelligence check")
	}

	if channelID != 0 || strings.TrimSpace(apiKey) != "" {
		narrowed, err := narrowIntelligenceConfig(config, channelID, strings.TrimSpace(apiKey))
		if err != nil {
			return 0, err
		}

		config = narrowed
	}

	// The run outlives the GraphQL request: detach the context so the request
	// returning does not abort the work or close its database connection, and
	// take the system bypass because the detached context carries no principal.
	bgCtx := authz.WithSystemBypass(context.WithoutCancel(ctx), "intelligence-check-manual")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error(context.Background(), "intelligence check: manual run panicked", log.Any("panic", r))
			}
		}()

		s.runConfiguredTargets(bgCtx, config, "manual")
	}()

	return len(config.Targets), nil
}

// narrowIntelligenceConfig keeps only the named channel's target, so a manual
// run can re-check one channel without touching the rest. A channel that is not
// configured is an error rather than an empty run.
// narrowIntelligenceConfig keeps only the target the run asked for. A channel
// may carry several targets, one per key, so naming just the channel would run
// all of its keys; apiKey, when given, is what selects between them.
func narrowIntelligenceConfig(config *objects.IntelligenceConfig, channelID int, apiKey string) (*objects.IntelligenceConfig, error) {
	selected := make([]objects.IntelligenceTarget, 0, 1)
	for _, target := range config.Targets {
		if target.ChannelID != channelID {
			continue
		}
		if apiKey != "" && target.APIKey != apiKey {
			continue
		}

		selected = append(selected, target)
	}
	if len(selected) == 0 {
		if apiKey != "" {
			return nil, fmt.Errorf("key %s of channel %d is not configured for the intelligence check",
				maskIntelligenceTargetKey(apiKey), channelID)
		}

		return nil, fmt.Errorf("channel %d is not configured for the intelligence check", channelID)
	}

	return &objects.IntelligenceConfig{
		Enabled:         config.Enabled,
		IntervalMinutes: config.IntervalMinutes,
		Targets:         selected,
	}, nil
}

// runConfiguredTargets evaluates every target, one channel at a time so a run
// does not fan out past the upstream concurrency the check already uses.
//
// The scheduler invokes its tasks with a context it builds itself, which
// carries no ent client, while the work below reads and writes through ent both
// directly and inside the request persistence middleware. Attaching the client
// here covers the scheduled path without depending on how the run was started.
func (s *IntelligenceService) runConfiguredTargets(ctx context.Context, config *objects.IntelligenceConfig, trigger string) {
	ctx = s.attachEntClient(ctx)

	for _, target := range config.Targets {
		if err := s.runOneChannel(ctx, target, trigger); err != nil {
			log.Error(ctx, "intelligence check: channel run failed",
				log.Int("channel_id", target.ChannelID), log.Cause(err))
		}
	}
}

// attachEntClient makes sure the context carries the service's ent client.
// A context that already has one keeps it, so callers that came in through a
// request are unaffected.
func (s *IntelligenceService) attachEntClient(ctx context.Context) context.Context {
	if s.ent == nil || ent.FromContext(ctx) != nil {
		return ctx
	}

	return ent.NewContext(ctx, s.ent)
}

// runOneChannel evaluates every enabled key of one channel and records the run.
func (s *IntelligenceService) runOneChannel(ctx context.Context, target objects.IntelligenceTarget, trigger string) error {
	startedAt := time.Now()

	channel, err := s.channelSvc.GetChannel(ctx, target.ChannelID)
	if err != nil {
		return fmt.Errorf("failed to load channel %d: %w", target.ChannelID, err)
	}

	// A configured key narrows the run to that key; leaving it empty keeps the
	// original behaviour of evaluating every enabled key of the channel.
	var keys []string
	if strings.TrimSpace(target.APIKey) != "" {
		keys = []string{target.APIKey}
	}

	runCtx := contexts.WithSource(ctx, request.SourceTest)
	result, err := s.testSvc.EvaluateChannelIntelligence(
		runCtx, objects.GUID{Type: "channel", ID: channel.ID}, lo.ToPtr(target.ModelID), keys, nil, nil,
		lo.ToPtr(target.ReasoningEffort))
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

	status := runStatus(results)

	saved, err := s.ent.IntelligenceRun.Create().
		SetChannelID(channel.ID).
		SetChannelName(channel.Name).
		SetModelID(target.ModelID).
		SetReasoningEffort(strings.TrimSpace(target.ReasoningEffort)).
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

// trimRun is the part of a recorded run the retention pass needs: which row it
// is, and which API keys it evaluated.
type trimRun struct {
	ID   int
	Keys []string
}

// unnamedIntelligenceKey buckets runs that produced no per-key result.
const unnamedIntelligenceKey = ""

// runsToTrim picks the runs to delete so that at most limit runs remain per API
// key, newest first.
//
// A run is dropped only when every key it covers already has limit newer runs,
// so a run that one key still needs is kept even if another key has moved on.
// That keeps the per-key bound honest without discarding the only record of a
// key. Runs that evaluated no key at all — an evaluation that failed before it
// reached any of them — share one bucket, so they cannot pile up either.
func runsToTrim(runs []trimRun, limit int) []int {
	seen := make(map[string]int, 4)
	var drop []int

	for _, run := range runs {
		keys := run.Keys
		if len(keys) == 0 {
			keys = []string{unnamedIntelligenceKey}
		}

		beyond := true
		for _, key := range keys {
			if seen[key] < limit {
				beyond = false
				break
			}
		}

		// A dropped run does not count towards any key's budget; otherwise one
		// extra key would silently halve another key's history.
		if beyond {
			drop = append(drop, run.ID)
			continue
		}

		for _, key := range keys {
			seen[key]++
		}
	}

	return drop
}

// trimHistory keeps only the newest intelligenceMaxRunsPerKey runs of each API
// key of a channel.
func (s *IntelligenceService) trimHistory(ctx context.Context, channelID int) error {
	runs, err := s.ent.IntelligenceRun.Query().
		Where(intelligencerun.ChannelIDEQ(channelID)).
		Order(ent.Desc(intelligencerun.FieldCreatedAt)).
		Select(intelligencerun.FieldID, intelligencerun.FieldResults).
		All(ctx)
	if err != nil {
		return err
	}

	trim := make([]trimRun, 0, len(runs))
	for _, run := range runs {
		keys := make([]string, 0, len(run.Results))
		seen := make(map[string]struct{}, len(run.Results))
		for _, result := range run.Results {
			if _, ok := seen[result.KeyPrefix]; ok {
				continue
			}
			seen[result.KeyPrefix] = struct{}{}
			keys = append(keys, result.KeyPrefix)
		}
		trim = append(trim, trimRun{ID: run.ID, Keys: keys})
	}

	ids := runsToTrim(trim, intelligenceMaxRunsPerKey)
	if len(ids) == 0 {
		return nil
	}

	_, err = s.ent.IntelligenceRun.Delete().Where(intelligencerun.IDIn(ids...)).Exec(ctx)
	return err
}

// SetManualVerdict records an operator's verdict for one key of a run, or clears
// it when verdict is empty, and recomputes the run status. This exists for the
// case automatic scoring cannot cover: the detection service is unreachable, so
// a human judges the generated source and the run should end up reflecting that.
func (s *IntelligenceService) SetManualVerdict(ctx context.Context, runID int, keyPrefix string, verdict string) (*ent.IntelligenceRun, error) {
	keyPrefix = strings.TrimSpace(keyPrefix)
	if keyPrefix == "" {
		return nil, fmt.Errorf("key prefix is required")
	}

	switch verdict {
	case "", objects.IntelligenceManualVerdictNormal, objects.IntelligenceManualVerdictDegraded:
	default:
		return nil, fmt.Errorf("verdict must be %s or %s", objects.IntelligenceManualVerdictNormal, objects.IntelligenceManualVerdictDegraded)
	}

	run, err := s.ent.IntelligenceRun.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to load intelligence run: %w", err)
	}

	results := make([]objects.IntelligenceKeyResult, len(run.Results))
	copy(results, run.Results)

	found := false
	for i := range results {
		if results[i].KeyPrefix != keyPrefix {
			continue
		}
		results[i].ManualVerdict = verdict
		found = true
	}
	if !found {
		return nil, fmt.Errorf("run %d has no result for key %s", runID, keyPrefix)
	}

	// The counters and the run status follow the effective verdict, so a manual
	// decision is reflected everywhere the history is read.
	success := 0
	for _, result := range results {
		if keyVerdict(result) == objects.IntelligenceManualVerdictNormal {
			success++
		}
	}

	updated, err := s.ent.IntelligenceRun.UpdateOneID(runID).
		SetResults(results).
		SetSuccessKeys(success).
		SetFailedKeys(len(results) - success).
		SetStatus(intelligencerun.Status(runStatus(results))).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to record manual verdict: %w", err)
	}

	return updated, nil
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

// runStatus collapses the per-key outcomes into the run-level verdict. A key
// that never produced an answer counts as failed rather than succeeded, so a
// run only reads as succeeded when the model was actually judged normal, and
// only reads as partial when some keys passed and others did not.
func runStatus(results []objects.IntelligenceKeyResult) objects.IntelligenceRunStatus {
	if len(results) == 0 {
		return objects.IntelligenceRunFailed
	}

	normal, failed := 0, 0
	for _, result := range results {
		switch keyVerdict(result) {
		case objects.IntelligenceManualVerdictNormal:
			normal++
		default:
			failed++
		}
	}

	switch {
	case failed == 0:
		return objects.IntelligenceRunSucceeded
	case normal == 0:
		return objects.IntelligenceRunFailed
	default:
		return objects.IntelligenceRunPartial
	}
}

// keyVerdict resolves one key's effective outcome. An operator's manual verdict
// wins over whatever automatic scoring reported, which is what lets a human
// close out a run whose scoring failed.
func keyVerdict(result objects.IntelligenceKeyResult) string {
	switch result.ManualVerdict {
	case objects.IntelligenceManualVerdictNormal, objects.IntelligenceManualVerdictDegraded:
		return result.ManualVerdict
	}

	if !result.Success {
		return objects.IntelligenceManualVerdictDegraded
	}

	switch result.Quality {
	case "normal":
		return objects.IntelligenceManualVerdictNormal
	default:
		return objects.IntelligenceManualVerdictDegraded
	}
}

// isAllowedInterval reports whether the configuration accepts the interval.
func isAllowedInterval(minutes int) bool {
	return slices.Contains(allowedIntelligenceIntervals, minutes)
}
