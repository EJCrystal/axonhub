package antigravity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestAntigravityMetrics_QueueAndBackoff(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	previousProvider := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previousProvider) })

	queue := newAntigravityGlobalQueue(1)
	metrics := antigravityMetricsForQueue(queue)
	label := antigravityQueueLabel{channelID: "26", channelName: "Anti-swenjie960428@gmail.com"}
	waitingLabel := antigravityQueueLabel{channelID: "55", channelName: "Anti-wenj73487@gmail.com"}

	require.NoError(t, queue.acquire(context.Background(), label))
	waitingCtx, cancelWaiting := context.WithCancel(context.Background())
	defer cancelWaiting()
	waitingAcquired := make(chan struct{})
	go func() {
		if err := queue.acquire(waitingCtx, waitingLabel); err == nil {
			close(waitingAcquired)
		}
	}()
	require.Eventually(t, func() bool {
		for _, snapshot := range queue.snapshot() {
			if snapshot.Label == waitingLabel {
				return snapshot.Waiting == 1
			}
		}

		return false
	}, time.Second, time.Millisecond)
	rm := metricdata.ResourceMetrics{}
	require.NoError(t, reader.Collect(context.Background(), &rm))
	waiting := findAntigravityGauge(t, rm, "axonhub_antigravity_queue_waiting")
	assertGaugeValueForChannel(t, waiting, "55", 1)
	inFlight := findAntigravityGauge(t, rm, "axonhub_antigravity_inflight")
	assertGaugeValueForChannel(t, inFlight, "26", 1)

	queue.releaseFor(label)
	require.Eventually(t, func() bool {
		select {
		case <-waitingAcquired:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	queue.releaseFor(waitingLabel)
	metrics.record429(context.Background(), label, "claude-sonnet-4-5", EndpointDaily)
	metrics.recordBackoff(context.Background(), label, "claude-sonnet-4-5", EndpointDaily, "exponential", 2*time.Second)

	rm = metricdata.ResourceMetrics{}
	require.NoError(t, reader.Collect(context.Background(), &rm))
	limited := findAntigravityCounter(t, rm, "axonhub_antigravity_429_total")
	assertCounterValueForChannel(t, limited, "26", 1)

	backoff := findAntigravityCounter(t, rm, "axonhub_antigravity_backoff_total")
	assertCounterValueForChannel(t, backoff, "26", 1)

	backoffSeconds := findAntigravityHistogram(t, rm, "axonhub_antigravity_backoff_seconds")
	require.NotEmpty(t, backoffSeconds.DataPoints)
	for _, dataPoint := range backoffSeconds.DataPoints {
		if value, ok := dataPoint.Attributes.Value(attribute.Key("channel_id")); ok && value.AsString() == "26" {
			assert.Equal(t, uint64(1), dataPoint.Count)
			assert.Equal(t, 2.0, dataPoint.Sum)

			return
		}
	}
	t.Fatal("histogram data point for channel 26 not found")
}

func findAntigravityGauge(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Gauge[int64] {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}

			gauge, ok := metric.Data.(metricdata.Gauge[int64])
			require.True(t, ok)

			return gauge
		}
	}

	t.Fatalf("gauge %q not found", name)
	return metricdata.Gauge[int64]{}
}

func findAntigravityCounter(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Sum[int64] {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}

			counter, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)

			return counter
		}
	}

	t.Fatalf("counter %q not found", name)
	return metricdata.Sum[int64]{}
}

func findAntigravityHistogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[float64] {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}

			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			require.True(t, ok)

			return histogram
		}
	}

	t.Fatalf("histogram %q not found", name)
	return metricdata.Histogram[float64]{}
}

func assertGaugeValueForChannel(t *testing.T, gauge metricdata.Gauge[int64], channelID string, want int64) {
	t.Helper()

	for _, dataPoint := range gauge.DataPoints {
		if value, ok := dataPoint.Attributes.Value(attribute.Key("channel_id")); ok && value.AsString() == channelID {
			assert.Equal(t, want, dataPoint.Value)

			return
		}
	}

	t.Fatalf("gauge data point for channel %s not found", channelID)
}

func assertCounterValueForChannel(t *testing.T, counter metricdata.Sum[int64], channelID string, want int64) {
	t.Helper()

	for _, dataPoint := range counter.DataPoints {
		if value, ok := dataPoint.Attributes.Value(attribute.Key("channel_id")); ok && value.AsString() == channelID {
			assert.Equal(t, want, dataPoint.Value)

			return
		}
	}

	t.Fatalf("counter data point for channel %s not found", channelID)
}
