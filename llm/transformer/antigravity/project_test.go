package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/samber/lo"
)

func TestDiscoverProjectIDUsesLoadCodeAssistHandshake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1internal:loadCodeAssist", r.URL.Path)
		require.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
		require.Equal(t, GetUserAgent(), r.Header.Get("User-Agent"))
		require.Equal(t, "antigravity", r.Header.Get("X-Client-Name"))
		require.Equal(t, ClientMetadata, r.Header.Get("Client-Metadata"))

		var request loadCodeAssistRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, loadCodeAssistMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		}, request.Metadata)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cloudaicompanionProject":{"id":"negotiated-project"}}`)
	}))
	defer server.Close()

	project, err := discoverProjectID(
		context.Background(),
		"access-token",
		server.URL,
		httpclient.NewHttpClientWithClient(server.Client()),
	)
	require.NoError(t, err)
	require.Equal(t, "negotiated-project", project)
}

func TestTransformRequestNegotiatesProjectBeforeBuildingEnvelope(t *testing.T) {
	var loadCalls atomic.Int32
	transport := mockRoundTripper{roundTrip: func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.String() == TokenURL:
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"access_token":"access-token","expires_in":3600,"token_type":"Bearer"}`)),
				Header:     make(http.Header),
			}, nil
		case request.URL.Path == "/v1internal:loadCodeAssist":
			loadCalls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"cloudaicompanionProject":"negotiated-project"}`)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected request: %s", request.URL.String())
			return nil, nil
		}
	}}

	transformer, err := NewTransformer(
		Config{BaseURL: "https://control-plane.test", APIKey: "refresh-token|saved-project"},
		WithHTTPClient(httpclient.NewHttpClientWithClient(&http.Client{Transport: &transport})),
	)
	require.NoError(t, err)

	request := &llm.Request{
		Model: "gemini-2.5-flash",
		Messages: []llm.Message{{
			Role:    "user",
			Content: llm.MessageContent{Content: lo.ToPtr("hello")},
		}},
	}
	first, err := transformer.TransformRequest(context.Background(), request)
	require.NoError(t, err)
	second, err := transformer.TransformRequest(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, int32(1), loadCalls.Load())

	for _, outbound := range []*httpclient.Request{first, second} {
		var envelope AntigravityEnvelope
		require.NoError(t, json.Unmarshal(outbound.Body, &envelope))
		require.Equal(t, "negotiated-project", envelope.Project)
	}
}

func TestProjectIDFromLoadCodeAssist(t *testing.T) {
	require.Equal(t, "string-project", projectIDFromLoadCodeAssist(json.RawMessage(`"string-project"`)))
	require.Equal(t, "object-project", projectIDFromLoadCodeAssist(json.RawMessage(`{"id":"object-project"}`)))
	require.Equal(t, "project-id", projectIDFromLoadCodeAssist(json.RawMessage(`{"projectId":"project-id"}`)))
	require.Empty(t, projectIDFromLoadCodeAssist(json.RawMessage(`null`)))
}

func TestTransformerResolveProjectCachesNegotiation(t *testing.T) {
	var calls atomic.Int32
	transformer := &Transformer{
		config: Config{Project: "saved-project", BaseURL: EndpointDaily},
		projectResolver: func(context.Context, string, string, *httpclient.HttpClient) (string, error) {
			calls.Add(1)
			return "negotiated-project", nil
		},
	}

	first, err := transformer.resolveProject(context.Background(), "access-token")
	require.NoError(t, err)
	second, err := transformer.resolveProject(context.Background(), "access-token")
	require.NoError(t, err)
	require.Equal(t, "negotiated-project", first)
	require.Equal(t, first, second)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "negotiated-project", transformer.config.Project)
}

func TestTransformerResolveProjectFallsBackBriefly(t *testing.T) {
	var calls atomic.Int32
	transformer := &Transformer{
		config: Config{Project: "saved-project", BaseURL: EndpointDaily},
		projectResolver: func(context.Context, string, string, *httpclient.HttpClient) (string, error) {
			calls.Add(1)
			return "", context.DeadlineExceeded
		},
	}

	first, err := transformer.resolveProject(context.Background(), "access-token")
	require.NoError(t, err)
	second, err := transformer.resolveProject(context.Background(), "access-token")
	require.NoError(t, err)
	require.Equal(t, "saved-project", first)
	require.Equal(t, first, second)
	require.Equal(t, int32(1), calls.Load())

	transformer.projectResolvedAt = time.Now().Add(-projectFailureCacheTTL - time.Second)
	_, err = transformer.resolveProject(context.Background(), "access-token")
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
}

func TestLoadCodeAssistEndpointsPreferConfiguredEndpoint(t *testing.T) {
	endpoints := loadCodeAssistEndpoints(EndpointDaily + "/")
	require.NotEmpty(t, endpoints)
	require.Equal(t, EndpointDaily, endpoints[0])
	require.Equal(t, len(endpoints), len(uniqueStrings(endpoints)))
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
