package antigravity

import (
	"context"
	"sync"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

// DefaultGlobalConcurrency is deliberately conservative because Antigravity
// channels can share the same upstream project and model rate bucket.
const DefaultGlobalConcurrency = 1

// antigravityGlobalQueue is a process-wide admission queue for Antigravity
// requests. A buffered channel gives us a context-aware blocking wait path
// without allowing another request to reach the upstream while the current
// request is still draining.
type antigravityGlobalQueue struct {
	slots chan struct{}
}

func newAntigravityGlobalQueue(capacity int) *antigravityGlobalQueue {
	if capacity < 1 {
		capacity = 1
	}

	return &antigravityGlobalQueue{slots: make(chan struct{}, capacity)}
}

func (q *antigravityGlobalQueue) acquire(ctx context.Context) error {
	select {
	case q.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *antigravityGlobalQueue) release() {
	select {
	case <-q.slots:
	default:
	}
}

var defaultAntigravityGlobalQueue = newAntigravityGlobalQueue(DefaultGlobalConcurrency)

// antigravityQueueSlot owns one global queue admission. It is shared by the
// executor and the returned stream so a slot cannot be released twice.
type antigravityQueueSlot struct {
	queue *antigravityGlobalQueue
	once  sync.Once
}

func (s *antigravityQueueSlot) release() {
	if s == nil || s.queue == nil {
		return
	}

	s.once.Do(s.queue.release)
}

// antigravityQueuedStream keeps the global slot until the upstream stream has
// been fully consumed or explicitly closed. The upstream is closed before the
// slot is returned so a waiting request cannot start while the old connection
// is still being torn down.
type antigravityQueuedStream struct {
	stream    streams.Stream[*httpclient.StreamEvent]
	slot      *antigravityQueueSlot
	closeOnce sync.Once
	closeErr  error
}

func (s *antigravityQueuedStream) Next() bool {
	if s.stream.Next() {
		return true
	}

	_ = s.closeUpstream()
	s.slot.release()

	return false
}

func (s *antigravityQueuedStream) Current() *httpclient.StreamEvent {
	return s.stream.Current()
}

func (s *antigravityQueuedStream) Err() error {
	return s.stream.Err()
}

func (s *antigravityQueuedStream) Close() error {
	// Close the upstream before admitting the next queued request.
	err := s.closeUpstream()
	s.slot.release()

	return err
}

func (s *antigravityQueuedStream) closeUpstream() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.stream.Close()
	})

	return s.closeErr
}
