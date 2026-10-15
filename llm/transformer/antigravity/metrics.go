package antigravity

import (
	"context"
	"reflect"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/looplj/axonhub/llm/httpclient"
)

const antigravityMeterName = "github.com/looplj/axonhub/llm/transformer/antigravity"

// antigravityMetrics exposes both admission state and upstream rate-limit
// behavior. The queue gauges are observable so they remain useful even when
// no request is currently emitting an event.
type antigravityMetrics struct {
	inFlight       metric.Int64ObservableGauge
	waiting        metric.Int64ObservableGauge
	queueWait      metric.Float64Histogram
	rateLimited429 metric.Int64Counter
	backoff        metric.Int64Counter
	backoffSeconds metric.Float64Histogram
}

type antigravityMetricsRegistryEntry struct {
	provider metric.MeterProvider
	metrics  *antigravityMetrics
}

var antigravityMetricsRegistry = struct {
	sync.Mutex
	entries map[*antigravityGlobalQueue]antigravityMetricsRegistryEntry
}{
	entries: make(map[*antigravityGlobalQueue]antigravityMetricsRegistryEntry),
}

func antigravityMetricsForQueue(queue *antigravityGlobalQueue) *antigravityMetrics {
	if queue == nil {
		return &antigravityMetrics{}
	}

	provider := otel.GetMeterProvider()
	antigravityMetricsRegistry.Lock()
	defer antigravityMetricsRegistry.Unlock()

	if entry, ok := antigravityMetricsRegistry.entries[queue]; ok && sameMeterProvider(entry.provider, provider) {
		return entry.metrics
	}

	metrics, err := newAntigravityMetrics(otel.Meter(antigravityMeterName), queue)
	if err != nil {
		// Instrument creation errors must never break a provider request. The
		// configured metric names are static, so this is only a defensive
		// fallback for an incompatible MeterProvider.
		metrics = &antigravityMetrics{}
	}
	antigravityMetricsRegistry.entries[queue] = antigravityMetricsRegistryEntry{
		provider: provider,
		metrics:  metrics,
	}

	return metrics
}

func sameMeterProvider(left, right metric.MeterProvider) bool {
	if left == nil || right == nil {
		return left == right
	}

	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() || !leftValue.Type().Comparable() {
		return false
	}

	return leftValue.Interface() == rightValue.Interface()
}

func newAntigravityMetrics(meter metric.Meter, queue *antigravityGlobalQueue) (*antigravityMetrics, error) {
	inFlight, err := meter.Int64ObservableGauge(
		"axonhub_antigravity_inflight",
		metric.WithDescription("Antigravity requests currently holding the process-wide queue slot"),
		metric.WithUnit("requests"),
	)
	if err != nil {
		return nil, err
	}

	waiting, err := meter.Int64ObservableGauge(
		"axonhub_antigravity_queue_waiting",
		metric.WithDescription("Antigravity requests waiting for the process-wide queue slot"),
		metric.WithUnit("requests"),
	)
	if err != nil {
		return nil, err
	}

	queueWait, err := meter.Float64Histogram(
		"axonhub_antigravity_queue_wait_seconds",
		metric.WithDescription("Time spent waiting for the Antigravity process-wide queue slot"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	rateLimited429, err := meter.Int64Counter(
		"axonhub_antigravity_429_total",
		metric.WithDescription("Antigravity upstream responses with HTTP 429"),
		metric.WithUnit("requests"),
	)
	if err != nil {
		return nil, err
	}

	backoff, err := meter.Int64Counter(
		"axonhub_antigravity_backoff_total",
		metric.WithDescription("Antigravity retries that applied rate-limit backoff"),
		metric.WithUnit("retries"),
	)
	if err != nil {
		return nil, err
	}

	backoffSeconds, err := meter.Float64Histogram(
		"axonhub_antigravity_backoff_seconds",
		metric.WithDescription("Duration of Antigravity rate-limit backoff delays"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		for _, snapshot := range queue.snapshot() {
			attrs := metric.WithAttributes(antigravityQueueAttrs(snapshot.Label)...)
			observer.ObserveInt64(inFlight, int64(snapshot.InFlight), attrs)
			observer.ObserveInt64(waiting, int64(snapshot.Waiting), attrs)
		}

		return nil
	}, inFlight, waiting)
	if err != nil {
		return nil, err
	}

	return &antigravityMetrics{
		inFlight:       inFlight,
		waiting:        waiting,
		queueWait:      queueWait,
		rateLimited429: rateLimited429,
		backoff:        backoff,
		backoffSeconds: backoffSeconds,
	}, nil
}

func antigravityQueueLabelFromRequest(request *httpclient.Request) antigravityQueueLabel {
	if request == nil || request.Metadata == nil {
		return normalizeAntigravityQueueLabel(antigravityQueueLabel{})
	}

	return normalizeAntigravityQueueLabel(antigravityQueueLabel{
		channelID:   request.Metadata[httpclient.ChannelIDMetadataKey],
		channelName: request.Metadata[httpclient.ChannelNameMetadataKey],
	})
}

func antigravityQueueAttrs(label antigravityQueueLabel) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("channel_id", label.channelID),
		attribute.String("channel_name", label.channelName),
	}
}

func antigravityRequestAttrs(label antigravityQueueLabel, model, endpoint string) []attribute.KeyValue {
	attrs := antigravityQueueAttrs(label)
	attrs = append(attrs,
		attribute.String("model", model),
		attribute.String("endpoint", endpoint),
	)

	return attrs
}

func (m *antigravityMetrics) observeQueueWait(ctx context.Context, label antigravityQueueLabel, wait time.Duration) {
	if m == nil || m.queueWait == nil {
		return
	}

	m.queueWait.Record(ctx, wait.Seconds(), metric.WithAttributes(antigravityQueueAttrs(label)...))
}

func (m *antigravityMetrics) record429(ctx context.Context, label antigravityQueueLabel, model, endpoint string) {
	if m == nil || m.rateLimited429 == nil {
		return
	}

	m.rateLimited429.Add(ctx, 1, metric.WithAttributes(antigravityRequestAttrs(label, model, endpoint)...))
}

func (m *antigravityMetrics) recordBackoff(ctx context.Context, label antigravityQueueLabel, model, endpoint, source string, delay time.Duration) {
	if m == nil || m.backoff == nil || m.backoffSeconds == nil {
		return
	}

	attrs := antigravityRequestAttrs(label, model, endpoint)
	attrs = append(attrs, attribute.String("backoff_source", source))
	options := metric.WithAttributes(attrs...)
	m.backoff.Add(ctx, 1, options)
	m.backoffSeconds.Record(ctx, delay.Seconds(), options)
}
