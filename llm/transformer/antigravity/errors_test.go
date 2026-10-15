package antigravity

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCooldownResponseErrorCarriesRateLimitStatusWithoutUpstreamError(t *testing.T) {
	retryAt := time.Now().Add(time.Minute)
	err := newCooldownResponseError("gemini-3.8-flash-high", []string{EndpointDaily, EndpointAutopush}, retryAt)

	var responseErr *llm.ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, http.StatusTooManyRequests, responseErr.StatusCode)
	require.Equal(t, CooldownErrorType, responseErr.Detail.Type)
	require.Equal(t, CooldownErrorCode, responseErr.Detail.Code)

	var cooldownErr *CooldownError
	require.ErrorAs(t, err, &cooldownErr)
	require.True(t, IsCooldownError(err))
	require.Equal(t, []string{EndpointDaily, EndpointAutopush}, cooldownErr.Endpoints)
	require.WithinDuration(t, retryAt, cooldownErr.RetryAt, time.Millisecond)
	var httpErr *httpclient.Error
	require.NotErrorAs(t, err, &httpErr)
}
