package orchestrator

import (
	"strings"

	"github.com/tidwall/gjson"
)

// upstreamModelFromResponse reads only model metadata explicitly reported in a
// provider's raw response or stream event. The unified llm.Response.Model is not
// suitable here: transformers may synthesize it from the request for compatibility.
func upstreamModelFromResponse(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}

	// OpenAI-compatible and Ollama responses, Gemini, Anthropic message_start,
	// Responses API events, Antigravity envelopes, and Cline response envelopes.
	// Do not search recursively: tool arguments or generated content can contain
	// unrelated model fields.
	for _, path := range []string{
		"model",
		"modelVersion",
		"message.model",
		"response.model",
		"response.modelVersion",
		"data.model",
	} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return value.String()
		}
	}

	return ""
}
