package antigravity

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestAntigravityGlobalQueue_SerializesRequests(t *testing.T) {
	queue := newAntigravityGlobalQueue(1)
	require.NoError(t, queue.acquire(context.Background()))

	acquired := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if err := queue.acquire(context.Background()); err != nil {
			t.Errorf("acquire queued slot: %v", err)
			return
		}
		close(acquired)
		queue.release()
	}()

	select {
	case <-acquired:
		t.Fatal("second request bypassed the global queue")
	case <-time.After(20 * time.Millisecond):
	}

	queue.release()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("queued request was not admitted after the slot was released")
	}

	<-finished
}

func TestAntigravityQueuedStream_ReleasesOnEOF(t *testing.T) {
	queue := newAntigravityGlobalQueue(1)
	require.NoError(t, queue.acquire(context.Background()))

	var closed atomic.Bool
	stream := &testAntigravityStream{
		events: []*httpclient.StreamEvent{{Data: []byte("event")}},
		onClose: func() {
			closed.Store(true)
		},
	}
	queued := &antigravityQueuedStream{
		stream: stream,
		slot:   &antigravityQueueSlot{queue: queue},
	}

	assert.True(t, queued.Next())
	assert.False(t, queued.Next())
	assert.True(t, closed.Load())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, queue.acquire(ctx))
	queue.release()
}

func TestExecutor_429UsesExponentialBackoff(t *testing.T) {
	callCount := 0
	mockClient := &mockHTTPClient{
		doFunc: func(ctx context.Context, request *httpclient.Request) (*httpclient.Response, error) {
			callCount++
			if callCount <= 2 {
				return nil, &httpclient.Error{
					Method:     request.Method,
					URL:        request.URL,
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
				}
			}

			return &httpclient.Response{StatusCode: http.StatusOK}, nil
		},
	}
	executor := NewExecutor(mockClient)
	var delays []time.Duration
	executor.retryWait = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	resp, err := executor.Do(context.Background(), &httpclient.Request{
		Method: http.MethodPost,
		URL:    EndpointDaily + "/v1internal:generateContent",
		Metadata: map[string]string{
			"antigravity_model": "claude-sonnet-4-5",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, delays)
}

func TestExecutor_429RetryAfterTakesPriority(t *testing.T) {
	callCount := 0
	mockClient := &mockHTTPClient{
		doFunc: func(ctx context.Context, request *httpclient.Request) (*httpclient.Response, error) {
			callCount++
			if callCount == 1 {
				return nil, &httpclient.Error{
					Method:     request.Method,
					URL:        request.URL,
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
					Headers:    http.Header{"Retry-After": []string{"0"}},
				}
			}

			return &httpclient.Response{StatusCode: http.StatusOK}, nil
		},
	}
	executor := NewExecutor(mockClient)
	var delays []time.Duration
	executor.retryWait = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	_, err := executor.Do(context.Background(), &httpclient.Request{
		Method: http.MethodPost,
		URL:    EndpointDaily + "/v1internal:generateContent",
		Metadata: map[string]string{
			"antigravity_model": "claude-sonnet-4-5",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{0}, delays)
}

type testAntigravityStream struct {
	events  []*httpclient.StreamEvent
	index   int
	onClose func()
}

func (s *testAntigravityStream) Next() bool {
	if s.index >= len(s.events) {
		return false
	}

	s.index++

	return true
}

func (s *testAntigravityStream) Current() *httpclient.StreamEvent {
	return s.events[s.index-1]
}

func (s *testAntigravityStream) Err() error { return nil }

func (s *testAntigravityStream) Close() error {
	if s.onClose != nil {
		s.onClose()
	}

	return nil
}

var _ streams.Stream[*httpclient.StreamEvent] = (*testAntigravityStream)(nil)
