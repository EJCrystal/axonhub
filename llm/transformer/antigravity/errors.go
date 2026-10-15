package antigravity

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/looplj/axonhub/llm"
)

const (
	// CooldownErrorCode identifies a local endpoint-admission failure. It is
	// deliberately distinct from an upstream 429 so channel auto-disable rules
	// cannot treat a local cooldown as a provider response.
	CooldownErrorCode = "antigravity_endpoints_cooldown"
	CooldownErrorType = "rate_limit_error"
)

// CooldownError reports that every endpoint for a model is temporarily
// skipped by the local health tracker. It is not an upstream HTTP error.
type CooldownError struct {
	Model     string
	Endpoints []string
	RetryAt   time.Time
}

func (e *CooldownError) Error() string {
	if e == nil {
		return "antigravity endpoints are in cooldown"
	}

	message := fmt.Sprintf("all antigravity endpoints in cooldown for model %s", e.Model)
	if !e.RetryAt.IsZero() {
		message += fmt.Sprintf(" (retry after %s)", e.RetryAt.UTC().Format(time.RFC3339))
	}

	return message
}

// IsCooldownError reports whether err contains a local Antigravity cooldown
// failure, including errors wrapped by the request pipeline or API policy.
func IsCooldownError(err error) bool {
	var cooldownErr *CooldownError
	return errors.As(err, &cooldownErr)
}

func newCooldownResponseError(model string, endpoints []string, retryAt time.Time) error {
	cause := &CooldownError{
		Model:     model,
		Endpoints: append([]string(nil), endpoints...),
		RetryAt:   retryAt,
	}

	return &llm.ResponseError{
		StatusCode: http.StatusTooManyRequests,
		Detail: llm.ErrorDetail{
			Message: cause.Error(),
			Type:    CooldownErrorType,
			Code:    CooldownErrorCode,
		},
		Cause: cause,
	}
}

func cooldownRetryAt(tracker *AntigravityHealthTracker, model string, endpoints []string) time.Time {
	if tracker == nil {
		return time.Time{}
	}

	var retryAt time.Time
	for _, endpoint := range endpoints {
		failure := tracker.GetFailure(model, endpoint)
		if failure == nil || !time.Now().Before(failure.CooldownUntil) {
			continue
		}
		if retryAt.IsZero() || failure.CooldownUntil.Before(retryAt) {
			retryAt = failure.CooldownUntil
		}
	}

	return retryAt
}
