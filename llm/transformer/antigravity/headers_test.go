package antigravity

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestSanitizedInboundRequestRemovesCrossProtocolHeadersWithoutMutation(t *testing.T) {
	original := &httpclient.Request{Headers: http.Header{
		"Anthropic-Beta":    {"prompt-caching-2024-07-31"},
		"X-Stainless-Lang":  {"go"},
		"X-Codex-Turn":      {"turn-1"},
		"X-Opencode-Tools":  {"1"},
		"X-App":             {"claude-code"},
		"Thread-Id":         {"thread-1"},
		"Originator":        {"codex"},
		"X-Custom-Business": {"keep-me"},
	}}

	sanitized := sanitizedInboundRequest(original)
	require.NotSame(t, original, sanitized)
	require.Empty(t, sanitized.Headers.Get("Anthropic-Beta"))
	require.Empty(t, sanitized.Headers.Get("X-Stainless-Lang"))
	require.Empty(t, sanitized.Headers.Get("X-Codex-Turn"))
	require.Empty(t, sanitized.Headers.Get("X-Opencode-Tools"))
	require.Empty(t, sanitized.Headers.Get("X-App"))
	require.Empty(t, sanitized.Headers.Get("Thread-Id"))
	require.Empty(t, sanitized.Headers.Get("Originator"))
	require.Equal(t, "keep-me", sanitized.Headers.Get("X-Custom-Business"))

	// The request retained for tracing and persistence must be unchanged.
	require.Equal(t, "prompt-caching-2024-07-31", original.Headers.Get("Anthropic-Beta"))
	require.Equal(t, "go", original.Headers.Get("X-Stainless-Lang"))

	providerRequest := &httpclient.Request{Headers: http.Header{
		"User-Agent":       {GetUserAgent()},
		"X-Client-Version": {GetVersion()},
	}}
	merged := httpclient.MergeInboundRequest(providerRequest, sanitized)
	require.Equal(t, GetUserAgent(), merged.Headers.Get("User-Agent"))
	require.Equal(t, GetVersion(), merged.Headers.Get("X-Client-Version"))
	require.Equal(t, "keep-me", merged.Headers.Get("X-Custom-Business"))
}
