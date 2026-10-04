package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	"github.com/looplj/axonhub/llm/transformer/openai"
)

const (
	// defaultIntelligenceBaseURL is the public endpoint of the 满血.AI
	// (manxue.ai) de-intelligence detection service.
	defaultIntelligenceBaseURL = "https://manxue.ai"

	// intelligenceTestsPath creates an asynchronous detection task. Submitting a
	// non-empty html body switches the task to HTML evaluation mode.
	intelligenceTestsPath = "/api/v1/tests"

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

	// intelligencePollTimeout mirrors the documented 10 minute task ceiling.
	intelligencePollTimeout = 5 * time.Minute

	// intelligenceTotalTimeout bounds a whole evaluation run. The admin GraphQL
	// route grants this operation the server's LLM request timeout
	// (adminGraphQLTimeout in the server package), so the run gives up before
	// that budget expires and reports a readable error instead of letting the
	// connection drop.
	intelligenceTotalTimeout = 9 * time.Minute

	// intelligenceMaxConcurrency caps how many channel API keys are evaluated at
	// the same time. Each key fans out to both the tested model and the detection
	// service, so the limit also bounds upstream cost and rate-limit pressure.
	intelligenceMaxConcurrency = 4
)

// defaultIntelligencePrompt is the published pelican task. The upstream service
// scores the returned HTML with a local classifier trained against this exact
// prompt, so it is kept verbatim to keep results comparable across runs.
const defaultIntelligencePrompt = "请生成可直接运行的单文件HTML，使用内联SVG绘制鹈鹕骑自行车的二维循环动画。画面以鹈鹕和自行车为主体，展示清晰的身体结构、踩踏动作和车轮转动，配合协调的背景、配色与层次。动画应流畅自然、衔接连续，并适配不同屏幕尺寸。禁止依赖外部资源，只输出完整HTML，不要代码围栏或解释文字。"

// IntelligenceKeyResult is the evaluation outcome for a single API key.
type IntelligenceKeyResult struct {
	KeyPrefix    string
	Success      bool
	Quality      string
	Label        string
	Reason       string
	TaskID       string
	DurationMs   int
	InputTokens  int
	OutputTokens int
	Error        *string
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

// submit creates an HTML evaluation task and returns its task id.
func (c *intelligenceEvaluator) submit(ctx context.Context, html string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"benchmark": intelligenceBenchmark,
		"html":      html,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal evaluation request: %w", err)
	}

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
		result.DurationMs = int(generated.Get("duration_ms").Int())
		result.InputTokens = int(generated.Get("input_tokens").Int())
		result.OutputTokens = int(generated.Get("output_tokens").Int())
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
) (*IntelligenceEvaluateResult, error) {
	channel, err := processor.channelService.GetChannel(ctx, channelID.ID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, intelligenceTotalTimeout)
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
	if prompt != nil && strings.TrimSpace(*prompt) != "" {
		generationPrompt = *prompt
	}

	evaluator := newIntelligenceEvaluator(processor.httpClient, lo.FromPtr(baseURL))
	results := make([]*IntelligenceKeyResult, len(resolvedKeys))

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(intelligenceMaxConcurrency)

	for index, key := range resolvedKeys {
		group.Go(func() error {
			results[index] = processor.evaluateIntelligenceKey(groupCtx, channel, key, model, generationPrompt, evaluator)
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
) *IntelligenceKeyResult {
	result := &IntelligenceKeyResult{KeyPrefix: maskAPIKey(key)}

	html, err := processor.generateIntelligenceHTML(ctx, channel, key, model, prompt)
	if err != nil {
		result.Error = lo.ToPtr(err.Error())
		return result
	}

	html = stripMarkdownFence(html)
	if html == "" {
		result.Error = lo.ToPtr("model returned empty HTML")
		return result
	}

	taskID, err := evaluator.submit(ctx, html)
	if err != nil {
		result.Error = lo.ToPtr(err.Error())
		return result
	}

	result.TaskID = taskID

	evaluation, err := evaluator.wait(ctx, taskID)
	if err != nil {
		result.Error = lo.ToPtr(err.Error())
		return result
	}

	evaluation.KeyPrefix = result.KeyPrefix

	return evaluation
}

// generateIntelligenceHTML asks the tested model to produce the pelican HTML
// through the channel pipeline, forcing the supplied API key.
func (processor *TestChannelOrchestrator) generateIntelligenceHTML(
	ctx context.Context,
	channel *biz.Channel,
	key string,
	model string,
	prompt string,
) (string, error) {
	inbound := openai.NewInboundTransformer()

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

	useStream := channel.Policies.Stream == objects.CapabilityPolicyRequire
	responsesWebSocket := usesResponsesWebSocket(channel)

	llmRequest := buildChannelTestRequest(model, useStream, "", prompt, responsesWebSocket)
	if !responsesWebSocket {
		llmRequest.MaxCompletionTokens = lo.ToPtr(int64(intelligenceGenerateMaxTokens))
	}

	body, err := json.Marshal(llmRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal generation request: %w", err)
	}

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
