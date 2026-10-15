package antigravity

import (
	"context"
	"sort"
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

	mu       sync.Mutex
	inFlight map[antigravityQueueLabel]int
	waiting  map[antigravityQueueLabel]int
	known    map[antigravityQueueLabel]struct{}
}

// antigravityQueueLabel identifies the selected AxonHub channel for queue
// metrics. It deliberately stays in the llm module so the queue does not
// depend on server-side entities.
type antigravityQueueLabel struct {
	channelID   string
	channelName string
}

type antigravityQueueSnapshot struct {
	Label    antigravityQueueLabel
	InFlight int
	Waiting  int
}

func newAntigravityGlobalQueue(capacity int) *antigravityGlobalQueue {
	if capacity < 1 {
		capacity = 1
	}

	return &antigravityGlobalQueue{
		slots:    make(chan struct{}, capacity),
		inFlight: make(map[antigravityQueueLabel]int),
		waiting:  make(map[antigravityQueueLabel]int),
		known:    make(map[antigravityQueueLabel]struct{}),
	}
}

func (q *antigravityGlobalQueue) acquire(ctx context.Context, labels ...antigravityQueueLabel) error {
	label := normalizeAntigravityQueueLabel(firstQueueLabel(labels))

	q.mu.Lock()
	q.known[label] = struct{}{}
	q.mu.Unlock()

	select {
	case q.slots <- struct{}{}:
		q.recordAcquire(label)
		return nil
	default:
	}

	q.changeWaiting(label, 1)
	defer q.changeWaiting(label, -1)

	select {
	case q.slots <- struct{}{}:
		q.recordAcquire(label)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *antigravityGlobalQueue) release() {
	q.releaseFor(antigravityQueueLabel{})
}

func (q *antigravityGlobalQueue) releaseFor(label antigravityQueueLabel) {
	select {
	case <-q.slots:
		q.recordRelease(normalizeAntigravityQueueLabel(label))
	default:
	}
}

func firstQueueLabel(labels []antigravityQueueLabel) antigravityQueueLabel {
	if len(labels) == 0 {
		return antigravityQueueLabel{}
	}

	return labels[0]
}

func normalizeAntigravityQueueLabel(label antigravityQueueLabel) antigravityQueueLabel {
	if label.channelID == "" {
		label.channelID = "unknown"
	}
	if label.channelName == "" {
		label.channelName = "unknown"
	}

	return label
}

func (q *antigravityGlobalQueue) recordAcquire(label antigravityQueueLabel) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.inFlight[label]++
}

func (q *antigravityGlobalQueue) recordRelease(label antigravityQueueLabel) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.inFlight[label] > 0 {
		q.inFlight[label]--

		return
	}

	// Tests and older callers can release a raw queue slot without supplying a
	// label. Attribute that release to the only active label (or any active
	// label for a queue with capacity > 1).
	for activeLabel, count := range q.inFlight {
		if count <= 0 {
			continue
		}

		q.inFlight[activeLabel]--

		break
	}
}

func (q *antigravityGlobalQueue) changeWaiting(label antigravityQueueLabel, delta int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.waiting[label] += delta
	if q.waiting[label] <= 0 {
		delete(q.waiting, label)
	}
}

func (q *antigravityGlobalQueue) snapshot() []antigravityQueueSnapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	labels := make([]antigravityQueueLabel, 0, len(q.known))
	for label := range q.known {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool {
		if labels[i].channelID != labels[j].channelID {
			return labels[i].channelID < labels[j].channelID
		}

		return labels[i].channelName < labels[j].channelName
	})

	snapshots := make([]antigravityQueueSnapshot, 0, len(labels))
	for _, label := range labels {
		snapshots = append(snapshots, antigravityQueueSnapshot{
			Label:    label,
			InFlight: q.inFlight[label],
			Waiting:  q.waiting[label],
		})
	}

	return snapshots
}

var defaultAntigravityGlobalQueue = newAntigravityGlobalQueue(DefaultGlobalConcurrency)

// antigravityQueueSlot owns one global queue admission. It is shared by the
// executor and the returned stream so a slot cannot be released twice.
type antigravityQueueSlot struct {
	queue *antigravityGlobalQueue
	label antigravityQueueLabel
	once  sync.Once
}

func (s *antigravityQueueSlot) release() {
	if s == nil || s.queue == nil {
		return
	}

	s.once.Do(func() { s.queue.releaseFor(s.label) })
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
