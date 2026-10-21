package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

// failingStream yields the events it was given and then reports a transport
// error, which is what an upstream does when it closes the HTTP/2 stream after
// the answer has already been delivered.
type failingStream struct {
	items  []*httpclient.StreamEvent
	index  int
	err    error
	closed bool
}

func (s *failingStream) Next() bool {
	if s.index < len(s.items) {
		return true
	}

	return false
}

func (s *failingStream) Current() *httpclient.StreamEvent {
	if s.index < len(s.items) {
		item := s.items[s.index]
		s.index++

		return item
	}

	return nil
}

func (s *failingStream) Err() error { return s.err }

func (s *failingStream) Close() error {
	s.closed = true

	return nil
}

// A relay that closes the HTTP/2 stream after sending the whole answer would
// otherwise be recorded as a failure, and the multi-minute generation would be
// thrown away and re-run. What the check asked for is the content, so complete
// content decides the outcome and the way the stream ended only explains the
// timing.
func TestHandleStreamResponseKeepsContentWhenTheStreamFailsAfterwards(t *testing.T) {
	processor := &TestChannelOrchestrator{}

	events := []*httpclient.StreamEvent{
		{Data: []byte(`{"choices":[{"delta":{"content":"<!DOCTYPE html>"}}]}`)},
		{Data: []byte(`{"choices":[{"delta":{"content":"<html></html>"}}]}`)},
	}

	stream := &failingStream{
		items: events,
		err:   errors.New("error: Upstream HTTP/2 stream failed, code: upstream_http2_stream_error, type: error"),
	}

	result, err := processor.handleStreamResponse(context.Background(), stream, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "a complete answer is a success even when the stream then fails")
	require.NotNil(t, result.Message)
	require.Equal(t, "<!DOCTYPE html><html></html>", *result.Message)
	require.Nil(t, result.Error)
	require.True(t, stream.closed, "the stream is still closed")
}

// A stream that dies before delivering anything is a plain failure: there is no
// content to stand on, and the error has to reach the run so it can be retried.
func TestHandleStreamResponseFailsWhenNothingArrived(t *testing.T) {
	processor := &TestChannelOrchestrator{}

	stream := &failingStream{
		err: errors.New("error: Upstream HTTP/2 stream failed, code: upstream_http2_stream_error, type: error"),
	}

	result, err := processor.handleStreamResponse(context.Background(), stream, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success, "nothing arrived, so nothing succeeded")
	require.NotNil(t, result.Error)
	require.Contains(t, *result.Error, "upstream_http2_stream_error")
}

// An empty answer is not content: whitespace is what a stream sends when the
// upstream gave up before writing anything, and it must not read as a success.
func TestHandleStreamResponseTreatsWhitespaceAsEmpty(t *testing.T) {
	processor := &TestChannelOrchestrator{}

	stream := &failingStream{
		items: []*httpclient.StreamEvent{
			{Data: []byte(`{"choices":[{"delta":{"content":"   "}}]}`)},
		},
		err: errors.New("stream reset"),
	}

	result, err := processor.handleStreamResponse(context.Background(), stream, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success, "whitespace is not an answer")
}

// A reasoning model can consume the whole completion budget before writing any
// visible content. That is not an empty response: it has to be reported as a
// budget exhaustion so the intelligence check can retry with a larger ceiling.
func TestHandleStreamResponseReportsReasoningBudgetExhaustion(t *testing.T) {
	processor := &TestChannelOrchestrator{}

	stream := &failingStream{
		items: []*httpclient.StreamEvent{
			{Data: []byte(`{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`)},
			{Data: []byte(`{"choices":[{"delta":{},"finish_reason":"length"}]}`)},
		},
	}

	result, err := processor.handleStreamResponse(context.Background(), stream, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success)
	require.Equal(t, "length", result.FinishReason)
	require.NotNil(t, result.Error)
	require.Equal(t, errReasoningBudgetExhausted.Error(), *result.Error)
}

func TestNextIntelligenceGenerationBudgetEscalatesOnce(t *testing.T) {
	next, retry := nextIntelligenceGenerationBudget(intelligenceGenerateMaxTokens, errReasoningBudgetExhausted, false)
	require.True(t, retry)
	require.Equal(t, int64(intelligenceGenerateReasoningMaxTokens), next)

	_, retry = nextIntelligenceGenerationBudget(next, errReasoningBudgetExhausted, false)
	require.False(t, retry, "the larger budget is the last attempt")

	_, retry = nextIntelligenceGenerationBudget(intelligenceGenerateMaxTokens, errors.New("stream reset"), false)
	require.False(t, retry, "only budget exhaustion escalates")

	_, retry = nextIntelligenceGenerationBudget(intelligenceGenerateMaxTokens, errReasoningBudgetExhausted, true)
	require.False(t, retry, "websocket responses do not carry a completion budget")
}
