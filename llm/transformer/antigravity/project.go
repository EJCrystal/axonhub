package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/looplj/axonhub/llm/httpclient"
)

const (
	projectNegotiationTimeout = 8 * time.Second
	projectCacheTTL           = 10 * time.Minute
	projectFailureCacheTTL    = 30 * time.Second
)

// projectDiscoveryFunc is kept injectable so request transformation tests can
// exercise the envelope without contacting Google's control plane.
type projectDiscoveryFunc func(context.Context, string, string, *httpclient.HttpClient) (string, error)

type loadCodeAssistRequest struct {
	Metadata loadCodeAssistMetadata `json:"metadata"`
}

type loadCodeAssistMetadata struct {
	IDEType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
}

type loadCodeAssistResponse struct {
	CloudAICompanionProject json.RawMessage `json:"cloudaicompanionProject"`
}

// resolveProject follows the Antigravity client's control-plane handshake. A
// successful negotiation is cached per transformer because the enabled
// channel cache keeps transformers alive across requests. The saved project is
// retained as a short-lived fallback when the control plane is unavailable.
func (t *Transformer) resolveProject(ctx context.Context, accessToken string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("access token is empty")
	}

	now := time.Now()
	t.projectMu.RLock()
	configuredProject := strings.TrimSpace(t.config.Project)
	resolvedProject := configuredProject
	resolved := t.projectResolved
	lastAttempt := t.projectResolvedAt
	resolver := t.projectResolver
	baseURL := t.config.BaseURL
	httpClient := t.httpClient
	t.projectMu.RUnlock()

	if !lastAttempt.IsZero() {
		cacheTTL := projectFailureCacheTTL
		if resolved {
			cacheTTL = projectCacheTTL
		}
		if now.Sub(lastAttempt) < cacheTTL && resolvedProject != "" {
			return resolvedProject, nil
		}
	}

	v, err, _ := t.projectSF.Do("load-code-assist", func() (any, error) {
		// Re-check after waiting for another request's negotiation.
		t.projectMu.RLock()
		project := strings.TrimSpace(t.config.Project)
		resolved := t.projectResolved
		lastAttempt := t.projectResolvedAt
		t.projectMu.RUnlock()
		now := time.Now()
		cacheTTL := projectFailureCacheTTL
		if resolved {
			cacheTTL = projectCacheTTL
		}
		if !lastAttempt.IsZero() && now.Sub(lastAttempt) < cacheTTL && project != "" {
			return project, nil
		}

		if resolver == nil {
			resolver = discoverProjectID
		}
		negotiated, discoverErr := resolver(ctx, accessToken, baseURL, httpClient)
		negotiated = strings.TrimSpace(negotiated)
		if discoverErr == nil && negotiated != "" {
			t.projectMu.Lock()
			t.config.Project = negotiated
			t.projectResolved = true
			t.projectResolvedAt = time.Now()
			t.projectMu.Unlock()
			slog.InfoContext(ctx, "antigravity: project negotiated via loadCodeAssist", slog.String("project", negotiated))

			return negotiated, nil
		}

		t.projectMu.Lock()
		t.projectResolved = false
		t.projectResolvedAt = time.Now()
		t.projectMu.Unlock()
		if project != "" {
			slog.WarnContext(ctx, "antigravity: loadCodeAssist failed, using saved project", slog.String("project", project), slog.Any("error", discoverErr))

			return project, nil
		}
		if discoverErr == nil {
			discoverErr = errors.New("loadCodeAssist returned no cloudaicompanionProject")
		}

		return "", discoverErr
	})
	if err != nil {
		return "", err
	}

	project, ok := v.(string)
	if !ok || strings.TrimSpace(project) == "" {
		return "", fmt.Errorf("project negotiation returned unexpected value %T", v)
	}

	return project, nil
}

// discoverProjectID calls loadCodeAssist using the same identity metadata as
// the desktop client. The configured endpoint is tried first when it is one of
// the provider endpoints; the normal inference order is then used as fallback.
func discoverProjectID(ctx context.Context, accessToken, baseURL string, httpClient *httpclient.HttpClient) (string, error) {
	if httpClient == nil {
		return "", errors.New("http client is nil")
	}

	requestCtx, cancel := context.WithTimeout(ctx, projectNegotiationTimeout)
	defer cancel()

	body, err := json.Marshal(loadCodeAssistRequest{
		Metadata: loadCodeAssistMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal loadCodeAssist request: %w", err)
	}

	var lastErr error
	for _, endpoint := range loadCodeAssistEndpoints(baseURL) {
		headers := make(http.Header)
		SetClientHeaders(headers)
		headers.Set("Content-Type", "application/json")
		headers.Set("Accept", "application/json")

		response, requestErr := httpClient.Do(requestCtx, &httpclient.Request{
			Method:  http.MethodPost,
			URL:     strings.TrimSuffix(endpoint, "/") + "/v1internal:loadCodeAssist",
			Headers: headers,
			Body:    body,
			Auth: &httpclient.AuthConfig{
				Type:   httpclient.AuthTypeBearer,
				APIKey: accessToken,
			},
		})
		if requestErr != nil {
			lastErr = fmt.Errorf("%s: %w", endpoint, requestErr)
			continue
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			lastErr = fmt.Errorf("%s: status %d", endpoint, response.StatusCode)
			continue
		}

		var payload loadCodeAssistResponse
		if err := json.Unmarshal(response.Body, &payload); err != nil {
			lastErr = fmt.Errorf("%s: decode response: %w", endpoint, err)
			continue
		}
		if project := projectIDFromLoadCodeAssist(payload.CloudAICompanionProject); project != "" {
			return project, nil
		}
		lastErr = fmt.Errorf("%s: response has no cloudaicompanionProject", endpoint)
	}

	if lastErr == nil {
		lastErr = errors.New("no loadCodeAssist endpoints configured")
	}

	return "", lastErr
}

func projectIDFromLoadCodeAssist(raw json.RawMessage) string {
	var project string
	if json.Unmarshal(raw, &project) == nil {
		return strings.TrimSpace(project)
	}

	var object struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
	}
	if json.Unmarshal(raw, &object) == nil {
		if strings.TrimSpace(object.ID) != "" {
			return strings.TrimSpace(object.ID)
		}

		return strings.TrimSpace(object.ProjectID)
	}

	return ""
}

func loadCodeAssistEndpoints(baseURL string) []string {
	seen := make(map[string]struct{})
	endpoints := make([]string, 0, len(LoadEndpoints)+1)
	add := func(endpoint string) {
		endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if endpoint == "" {
			return
		}
		if _, ok := seen[endpoint]; ok {
			return
		}
		seen[endpoint] = struct{}{}
		endpoints = append(endpoints, endpoint)
	}

	add(baseURL)
	for _, endpoint := range GetFallbackEndpoints() {
		add(endpoint)
	}
	for _, endpoint := range LoadEndpoints {
		add(endpoint)
	}

	return endpoints
}
