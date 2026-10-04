package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminGraphQLTimeout(t *testing.T) {
	const (
		defaultTimeout = 30 * time.Second
		longTimeout    = 10 * time.Minute
	)

	tests := []struct {
		name string
		body string
		want time.Duration
	}{
		{
			name: "intelligence check gets the long timeout",
			body: `{"query":"mutation EvaluateChannelIntelligence($input: IntelligenceEvaluateInput!) { evaluateChannelIntelligence(input: $input) { total } }"}`,
			want: longTimeout,
		},
		{
			name: "intelligence check is matched despite a custom operation name",
			body: `{"query":"mutation Custom { evaluateChannelIntelligence(input: {channelID: \"1\"}) { total } }"}`,
			want: longTimeout,
		},
		{
			name: "anonymous intelligence check is matched",
			body: `{"query":"mutation { evaluateChannelIntelligence(input: {channelID: \"1\"}) { total } }"}`,
			want: longTimeout,
		},
		{
			name: "other admin mutation keeps the default timeout",
			body: `{"query":"mutation CreateChannel($input: CreateChannelInput!) { createChannel(input: $input) { id } }"}`,
			want: defaultTimeout,
		},
		{
			name: "query selecting the same field keeps the default timeout",
			body: `{"query":"query EvaluateChannelIntelligence { evaluateChannelIntelligence { total } }"}`,
			want: defaultTimeout,
		},
		{
			name: "malformed json keeps the default timeout",
			body: `{not json`,
			want: defaultTimeout,
		},
		{
			name: "unparsable query keeps the default timeout",
			body: `{"query":"mutation { evaluateChannelIntelligence("}`,
			want: defaultTimeout,
		},
		{
			name: "empty query keeps the default timeout",
			body: `{"query":"   "}`,
			want: defaultTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/graphql", strings.NewReader(tt.body))

			if got := adminGraphQLTimeout(req, defaultTimeout, longTimeout); got != tt.want {
				t.Fatalf("adminGraphQLTimeout() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAdminGraphQLTimeoutRestoresRequestBody(t *testing.T) {
	body := `{"query":"mutation EvaluateChannelIntelligence { evaluateChannelIntelligence { total } }"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/graphql", strings.NewReader(body))

	adminGraphQLTimeout(req, time.Second, time.Minute)

	restored, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("reading restored body: %v", err)
	}
	if string(restored) != body {
		t.Fatalf("body after inspection = %q, want %q", restored, body)
	}
}
