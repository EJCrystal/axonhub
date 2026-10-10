package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/errgroup"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xjson"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/pipeline/stream"
	"github.com/looplj/axonhub/llm/transformer"
)

const (
	// defaultIntelligenceBaseURL is the public endpoint of the 满血.AI
	// (manxue.ai) de-intelligence detection service.
	defaultIntelligenceBaseURL = "https://manxue.ai"

	// intelligenceTestsPath creates an asynchronous detection task. Submitting a
	// non-empty html body switches the task to HTML evaluation mode.
	intelligenceTestsPath = "/api/v1/tests"

	// IntelligenceBenchmarkPelican is the HTML benchmark: the model draws a pelican
	// riding a bicycle as a single-file page, and the detection service scores the
	// markup with its local classifier. It is the default, because every target
	// stored before the benchmark field existed ran it.
	IntelligenceBenchmarkPelican = "pelican"

	// IntelligenceBenchmarkCandy is the question benchmark: the model answers the
	// published candy-puzzle counting question, and the answer is judged here
	// rather than by the detection service.
	IntelligenceBenchmarkCandy = "candy"

	// intelligenceBenchmark selects the pelican (鹈鹕骑行) benchmark. A non-empty
	// html body makes the service evaluate that source with its local classifier
	// only, so no upstream credentials or model calls are involved.
	intelligenceBenchmark = "pelican"

	// intelligenceGenerateMaxTokens bounds the HTML produced by the tested model.
	// The service caps submitted HTML at 2 MiB, so a single-file SVG animation
	// fits comfortably while still leaving room for a complete document.
	intelligenceGenerateMaxTokens = 8192

	// intelligencePollInterval follows the service guidance of polling every
	// 2-5 seconds until the task reaches a terminal state.
	intelligencePollInterval = 3 * time.Second

	// intelligenceSubmitAttempts bounds the retries when submitting the generated
	// HTML. A transient reset or EOF leaves no task behind, so retrying is safe.
	intelligenceSubmitAttempts = 3

	// intelligenceSubmitRetryDelay is the base backoff between submit attempts.
	intelligenceSubmitRetryDelay = 2 * time.Second

	// intelligencePollTimeout mirrors the documented 10 minute task ceiling.
	intelligencePollTimeout = 5 * time.Minute

	// intelligenceTotalTimeout bounds a whole evaluation run unless the target
	// asks for a different budget. A run gives up before the server's request
	// budget expires so it can report a readable error instead of letting the
	// connection drop. It is deliberately shorter than the LLM request timeout
	// deployments configure, because a slow channel can spend over eight minutes
	// generating the HTML and the run still needs room to submit and poll after
	// that.
	intelligenceTotalTimeout = 12 * time.Minute

	// intelligenceMaxTimeoutMinutes caps a per-target override. Times beyond this
	// would outlast the server's request budget and be cut off mid-flight, so the
	// save rejects them instead of storing a value that cannot work.
	intelligenceMaxTimeoutMinutes = 20

	// intelligenceGenerateAttempts bounds the retries when the upstream cuts a
	// generation short. A dropped stream leaves nothing to submit, so one more try
	// is what turns an intermittent reset into a recorded verdict. A run that spent
	// its own deadline is not retried: the budget is gone, and a second attempt
	// would only be cancelled the same way.
	intelligenceGenerateAttempts = 2

	// intelligenceGenerateRetryDelay is how long to wait before the retry, so an
	// upstream that is still shedding load is not hit immediately.
	intelligenceGenerateRetryDelay = 5 * time.Second

	// intelligenceHTMLMaxBytes caps the generated source returned to the UI. The
	// detection service accepts up to 2 MiB, but the dialog only renders the
	// artifact, so anything larger is reported without its body.
	intelligenceHTMLMaxBytes = 256 * 1024

	// intelligenceMaxConcurrency caps how many channel API keys are evaluated at
	// the same time. Each key fans out to both the tested model and the detection
	// service, so the limit also bounds upstream cost and rate-limit pressure.
	//
	// It matches the target ceiling: a channel configured with several keys is the
	// other way a run fans out, and a lower cap here would silently re-serialise a
	// manual run that was meant to start everything.
	intelligenceMaxConcurrency = 10
)

// defaultIntelligencePrompt is the published pelican task. The upstream service
// scores the returned HTML with a local classifier trained against this exact
// prompt, so it is kept verbatim to keep results comparable across runs.
// defaultIntelligenceCandyPrompt is the published candy counting question. The
// verdict is decided here from the answer text, so unlike the pelican prompt this
// one is ours to keep readable.
const defaultIntelligenceCandyPrompt = `黑色袋子中有苹果、桃子、西瓜三种口味的糖果，圆形与五角星形可以靠手感区分。活动前决定取出的总数，最少取多少个，才能保证同时拥有不同形状的苹果味和桃子味糖果？

各口味与形状的糖果数量：

形状 苹果味 桃子味 西瓜味
圆形 7 9 8
五角星形 7 6 4

请给出最少需要取出的糖果总数，并说明理由。`

// candyAnswerPattern matches the expected answer as a standalone number. The
// boundary classes keep a number that merely contains 21, such as 210 or 121,
// from counting, while any surrounding punctuation is irrelevant.
var candyAnswerPattern = regexp.MustCompile(`(?:^|[^0-9])21(?:[^0-9]|$)`)

// judgeCandyAnswer scores one answer against the published rule: the answer
// passes when 21 appears as an independent number. The reason says what was
// looked for, so a failure reads as a wrong answer rather than a broken check.
func judgeCandyAnswer(answer string) (passed bool, reason string) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return false, "模型没有返回答案"
	}

	if candyAnswerPattern.MatchString(trimmed) {
		return true, "答案中出现独立的 21"
	}

	return false, "答案中没有出现独立的 21"
}

const defaultIntelligencePrompt = "请生成可直接运行的单文件HTML，使用内联SVG绘制鹈鹕骑自行车的二维循环动画。画面以鹈鹕和自行车为主体，展示清晰的身体结构、踩踏动作和车轮转动，配合协调的背景、配色与层次。动画应流畅自然、衔接连续，并适配不同屏幕尺寸。禁止依赖外部资源，只输出完整HTML，不要代码围栏或解释文字。"

// IntelligenceKeyResult is the evaluation outcome for a single API key.
type IntelligenceKeyResult struct {
	KeyPrefix string
	Success   bool
	Quality   string
	Label     string
	Reason    string
	TaskID    string
	// GenerationMs is how long the tested model took to produce the source, and
	// DurationMs is the whole run: generation plus submission and polling. They
	// are measured here rather than read from the service, which only scores the
	// source we hand it and therefore always reports 0 for its own duration and
	// token counts in HTML mode.
	GenerationMs int
	DurationMs   int
	// HTML is the source the tested model produced, so the UI can render the
	// exact artifact the detection service scored. It is dropped once it grows
	// past intelligenceHTMLMaxBytes to keep the GraphQL payload bounded.
	HTML *string
	// Answer is the text the candy question produced. It is kept apart from HTML
	// so the history can render each benchmark the way it reads: a page for
	// pelican, plain text for candy.
	Answer string
	Error  *string
}

// IntelligenceEvaluateResult aggregates the per-key evaluation outcomes.
type IntelligenceEvaluateResult struct {
	ChannelID    objects.GUID
	Model        string
	Total        int
	SuccessCount int
	FailedCount  int
	Results      []*IntelligenceKeyResult
}

// intelligenceEvaluator is a minimal client for the manxue.ai detection API.
type intelligenceEvaluator struct {
	httpClient *httpclient.HttpClient
	baseURL    string
}

func newIntelligenceEvaluator(httpClient *httpclient.HttpClient, baseURL string) *intelligenceEvaluator {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultIntelligenceBaseURL
	}

	return &intelligenceEvaluator{httpClient: httpClient, baseURL: base}
}

// submit creates an HTML evaluation task and returns its task id. Transient
// transport failures are retried: a lost response leaves no task behind, and a
// short upstream hiccup would otherwise fail the whole check.
func (c *intelligenceEvaluator) submit(ctx context.Context, html string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"benchmark": intelligenceBenchmark,
		"html":      html,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal evaluation request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= intelligenceSubmitAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(intelligenceSubmitRetryDelay * time.Duration(attempt-1)):
			}
		}

		taskID, err := c.submitOnce(ctx, body)
		if err == nil {
			return taskID, nil
		}
		lastErr = err
		if !isRetriableSubmitError(err) {
			break
		}
	}

	return "", lastErr
}

// submitOnce performs a single submit attempt.
func (c *intelligenceEvaluator) submitOnce(ctx context.Context, body []byte) (string, error) {
	response, err := c.httpClient.Do(ctx, &httpclient.Request{
		Method:      http.MethodPost,
		URL:         c.baseURL + intelligenceTestsPath,
		ContentType: "application/json",
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: body,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create evaluation task: %w", err)
	}

	taskID := gjson.GetBytes(response.Body, "id").String()
	if taskID == "" {
		return "", fmt.Errorf("evaluation service returned no task id")
	}

	return taskID, nil
}

// isRetriableSubmitError reports whether a failed submit is worth another try.
// Transport-level failures always are; HTTP errors only when the service side
// failed, since a rejected payload fails the same way every time.
func isRetriableSubmitError(err error) bool {
	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= http.StatusInternalServerError
	}

	return true
}

// wait polls a task until it reaches a terminal state.
func (c *intelligenceEvaluator) wait(ctx context.Context, taskID string) (*IntelligenceKeyResult, error) {
	deadline := time.Now().Add(intelligencePollTimeout)
	ticker := time.NewTicker(intelligencePollInterval)
	defer ticker.Stop()

	for {
		result, done, err := c.poll(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if done {
			return result, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("evaluation task %s timed out after %s", taskID, intelligencePollTimeout)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// poll reads the current task state once. The boolean reports whether the task
// reached a terminal state.
func (c *intelligenceEvaluator) poll(ctx context.Context, taskID string) (*IntelligenceKeyResult, bool, error) {
	response, err := c.httpClient.Do(ctx, &httpclient.Request{
		Method: http.MethodGet,
		URL:    c.baseURL + intelligenceTestsPath + "/" + taskID,
		Headers: http.Header{
			"Accept": []string{"application/json"},
		},
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to read evaluation task: %w", err)
	}

	result := &IntelligenceKeyResult{TaskID: taskID}

	if assessment := gjson.GetBytes(response.Body, "assessment"); assessment.Exists() {
		result.Quality = assessment.Get("quality").String()
		result.Label = assessment.Get("label").String()
		result.Reason = assessment.Get("reason").String()
	}

	if generated := gjson.GetBytes(response.Body, "result"); generated.Exists() {
		if result.Quality == "" {
			result.Quality = generated.Get("quality").String()
		}
	}

	switch status := gjson.GetBytes(response.Body, "status").String(); status {
	case "succeeded":
		result.Success = true
		return result, true, nil
	case "failed", "cancelled":
		reason := strings.TrimSpace(gjson.GetBytes(response.Body, "error").String())
		if reason == "" {
			reason = "evaluation task " + status
		}
		result.Error = lo.ToPtr(reason)
		return result, true, nil
	default:
		return nil, false, nil
	}
}

// EvaluateChannelIntelligence runs the pelican HTML evaluation for every
// targeted API key of a channel. Each key first asks the tested model to
// generate the HTML, then submits that source to the detection service.
func (processor *TestChannelOrchestrator) EvaluateChannelIntelligence(
	ctx context.Context,
	channelID objects.GUID,
	modelID *string,
	keys []string,
	prompt *string,
	baseURL *string,
	reasoningEffort *string,
	timeoutMinutes *int,
	benchmark string,
) (*IntelligenceEvaluateResult, error) {
	channel, err := processor.channelService.GetChannel(ctx, channelID.ID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, intelligenceTargetTimeout(timeoutMinutes))
	defer cancel()

	model := strings.TrimSpace(lo.FromPtr(modelID))
	if model == "" {
		model = channel.DefaultTestModel
	}
	if model == "" {
		return nil, fmt.Errorf("no model selected and channel %s has no default test model", channel.Name)
	}

	resolvedKeys := resolveIntelligenceKeys(channel, keys)
	if len(resolvedKeys) == 0 {
		// OAuth channels hold a single rotating credential that never appears in
		// the key list. Evaluate it through the channel's default selection so
		// those channels stay checkable.
		if !channel.Credentials.IsOAuth() {
			return nil, fmt.Errorf("no enabled API keys configured for channel %s", channel.Name)
		}

		resolvedKeys = []string{""}
	}

	generationPrompt := defaultIntelligencePrompt
	if benchmark == IntelligenceBenchmarkCandy {
		generationPrompt = defaultIntelligenceCandyPrompt
	}
	if prompt != nil && strings.TrimSpace(*prompt) != "" {
		generationPrompt = *prompt
	}

	evaluator := newIntelligenceEvaluator(processor.httpClient, lo.FromPtr(baseURL))
	results := make([]*IntelligenceKeyResult, len(resolvedKeys))

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(intelligenceMaxConcurrency)

	for index, key := range resolvedKeys {
		group.Go(func() error {
			// The candy question is judged here from the answer text, so it never
			// reaches the detection service, which only scores pelican HTML.
			if benchmark == IntelligenceBenchmarkCandy {
				results[index] = processor.evaluateCandyKey(
					groupCtx, channel, key, model, generationPrompt, lo.FromPtr(reasoningEffort))
				return nil
			}

			results[index] = processor.evaluateIntelligenceKey(
				groupCtx, channel, key, model, generationPrompt, evaluator, lo.FromPtr(reasoningEffort))
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	aggregate := &IntelligenceEvaluateResult{
		ChannelID: channelID,
		Model:     model,
		Total:     len(results),
		Results:   results,
	}
	for _, result := range results {
		if result.Success {
			aggregate.SuccessCount++
		} else {
			aggregate.FailedCount++
		}
	}

	log.Info(ctx, "channel intelligence evaluation completed",
		log.Int("channel_id", channelID.ID),
		log.String("model", model),
		log.Int("total", aggregate.Total),
		log.Int("success", aggregate.SuccessCount),
		log.Int("failed", aggregate.FailedCount))

	return aggregate, nil
}

// resolveIntelligenceKeys returns the requested keys, or every enabled key when
// no explicit selection was made. Keys that do not belong to the channel are
// dropped so a stale UI selection cannot probe an unrelated credential.
func resolveIntelligenceKeys(channel *biz.Channel, requested []string) []string {
	if len(requested) == 0 {
		return channel.GetEnabledAPIKeys()
	}

	configured := channel.Credentials.GetAllAPIKeys()
	selected := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))

	for _, key := range requested {
		if _, ok := seen[key]; ok {
			continue
		}
		if !lo.Contains(configured, key) {
			continue
		}

		seen[key] = struct{}{}
		selected = append(selected, key)
	}

	return selected
}

// evaluateIntelligenceKey generates the HTML with one specific API key and
// submits it for evaluation.
func (processor *TestChannelOrchestrator) evaluateIntelligenceKey(
	ctx context.Context,
	channel *biz.Channel,
	key string,
	model string,
	prompt string,
	evaluator *intelligenceEvaluator,
	reasoningEffort string,
) *IntelligenceKeyResult {
	result := &IntelligenceKeyResult{KeyPrefix: maskAPIKey(key)}
	startedAt := time.Now()

	html, err := processor.generateIntelligenceHTML(ctx, channel, key, model, prompt, reasoningEffort)
	if err != nil {
		result.DurationMs = int(time.Since(startedAt).Milliseconds())
		result.Error = lo.ToPtr(err.Error())
		return result
	}

	result.GenerationMs = int(time.Since(startedAt).Milliseconds())

	html = stripMarkdownFence(html)
	if html == "" {
		result.DurationMs = int(time.Since(startedAt).Milliseconds())
		result.Error = lo.ToPtr("model returned empty HTML")
		return result
	}

	// Keep the generated source on the result before submitting it, so a failed
	// or inconclusive assessment still lets the UI show what the model produced.
	result.HTML = htmlForResult(html)

	taskID, err := evaluator.submit(ctx, html)
	if err != nil {
		result.DurationMs = int(time.Since(startedAt).Milliseconds())
		result.Error = lo.ToPtr(err.Error())
		return result
	}

	result.TaskID = taskID

	evaluation, err := evaluator.wait(ctx, taskID)
	if err != nil {
		result.DurationMs = int(time.Since(startedAt).Milliseconds())
		result.Error = lo.ToPtr(err.Error())
		return result
	}

	evaluation.KeyPrefix = result.KeyPrefix
	evaluation.GenerationMs = result.GenerationMs
	evaluation.DurationMs = int(time.Since(startedAt).Milliseconds())
	evaluation.HTML = result.HTML

	return evaluation
}

// evaluateCandyKey asks one API key the candy question and judges the answer
// locally. Nothing is submitted to the detection service: that service only
// scores pelican HTML, and the candy rule is the published one, so judging here
// keeps the credential on this host.
func (processor *TestChannelOrchestrator) evaluateCandyKey(
	ctx context.Context,
	channel *biz.Channel,
	key string,
	model string,
	prompt string,
	reasoningEffort string,
) *IntelligenceKeyResult {
	result := &IntelligenceKeyResult{KeyPrefix: maskAPIKey(key)}
	startedAt := time.Now()

	answer, err := processor.generateIntelligenceHTML(ctx, channel, key, model, prompt, reasoningEffort)
	if err != nil {
		result.DurationMs = int(time.Since(startedAt).Milliseconds())
		result.Error = lo.ToPtr(err.Error())
		return result
	}

	result.GenerationMs = int(time.Since(startedAt).Milliseconds())
	result.DurationMs = result.GenerationMs

	trimmed := stripMarkdownFence(answer)
	if strings.TrimSpace(trimmed) == "" {
		result.Error = lo.ToPtr("model returned an empty answer")
		return result
	}

	// The answer is text, not a page, so it goes in its own field and the history
	// renders it as prose rather than trying to load it as HTML.
	result.Answer = trimmed

	passed, reason := judgeCandyAnswer(trimmed)
	result.Success = passed
	result.Reason = reason
	if passed {
		result.Quality = objects.IntelligenceManualVerdictNormal
		result.Label = "正常"
	} else {
		result.Quality = objects.IntelligenceManualVerdictDegraded
		result.Label = "疑似降智"
	}

	return result
}

// htmlForResult returns the generated source for the UI, or nil when it is too
// large to ship through the GraphQL payload.
func htmlForResult(html string) *string {
	if html == "" || len(html) > intelligenceHTMLMaxBytes {
		return nil
	}

	return lo.ToPtr(html)
}

// generateIntelligenceHTML asks the tested model to produce the pelican HTML
// through the channel pipeline, forcing the supplied API key.
func (processor *TestChannelOrchestrator) generateIntelligenceHTML(
	ctx context.Context,
	channel *biz.Channel,
	key string,
	model string,
	prompt string,
	reasoningEffort string,
) (string, error) {
	// The channel's own API format decides the request shape and the inbound
	// transformer, matching how the channel test builds its request: a Decisions
	// channel needs the Decisions body, not a chat completion.
	apiFormat := channelTestAPIFormat(channel, model)
	inbound := channelTestInbound(apiFormat)

	chatProcessor := &ChatCompletionOrchestrator{
		channelSelector: &SpecifiedChannelSelector{
			ChannelService: processor.channelService,
			ChannelID:      objects.GUID{Type: "channel", ID: channel.ID},
			SelectedAPIKey: key,
		},
		RequestService:             processor.requestService,
		ChannelService:             processor.channelService,
		PromptProvider:             &stubPromptProvider{},
		PromptProtecter:            processor.promptProtectionRuleService,
		PipelineFactory:            pipeline.NewFactory(processor.httpClient),
		Middlewares:                []pipeline.Middleware{stream.EnsureUsage()},
		Inbound:                    inbound,
		SystemService:              processor.systemService,
		UsageLogService:            processor.usageLogService,
		ModelMapper:                processor.modelMapper,
		adaptiveLoadBalancer:       processor.loadBalancer,
		failoverLoadBalancer:       processor.loadBalancer,
		circuitBreakerLoadBalancer: processor.loadBalancer,
		channelLimiterManager:      processor.channelLimiterManager,
		modelCircuitBreaker:        processor.modelCircuitBreaker,
	}

	// The generation is always streamed. A full HTML document takes minutes to
	// produce, and the tested channels sit behind Cloudflare's 120-second proxy
	// read timeout: a non-streamed request must deliver its whole body inside that
	// window, so a slow model is cut off with a 524 before it finishes, while a
	// streamed one keeps sending and holds the connection open. The channel's own
	// stream policy does not apply, because this is a request the check issues
	// itself rather than one a client sent.
	useStream := true
	responsesWebSocket := usesResponsesWebSocket(channel)

	var lastErr error
	for attempt := 1; attempt <= intelligenceGenerateAttempts; attempt++ {
		llmRequest := buildChannelTestRequest(model, useStream, "", prompt, responsesWebSocket, apiFormat)
		if !responsesWebSocket {
			llmRequest.MaxCompletionTokens = lo.ToPtr(int64(intelligenceGenerateMaxTokens))
		}

		// The configured thinking level wins over whatever the model name or the
		// auto-reasoning middleware would otherwise decide for this run.
		if strings.TrimSpace(reasoningEffort) != "" {
			llmRequest.ReasoningEffort = strings.TrimSpace(reasoningEffort)
		}

		body, err := json.Marshal(llmRequest)
		if err != nil {
			return "", fmt.Errorf("failed to marshal generation request: %w", err)
		}

		html, err := processor.runIntelligenceGeneration(ctx, chatProcessor, inbound, body)
		if err == nil {
			return html, nil
		}
		lastErr = err

		// Only a transient cut is worth another try. A deadline the run spent on
		// its own is not, and neither is a rejected request, which would fail the
		// same way every time.
		if attempt >= intelligenceGenerateAttempts || !isRetriableGenerationError(err) || ctx.Err() != nil {
			return "", err
		}

		log.Warn(ctx, "intelligence check: generation failed, retrying",
			log.Int("channel_id", channel.ID),
			log.String("model", model),
			log.Int("attempt", attempt),
			log.Cause(err),
		)

		select {
		case <-ctx.Done():
			return "", lastErr
		case <-time.After(intelligenceGenerateRetryDelay):
		}
	}

	return "", lastErr
}

// runIntelligenceGeneration issues one generation request and returns the source
// the model produced. Errors carry the upstream message so the recorded failure
// explains itself.
func (processor *TestChannelOrchestrator) runIntelligenceGeneration(
	ctx context.Context,
	chatProcessor *ChatCompletionOrchestrator,
	inbound transformer.Inbound,
	body []byte,
) (string, error) {
	rawResponse, err := chatProcessor.Process(ctx, &httpclient.Request{
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: body,
	})
	if err != nil {
		rawErr := inbound.TransformError(ctx, err)
		message := gjson.GetBytes(rawErr.Body, "error.message").String()
		if message == "" {
			message = err.Error()
		}

		return "", fmt.Errorf("%s", message)
	}

	if rawResponse.ChatCompletionStream != nil {
		streamResult, err := processor.handleStreamResponse(ctx, rawResponse.ChatCompletionStream, time.Now())
		if err != nil {
			return "", err
		}
		if !streamResult.Success {
			return "", fmt.Errorf("%s", lo.FromPtr(streamResult.Error))
		}

		return lo.FromPtr(streamResult.Message), nil
	}

	response, err := xjson.To[llm.Response](rawResponse.ChatCompletion.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse generation response: %w", err)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no message in generation response")
	}

	return lo.FromPtr(response.Choices[0].Message.Content.Content), nil
}

// intelligenceRetriableGenerationErrors are the upstream failures that a second
// attempt can plausibly clear. They are all transport-level interruptions: the
// connection dropped, an empty stream came back, or an edge proxy gave up. A
// rejected request or an error the model itself reported is deliberately absent,
// because those repeat exactly.
var intelligenceRetriableGenerationErrors = []string{
	"no content in stream response",
	"stream error:",
	"internal_error",
	// The signature the tested relays actually return when they give up mid-stream
	// ("error: stream_timeout, code: stream_timeout, type: upstream_error"). It is
	// the connection dying, not a verdict on the request, so it gets another try.
	"stream_timeout",
	"stream timeout",
	"unexpected eof",
	"connection reset",
	"broken pipe",
	"temporarily unavailable",
	"service is busy",
	"upstream request failed",
	"error code: 524",
	"error code: 502",
	"error code: 503",
	"error code: 504",
}

// isRetriableGenerationError reports whether a generation failure is a transient
// interruption rather than a verdict on the request itself.
func isRetriableGenerationError(err error) bool {
	if err == nil {
		return false
	}

	// The run's own deadline is not an upstream problem; retrying would spend a
	// budget that is already gone.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}

	message := strings.ToLower(err.Error())
	for _, signature := range intelligenceRetriableGenerationErrors {
		if strings.Contains(message, signature) {
			return true
		}
	}

	return false
}

// intelligenceTargetTimeout resolves the budget for one run. A target that
// names its own timeout wins, because an upstream that needs over ten minutes to
// produce the HTML cannot finish inside the default; anything unset or out of
// range falls back to it.
func intelligenceTargetTimeout(minutes *int) time.Duration {
	if minutes == nil {
		return intelligenceTotalTimeout
	}

	value := *minutes
	if value <= 0 || value > intelligenceMaxTimeoutMinutes {
		return intelligenceTotalTimeout
	}

	return time.Duration(value) * time.Minute
}

// stripMarkdownFence removes a wrapping code fence if the model ignored the
// instruction to return raw HTML. The detection service classifies whatever is
// submitted, so fences would otherwise be counted as markup noise.
func stripMarkdownFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	if newline := strings.Index(trimmed, "\n"); newline >= 0 {
		trimmed = trimmed[newline+1:]
	}

	if end := strings.LastIndex(trimmed, "```"); end >= 0 {
		trimmed = trimmed[:end]
	}

	return strings.TrimSpace(trimmed)
}
