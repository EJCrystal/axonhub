package antigravity

import (
	"net/http"
	"strings"

	"github.com/looplj/axonhub/llm/httpclient"
)

var antigravityInboundHeadersToStrip = map[string]struct{}{
	"Accept":            {},
	"Authorization":     {},
	"Cache-Control":     {},
	"Client-Metadata":   {},
	"Connection":        {},
	"Content-Length":    {},
	"Content-Type":      {},
	"Host":              {},
	"Pragma":            {},
	"User-Agent":        {},
	"X-App":             {},
	"X-Client-Name":     {},
	"X-Client-Version":  {},
	"X-Goog-Api-Client": {},
	"X-Goog-Api-Key":    {},
	"Originator":        {},
	"Session-Id":        {},
	"Thread-Id":         {},
}

var antigravityInboundHeaderPrefixesToStrip = []string{
	"Anthropic-",
	"OpenAI-",
	"X-Claude-",
	"X-Codex-",
	"X-OpenAI-",
	"X-Opencode-",
	"X-Stainless-",
}

// sanitizedInboundRequest returns a shallow request copy with only the
// provider-identity headers removed. The original request is left untouched
// because it is also used by tracing and request persistence.
func sanitizedInboundRequest(request *httpclient.Request) *httpclient.Request {
	if request == nil || len(request.Headers) == 0 {
		return request
	}

	copyRequest := *request
	copyRequest.Headers = request.Headers.Clone()
	for name := range copyRequest.Headers {
		canonical := http.CanonicalHeaderKey(name)
		if _, ok := antigravityInboundHeadersToStrip[canonical]; ok || hasAntigravityInboundHeaderPrefix(canonical) {
			delete(copyRequest.Headers, name)
		}
	}

	return &copyRequest
}

func hasAntigravityInboundHeaderPrefix(name string) bool {
	for _, prefix := range antigravityInboundHeaderPrefixesToStrip {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}
