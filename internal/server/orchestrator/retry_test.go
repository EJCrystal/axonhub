package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

// testNetError implements net.Error so the retry predicates can be exercised
// against Timeout()/Temporary() transport failures in unit tests. The
// Temporary method is retained to mirror the net.Error interface the
// production predicate examines.
type testNetError struct {
	timeout   bool
	temporary bool
	message   string
}

func (e *testNetError) Error() string {
	return e.message
}

func (e *testNetError) Timeout() bool {
	return e.timeout
}

func (e *testNetError) Temporary() bool {
	return e.temporary
}

func TestDeriveLoadBalancerStrategy(t *testing.T) {
	defaultStrategy := "adaptive"
	retryPolicy := &biz.RetryPolicy{
		LoadBalancerStrategy: defaultStrategy,
	}

	tests := []struct {
		name     string
		apiKey   *ent.APIKey
		expected string
	}{
		{
			name:     "apiKey is nil",
			apiKey:   nil,
			expected: defaultStrategy,
		},
		{
			name: "active profile is nil",
			apiKey: &ent.APIKey{
				Profiles: nil,
			},
			expected: defaultStrategy,
		},
		{
			name: "active profile name is empty",
			apiKey: &ent.APIKey{
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "",
				},
			},
			expected: defaultStrategy,
		},
		{
			name: "active profile not found in profiles list",
			apiKey: &ent.APIKey{
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "non-existent",
					Profiles: []objects.APIKeyProfile{
						{Name: "other"},
					},
				},
			},
			expected: defaultStrategy,
		},
		{
			name: "load balance strategy is nil in active profile",
			apiKey: &ent.APIKey{
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "default",
					Profiles: []objects.APIKeyProfile{
						{
							Name:                "default",
							LoadBalanceStrategy: nil,
						},
					},
				},
			},
			expected: defaultStrategy,
		},
		{
			name: "load balance strategy is empty in active profile",
			apiKey: &ent.APIKey{
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "default",
					Profiles: []objects.APIKeyProfile{
						{
							Name:                "default",
							LoadBalanceStrategy: lo.ToPtr(""),
						},
					},
				},
			},
			expected: defaultStrategy,
		},
		{
			name: "load balance strategy is system_default in active profile",
			apiKey: &ent.APIKey{
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "default",
					Profiles: []objects.APIKeyProfile{
						{
							Name:                "default",
							LoadBalanceStrategy: lo.ToPtr("system_default"),
						},
					},
				},
			},
			expected: defaultStrategy,
		},
		{
			name: "load balance strategy is set to specific value in active profile",
			apiKey: &ent.APIKey{
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "default",
					Profiles: []objects.APIKeyProfile{
						{
							Name:                "default",
							LoadBalanceStrategy: lo.ToPtr("failover"),
						},
					},
				},
			},
			expected: "failover",
		},
		{
			name: "load balance strategy is round-robin in active profile",
			apiKey: &ent.APIKey{
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "default",
					Profiles: []objects.APIKeyProfile{
						{
							Name:                "default",
							LoadBalanceStrategy: lo.ToPtr(biz.LoadBalancerStrategyRoundRobin),
						},
					},
				},
			},
			expected: biz.LoadBalancerStrategyRoundRobin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveLoadBalancerStrategy(retryPolicy, tt.apiKey)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDeriveRoutingPolicyPriority(t *testing.T) {
	retryPolicy := &biz.RetryPolicy{
		LoadBalancerStrategy: biz.LoadBalancerStrategyAdaptive,
		TraceStickyMode:      biz.TraceStickyPreferPreviousChannel,
	}
	modelPolicy := &ModelRoutingPolicy{
		LoadBalancerStrategy: biz.LoadBalancerStrategyFailover,
		TraceStickyMode:      string(biz.TraceStickyDisabled),
	}

	t.Run("model overrides system", func(t *testing.T) {
		policy := deriveRoutingPolicy(retryPolicy, nil, modelPolicy)
		assert.Equal(t, biz.LoadBalancerStrategyFailover, policy.LoadBalancerStrategy)
		assert.Equal(t, biz.TraceStickyDisabled, policy.TraceStickyMode)
	})

	t.Run("profile default inherits model", func(t *testing.T) {
		apiKey := &ent.APIKey{Profiles: &objects.APIKeyProfiles{
			ActiveProfile: "default",
			Profiles: []objects.APIKeyProfile{{
				Name:                "default",
				LoadBalanceStrategy: lo.ToPtr(objects.RoutingPolicyDefault),
				TraceStickyMode:     lo.ToPtr(objects.RoutingPolicyDefault),
			}},
		}}

		policy := deriveRoutingPolicy(retryPolicy, apiKey, modelPolicy)
		assert.Equal(t, biz.LoadBalancerStrategyFailover, policy.LoadBalancerStrategy)
		assert.Equal(t, biz.TraceStickyDisabled, policy.TraceStickyMode)
	})

	t.Run("profile overrides model and system", func(t *testing.T) {
		apiKey := &ent.APIKey{Profiles: &objects.APIKeyProfiles{
			ActiveProfile: "default",
			Profiles: []objects.APIKeyProfile{{
				Name:                "default",
				LoadBalanceStrategy: lo.ToPtr(biz.LoadBalancerStrategyRoundRobin),
				TraceStickyMode:     lo.ToPtr(string(biz.TraceStickyPreferPreviousChannel)),
			}},
		}}

		policy := deriveRoutingPolicy(retryPolicy, apiKey, modelPolicy)
		assert.Equal(t, biz.LoadBalancerStrategyRoundRobin, policy.LoadBalancerStrategy)
		assert.Equal(t, biz.TraceStickyPreferPreviousChannel, policy.TraceStickyMode)
	})
}

func TestExtractStatusCodeFromError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{
			name:     "error is nil",
			err:      nil,
			expected: 0,
		},
		{
			name: "httpclient.Error",
			err: &httpclient.Error{
				StatusCode: http.StatusTooManyRequests,
			},
			expected: http.StatusTooManyRequests,
		},
		{
			name: "llm.ResponseError",
			err: &llm.ResponseError{
				StatusCode: http.StatusInternalServerError,
			},
			expected: http.StatusInternalServerError,
		},
		{
			name:     "wrapped httpclient.Error",
			err:      errors.New("wrapped: " + (&httpclient.Error{StatusCode: 401}).Error()), // This won't work with errors.As unless we use fmt.Errorf with %w
			expected: 0,
		},
		{
			name:     "wrapped httpclient.Error with %w",
			err:      errors.Join(errors.New("error"), &httpclient.Error{StatusCode: 401}),
			expected: 401,
		},
		{
			name:     "generic error",
			err:      errors.New("generic error"),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractStatusCodeFromError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "error is nil",
			err:      nil,
			expected: false,
		},
		{
			name: "429 Too Many Requests is retryable",
			err: &httpclient.Error{
				StatusCode: http.StatusTooManyRequests,
			},
			expected: true,
		},
		{
			name: "400 Bad Request is not retryable",
			err: &httpclient.Error{
				StatusCode: http.StatusBadRequest,
			},
			expected: false,
		},
		{
			name: "500 Internal Server Error is retryable",
			err: &llm.ResponseError{
				StatusCode: http.StatusInternalServerError,
			},
			expected: true,
		},
		{
			name:     "generic error is not retryable",
			err:      errors.New("generic error"),
			expected: false,
		},
		{
			name:     "wrapped upstream EOF is retryable",
			err:      fmt.Errorf("HTTP stream request failed: %w", io.EOF),
			expected: true,
		},
		{
			name:     "unexpected EOF is retryable",
			err:      io.ErrUnexpectedEOF,
			expected: true,
		},
		{
			name:     "net.Error with Timeout is retryable",
			err:      &testNetError{timeout: true, message: "i/o timeout"},
			expected: true,
		},
		{
			name:     "net.Error with Temporary is retryable",
			err:      &testNetError{temporary: true, message: "connection temporarily refused"},
			expected: true,
		},
		{
			name:     "net.Error without Timeout or Temporary is not retryable",
			err:      &testNetError{message: "non-timeout net error"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRetryableErrorForChannel(t *testing.T) {
	channel := &biz.Channel{
		Channel: &ent.Channel{
			Settings: &objects.ChannelSettings{
				RetryableStatusCodes: []int{400, 403},
				RetryableErrorPatterns: []objects.RetryableErrorPattern{
					{Pattern: "Console API returned 403"},
					{Pattern: `Console API returned \d+`, Regex: true},
				},
			},
		},
	}

	tests := []struct {
		name     string
		err      error
		channel  *biz.Channel
		expected bool
	}{
		{
			name:     "error is nil",
			err:      nil,
			channel:  channel,
			expected: false,
		},
		{
			name: "default retryable status remains retryable",
			err: &httpclient.Error{
				StatusCode: http.StatusInternalServerError,
			},
			channel:  nil,
			expected: true,
		},
		{
			name:     "wrapped incomplete Responses stream is retryable without channel settings",
			err:      fmt.Errorf("failed to stream request: %w", llm.ErrStreamIncomplete),
			channel:  nil,
			expected: true,
		},
		{
			name:     "wrapped upstream EOF is retryable without channel settings",
			err:      fmt.Errorf("failed to stream request: %w", io.EOF),
			channel:  nil,
			expected: true,
		},
		{
			name:     "net.Error with Timeout is retryable without channel settings",
			err:      &testNetError{timeout: true, message: "i/o timeout"},
			channel:  nil,
			expected: true,
		},
		{
			name:     "net.Error with Temporary is retryable without channel settings",
			err:      &testNetError{temporary: true, message: "connection temporarily refused"},
			channel:  nil,
			expected: true,
		},
		{
			name:     "net.Error without Timeout or Temporary is not retryable without channel settings",
			err:      &testNetError{message: "non-timeout net error"},
			channel:  nil,
			expected: false,
		},
		{
			name: "configured 400 status is retryable",
			err: &httpclient.Error{
				StatusCode: http.StatusBadRequest,
			},
			channel:  channel,
			expected: true,
		},
		{
			name: "unconfigured 401 status is not retryable",
			err: &httpclient.Error{
				StatusCode: http.StatusUnauthorized,
			},
			channel:  channel,
			expected: false,
		},
		{
			name:     "configured error text is retryable",
			err:      errors.New("failed to stream request: error: Console API returned 403, code: upstream_error, type: upstream_error"),
			channel:  channel,
			expected: true,
		},
		{
			name:     "configured error regex is retryable",
			err:      errors.New("failed to stream request: error: Console API returned 502, code: upstream_error, type: upstream_error"),
			channel:  channel,
			expected: true,
		},
		{
			name:     "unmatched error text is not retryable",
			err:      errors.New("failed to stream request: error: credentials rejected"),
			channel:  channel,
			expected: false,
		},
		{
			name: "structured_code",
			err: &llm.ResponseError{
				Detail: llm.ErrorDetail{Code: "server_is_overloaded"},
			},
			channel: &biz.Channel{Channel: &ent.Channel{
				Settings: &objects.ChannelSettings{
					RetryableErrorPatterns: []objects.RetryableErrorPattern{{Pattern: "server_is_overloaded"}},
				},
			}},
			expected: true,
		},
		{
			name: "unrelated_code",
			err: &llm.ResponseError{
				Detail: llm.ErrorDetail{Code: "invalid_api_key"},
			},
			channel: &biz.Channel{Channel: &ent.Channel{
				Settings: &objects.ChannelSettings{
					RetryableErrorPatterns: []objects.RetryableErrorPattern{{Pattern: "server_is_overloaded"}},
				},
			}},
			expected: false,
		},
		{
			name: "empty_pattern",
			err: &llm.ResponseError{
				Detail: llm.ErrorDetail{Code: "server_is_overloaded"},
			},
			channel: &biz.Channel{Channel: &ent.Channel{
				Settings: &objects.ChannelSettings{},
			}},
			expected: false,
		},
		{
			name: "configured status is not retryable without channel settings",
			err: &httpclient.Error{
				StatusCode: http.StatusBadRequest,
			},
			channel:  &biz.Channel{Channel: &ent.Channel{}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableErrorForChannel(tt.err, tt.channel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func newRetryTestChannel(t *testing.T, name string, credentials objects.ChannelCredentials) *biz.Channel {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", "file:retry-channel?mode=memory&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := authz.WithTestBypass(context.Background())
	entity := client.Channel.Create().
		SetType(entchannel.TypeOpenai).
		SetName(name).
		SetBaseURL("https://api.example.com/v1").
		SetCredentials(credentials).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SaveX(ctx)

	channelService := biz.NewChannelServiceForTest(client)
	channel, err := channelService.TestBuildChannel(entity)
	require.NoError(t, err)
	return channel
}

func TestPersistentOutboundTransformer_MultiKeyRetryOn429(t *testing.T) {
	ctx := contexts.EnsureContainer(t.Context())
	ctx = contexts.WithTrace(ctx, &ent.Trace{TraceID: "trace-429"})

	ch := newRetryTestChannel(t, "test-multi-key", objects.ChannelCredentials{
		APIKeys: []string{"key-1", "key-2", "key-3"},
	})

	candidate := &ChannelModelsCandidate{
		Channel: ch,
		Models: []biz.ChannelModelEntry{
			{RequestModel: "gpt-4", ActualModel: "gpt-4"},
		},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
		CurrentCandidateIndex:   0,
		CurrentCandidate:        candidate,
		CurrentModelIndex:       0,
		OriginalModel:           "gpt-4",
	}

	transformer := &PersistentOutboundTransformer{
		state: state,
	}

	rateLimitErr := &httpclient.Error{StatusCode: http.StatusTooManyRequests}

	// 1. Initial attempt with key-1
	state.CurrentAPIKey = "key-1"
	contexts.WithChannelAPIKey(ctx, "key-1")

	// Since key-2 and key-3 are available, CanRetry must return true
	assert.True(t, transformer.CanRetry(rateLimitErr), "should allow retry when other keys are available")

	// Prepare for retry
	err := transformer.PrepareForRetry(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 0, state.CurrentModelIndex, "should not advance model index while keys remain")
	assert.Empty(t, state.CurrentAPIKey)
	assert.Contains(t, state.FailedAPIKeys[ch.ID], "key-1")

	// 2. Second attempt with key-2
	state.CurrentAPIKey = "key-2"
	contexts.WithChannelAPIKey(ctx, "key-2")

	assert.True(t, transformer.CanRetry(rateLimitErr), "should allow retry when key-3 is still available")

	err = transformer.PrepareForRetry(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 0, state.CurrentModelIndex)
	assert.Contains(t, state.FailedAPIKeys[ch.ID], "key-2")

	// 3. Third attempt with key-3 (last key)
	state.CurrentAPIKey = "key-3"
	contexts.WithChannelAPIKey(ctx, "key-3")

	// All keys for this model are now exhausted, CanRetry must return false for 429 so pipeline switches channels
	assert.False(t, transformer.CanRetry(rateLimitErr), "should reject 429 retry when all keys are exhausted")
}

func TestPersistentOutboundTransformer_TransformRequestUsesNextKeyAfterRetry(t *testing.T) {
	keys := []string{"key-1", "key-2", "key-3"}
	ch := newRetryTestChannel(t, "test-transform-retry-key", objects.ChannelCredentials{
		APIKeys: keys,
		APIKeyModels: []objects.APIKeyModels{
			{APIKey: "key-1", Models: []string{"gpt-4"}},
			{APIKey: "key-2", Models: []string{"gpt-4"}},
			{APIKey: "key-3", Models: []string{"gpt-4"}},
		},
	})
	candidate := &ChannelModelsCandidate{
		Channel: ch,
		Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
	}
	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
		CurrentCandidate:        candidate,
		OriginalModel:           "gpt-4",
	}
	transformer := &PersistentOutboundTransformer{state: state}

	ctx := contexts.EnsureContainer(context.Background())
	ctx = contexts.WithTrace(ctx, &ent.Trace{TraceID: "trace-transform-retry"})
	text := "Hello"
	request := &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{{
			Role:    "user",
			Content: llm.MessageContent{Content: &text},
		}},
	}

	firstRequest, err := transformer.TransformRequest(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, firstRequest.Auth)
	firstKey := firstRequest.Auth.APIKey
	require.Contains(t, keys, firstKey)

	require.NoError(t, transformer.PrepareForRetry(ctx))
	secondRequest, err := transformer.TransformRequest(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, secondRequest.Auth)
	require.Contains(t, keys, secondRequest.Auth.APIKey)
	require.NotEqual(t, firstKey, secondRequest.Auth.APIKey)
	require.Contains(t, state.FailedAPIKeys[ch.ID], firstKey)
}

func TestPersistentOutboundTransformer_SingleKeyNoRetryOn429(t *testing.T) {
	ch := newRetryTestChannel(t, "test-single-key", objects.ChannelCredentials{
		APIKeys: []string{"single-key"},
	})

	candidate := &ChannelModelsCandidate{
		Channel: ch,
		Models: []biz.ChannelModelEntry{
			{RequestModel: "gpt-4", ActualModel: "gpt-4"},
		},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
		CurrentCandidateIndex:   0,
		CurrentCandidate:        candidate,
		CurrentModelIndex:       0,
		CurrentAPIKey:           "single-key",
		OriginalModel:           "gpt-4",
	}

	transformer := &PersistentOutboundTransformer{
		state: state,
	}

	rateLimitErr := &httpclient.Error{StatusCode: http.StatusTooManyRequests}

	// Single key channel has no alternative keys, 429 should immediately skip same-channel retry
	assert.False(t, transformer.CanRetry(rateLimitErr))
}

func TestPersistentOutboundTransformer_SingleKeyTransientRetryKeepsKey(t *testing.T) {
	ch := newRetryTestChannel(t, "test-single-key-transient", objects.ChannelCredentials{
		APIKeys: []string{"single-key"},
	})
	candidate := &ChannelModelsCandidate{
		Channel: ch,
		Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
	}
	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
		CurrentCandidate:        candidate,
		CurrentModelIndex:       0,
		CurrentAPIKey:           "single-key",
	}
	transformer := &PersistentOutboundTransformer{state: state}
	ctx := contexts.EnsureContainer(context.Background())
	contexts.WithChannelAPIKey(ctx, "single-key")

	transientErr := &httpclient.Error{StatusCode: http.StatusInternalServerError}
	assert.True(t, transformer.CanRetry(transientErr))
	require.NoError(t, transformer.PrepareForRetry(ctx))
	assert.Empty(t, state.FailedAPIKeys)

	text := "Hello"
	request := &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{{
			Role:    "user",
			Content: llm.MessageContent{Content: &text},
		}},
	}
	transformed, err := transformer.TransformRequest(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, transformed.Auth)
	assert.Equal(t, "single-key", transformed.Auth.APIKey)
}

func TestPersistentOutboundTransformer_MultiKeyRetryOn401(t *testing.T) {
	ctx := contexts.EnsureContainer(t.Context())

	ch := newRetryTestChannel(t, "test-401-key", objects.ChannelCredentials{
		APIKeys: []string{"key-1", "key-2"},
	})

	candidate := &ChannelModelsCandidate{
		Channel: ch,
		Models: []biz.ChannelModelEntry{
			{RequestModel: "gpt-4", ActualModel: "gpt-4"},
		},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
		CurrentCandidateIndex:   0,
		CurrentCandidate:        candidate,
		CurrentModelIndex:       0,
		CurrentAPIKey:           "key-1",
		OriginalModel:           "gpt-4",
	}

	transformer := &PersistentOutboundTransformer{
		state: state,
	}

	unauthErr := &httpclient.Error{StatusCode: http.StatusUnauthorized}

	// With key-2 available, 401 allows same-channel retry
	assert.True(t, transformer.CanRetry(unauthErr))

	err := transformer.PrepareForRetry(ctx)
	assert.NoError(t, err)

	// Now key-2 fails with 401 and no more keys remain
	state.CurrentAPIKey = "key-2"
	contexts.WithChannelAPIKey(ctx, "key-2")
	assert.False(t, transformer.CanRetry(unauthErr), "should reject 401 retry when no more keys remain")
}

func TestPersistentOutboundTransformer_PerKeyModelsFilteringOnRetry(t *testing.T) {
	ctx := contexts.EnsureContainer(t.Context())

	ch := newRetryTestChannel(t, "test-per-key-models", objects.ChannelCredentials{
		APIKeys: []string{"key-1", "key-2", "key-3"},
		APIKeyModels: []objects.APIKeyModels{
			{APIKey: "key-1", Models: []string{"gpt-4"}},
			{APIKey: "key-2", Models: []string{"gpt-3.5"}}, // Does not support gpt-4
			{APIKey: "key-3", Models: []string{"gpt-4"}},
		},
	})

	candidate := &ChannelModelsCandidate{
		Channel: ch,
		Models: []biz.ChannelModelEntry{
			{RequestModel: "gpt-4", ActualModel: "gpt-4"},
		},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
		CurrentCandidateIndex:   0,
		CurrentCandidate:        candidate,
		CurrentModelIndex:       0,
		CurrentAPIKey:           "key-1",
		OriginalModel:           "gpt-4",
	}

	transformer := &PersistentOutboundTransformer{
		state: state,
	}

	rateLimitErr := &httpclient.Error{StatusCode: http.StatusTooManyRequests}

	// key-1 fails: key-3 supports gpt-4 (even though key-2 does not), so retry is allowed
	assert.True(t, transformer.CanRetry(rateLimitErr))

	err := transformer.PrepareForRetry(ctx)
	assert.NoError(t, err)

	// key-3 fails: key-2 does not support gpt-4, so no more keys remain for gpt-4
	state.CurrentAPIKey = "key-3"
	contexts.WithChannelAPIKey(ctx, "key-3")
	assert.False(t, transformer.CanRetry(rateLimitErr), "key-2 does not support gpt-4, retry should be rejected")
}
