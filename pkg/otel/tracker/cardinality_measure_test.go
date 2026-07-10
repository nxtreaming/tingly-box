package tracker

// Empirical measurement for issue #1255: how much heap the cumulative
// metrics SDK retains per unique attribute set (the old behavior attached
// per-request latency as an attribute, making every request a new set).
// Run with: go test ./pkg/otel/tracker/ -run TestCardinalityRetention -v

import (
	"context"
	"runtime"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/metric"
)

func heapAfterGC() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestCardinalityRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement only")
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("measure")

	tracker, err := NewTokenTracker(meter)
	if err != nil {
		t.Fatal(err)
	}

	const n = 10000
	ctx := context.Background()

	// OLD behavior: unique latency attribute per request → new attribute set
	// per request on every instrument.
	base := heapAfterGC()
	for i := 0; i < n; i++ {
		attrs := []attribute.KeyValue{
			attribute.String("llm.provider", "openai"),
			attribute.String("llm.provider.uuid", "prov-uuid-1234"),
			attribute.String("llm.model", "gpt-4o"),
			attribute.String("llm.request.model", "claude-sonnet"),
			attribute.String("llm.scenario", "claude_code"),
			attribute.Bool("llm.streaming", true),
			attribute.String("llm.response.status", "success"),
			attribute.Int("llm.latency.ms", 10000+i), // unique per request
		}
		tracker.inputTokens.Add(ctx, 100, metric.WithAttributes(attrs...))
		tracker.outputTokens.Add(ctx, 50, metric.WithAttributes(attrs...))
		tracker.totalTokens.Add(ctx, 150, metric.WithAttributes(attrs...))
		tracker.requestCount.Add(ctx, 1, metric.WithAttributes(attrs...))
		tracker.requestDuration.Record(ctx, float64(10000+i), metric.WithAttributes(attrs...))
	}
	oldRetained := int64(heapAfterGC()) - int64(base)
	runtime.KeepAlive(provider)
	t.Logf("OLD (latency as attribute): %d requests retain %.2f MB (%.0f bytes/request, forever)",
		n, float64(oldRetained)/1024/1024, float64(oldRetained)/n)

	// NEW behavior: bounded attribute sets.
	reader2 := sdkmetric.NewManualReader()
	provider2 := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader2))
	tracker2, err := NewTokenTracker(provider2.Meter("measure2"))
	if err != nil {
		t.Fatal(err)
	}
	base2 := heapAfterGC()
	for i := 0; i < n; i++ {
		tracker2.RecordUsage(ctx, UsageOptions{
			Provider: "openai", ProviderUUID: "prov-uuid-1234",
			Model: "gpt-4o", RequestModel: "claude-sonnet",
			Scenario: "claude_code", Streamed: true, Status: "success",
			InputTokens: 100, OutputTokens: 50, LatencyMs: 10000 + i,
		})
	}
	newRetained := int64(heapAfterGC()) - int64(base2)
	t.Logf("NEW (latency as histogram value only): %d requests retain %.2f MB",
		n, float64(newRetained)/1024/1024)
}
