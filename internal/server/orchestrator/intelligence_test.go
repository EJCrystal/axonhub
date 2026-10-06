package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

// newIntelligenceTestServer records submissions and serves scripted task states.
func newIntelligenceTestServer(t *testing.T, states []string) (*httptest.Server, *map[string]string, *atomic.Int64) {
	t.Helper()

	received := map[string]string{}
	pollsUsed := &atomic.Int64{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tests":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"task-1","benchmark":"pelican","status":"running"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tests/task-1":
			index := int(pollsUsed.Add(1)) - 1
			if index >= len(states) {
				index = len(states) - 1
			}
			_, _ = w.Write([]byte(states[index]))
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(server.Close)

	return server, &received, pollsUsed
}

// TestIntelligenceEvaluatorSubmitUsesHTMLMode verifies the free HTML mode payload:
// the pelican benchmark plus the generated source, and no upstream credentials.
func TestIntelligenceEvaluatorSubmitUsesHTMLMode(t *testing.T) {
	server, received, _ := newIntelligenceTestServer(t, []string{`{"status":"succeeded"}`})

	evaluator := newIntelligenceEvaluator(httpclient.NewHttpClient(), server.URL+"/")

	taskID, err := evaluator.submit(context.Background(), "<html>pelican</html>")
	require.NoError(t, err)
	require.Equal(t, "task-1", taskID)
	require.Equal(t, "pelican", (*received)["benchmark"])
	require.Equal(t, "<html>pelican</html>", (*received)["html"])

	_, hasCredential := (*received)["api_key"]
	require.False(t, hasCredential, "HTML mode must not submit upstream credentials")
	_, hasBaseURL := (*received)["base_url"]
	require.False(t, hasBaseURL, "HTML mode must not submit an upstream base url")
}

// TestIntelligenceEvaluatorWaitMapsAssessment verifies a terminal task is mapped
// onto the per-key result, including the quality verdict.
func TestIntelligenceEvaluatorWaitMapsAssessment(t *testing.T) {
	server, _, _ := newIntelligenceTestServer(t, []string{
		`{"status":"running","phase":"classifying"}`,
		`{"status":"succeeded","phase":"complete","assessment":{"quality":"degraded","label":"疑似降智","reason":"分类结果为降智特征"},"result":{"duration_ms":0,"input_tokens":0,"output_tokens":0}}`,
	})

	evaluator := newIntelligenceEvaluator(httpclient.NewHttpClient(), server.URL)

	result, err := evaluator.wait(context.Background(), "task-1")
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "degraded", result.Quality)
	require.Equal(t, "疑似降智", result.Label)
	require.Equal(t, "分类结果为降智特征", result.Reason)
	// The service only scores the source we submit, so its own duration and
	// token counters stay zero in HTML mode and are deliberately not surfaced.
	require.Zero(t, result.DurationMs)
}

// TestIntelligenceEvaluatorPollKeepsPollingWhileRunning verifies a non-terminal
// state is not reported as a finished evaluation.
func TestIntelligenceEvaluatorPollKeepsPollingWhileRunning(t *testing.T) {
	server, _, _ := newIntelligenceTestServer(t, []string{`{"status":"running","phase":"generating"}`})

	evaluator := newIntelligenceEvaluator(httpclient.NewHttpClient(), server.URL)

	result, done, err := evaluator.poll(context.Background(), "task-1")
	require.NoError(t, err)
	require.False(t, done)
	require.Nil(t, result)
}

// TestIntelligenceEvaluatorWaitReportsFailure verifies a failed task surfaces the
// service error instead of a quality verdict.
func TestIntelligenceEvaluatorWaitReportsFailure(t *testing.T) {
	server, _, _ := newIntelligenceTestServer(t, []string{`{"status":"failed","error":"upstream rejected"}`})

	evaluator := newIntelligenceEvaluator(httpclient.NewHttpClient(), server.URL)

	result, err := evaluator.wait(context.Background(), "task-1")
	require.NoError(t, err)
	require.False(t, result.Success)
	require.NotNil(t, result.Error)
	require.Equal(t, "upstream rejected", *result.Error)
}

// TestIntelligenceEvaluatorDefaultsBaseURL verifies an empty override falls back
// to the public detection service.
func TestIntelligenceEvaluatorDefaultsBaseURL(t *testing.T) {
	require.Equal(t, defaultIntelligenceBaseURL, newIntelligenceEvaluator(httpclient.NewHttpClient(), "  ").baseURL)
}

// TestStripMarkdownFence verifies a model that wraps its HTML in a code fence
// still reaches the classifier with raw markup.
func TestStripMarkdownFence(t *testing.T) {
	require.Equal(t, "<html></html>", stripMarkdownFence("\n```html\n<html></html>\n```\n"))
	require.Equal(t, "<html></html>", stripMarkdownFence("<html></html>"))
	require.Equal(t, "", stripMarkdownFence("   "))
}

// TestResolveIntelligenceKeysFiltersForeignKeys verifies a stale key selection
// cannot probe a credential that is not configured on the channel.
func TestResolveIntelligenceKeysFiltersForeignKeys(t *testing.T) {
	ctx, client := setupTest(t)
	channelService, _, _, _ := setupTestServices(t, client)

	created, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Intelligence Channel").
		SetBaseURL("https://api.example.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKeys: []string{"key-a", "key-b"}}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	loaded, err := channelService.GetChannel(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, loaded.GetEnabledAPIKeys(), 2)

	selected := resolveIntelligenceKeys(loaded, []string{"key-a", "key-a", "foreign-key"})
	require.Equal(t, []string{"key-a"}, selected)

	all := resolveIntelligenceKeys(loaded, nil)
	require.Len(t, all, 2)
	require.ElementsMatch(t, []string{"key-a", "key-b"}, all)
}

func TestHTMLForResult(t *testing.T) {
	t.Run("returns the generated source", func(t *testing.T) {
		html := "<html><body>ok</body></html>"
		require.Equal(t, &html, htmlForResult(html))
	})

	t.Run("omits an empty document", func(t *testing.T) {
		require.Nil(t, htmlForResult(""))
	})

	t.Run("omits a document past the size cap", func(t *testing.T) {
		require.Nil(t, htmlForResult(strings.Repeat("x", intelligenceHTMLMaxBytes+1)))
	})

	t.Run("keeps a document at the size cap", func(t *testing.T) {
		html := strings.Repeat("x", intelligenceHTMLMaxBytes)
		require.Equal(t, &html, htmlForResult(html))
	})
}

// TestIntelligenceHTTP1ClientDisablesH2 verifies the evaluator forces HTTP/1.1,
// since manxue.ai's HTTP/2 streams intermittently die behind a CONNECT proxy.
func TestIntelligenceHTTP1ClientDisablesH2(t *testing.T) {
	base := httpclient.NewHttpClient()
	derived := newIntelligenceHTTP1Client(base)
	require.NotNil(t, derived)

	native := derived.GetNativeClient()
	require.NotNil(t, native)
	transport, ok := native.Transport.(*http.Transport)
	require.True(t, ok, "derived client should keep an *http.Transport")
	require.False(t, transport.ForceAttemptHTTP2)
	require.NotNil(t, transport.TLSNextProto)
	require.Empty(t, transport.TLSNextProto)
}

// TestIntelligenceHTTP1ClientInheritsBaseProxy checks the derived transport
// keeps the base client's proxy behavior instead of silently going direct.
func TestIntelligenceHTTP1ClientInheritsBaseProxy(t *testing.T) {
	base := httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{Type: httpclient.ProxyTypeDisabled})
	derived := newIntelligenceHTTP1Client(base)
	transport, ok := derived.GetNativeClient().Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.Proxy)

	u, err := url.Parse("https://manxue.ai/api/v1/tests")
	require.NoError(t, err)
	proxyURL, err := transport.Proxy(&http.Request{URL: u})
	require.NoError(t, err)
	require.Nil(t, proxyURL, "disabled proxy config should stay disabled")
}

// TestIntelligenceSubmitRetriesTransportErrors verifies a lost submit is
// retried, since no task is created when the response never arrives.
func TestIntelligenceSubmitRetriesTransportErrors(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			conn, _, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("{\"id\":\"task-1\"}"))
	}))
	t.Cleanup(server.Close)

	evaluator := newIntelligenceEvaluator(httpclient.NewHttpClient(), server.URL+"/")
	taskID, err := evaluator.submit(context.Background(), "<html>x</html>")
	require.NoError(t, err)
	require.Equal(t, "task-1", taskID)
	require.EqualValues(t, 3, attempts.Load())
}

// TestIntelligenceSubmitDoesNotRetryClientErrors verifies a rejected payload is
// reported right away instead of burning three identical attempts.
func TestIntelligenceSubmitDoesNotRetryClientErrors(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"error\":\"bad\"}"))
	}))
	t.Cleanup(server.Close)

	evaluator := newIntelligenceEvaluator(httpclient.NewHttpClient(), server.URL+"/")
	_, err := evaluator.submit(context.Background(), "<html>x</html>")
	require.Error(t, err)
	require.EqualValues(t, 1, attempts.Load())
}
