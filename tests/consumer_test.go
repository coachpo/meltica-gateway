package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/coachpo/meltica/core/consumer"
	"github.com/coachpo/meltica/core/events"
)

func TestConsumerWrapperAutoRecycleOnReturn(t *testing.T) {
	recycler, _ := newTrackingRecycler(t)
	metrics := consumer.NewConsumerMetrics(prometheus.NewRegistry())
	wrapper := consumer.NewWrapper("cons-1", recycler, metrics)

	ev := &events.Event{Kind: events.KindMarketData, RoutingVersion: 10}
	called := false
	err := wrapper.Invoke(context.Background(), ev, func(ctx context.Context, event *events.Event) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("invoke returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected consumer lambda to be invoked")
	}
	if !recycler.WasRecycled(ev) {
		t.Fatalf("expected event to be recycled")
	}
	if v := testutil.ToFloat64(metrics.InvocationsCounter("cons-1")); v != 1 {
		t.Fatalf("expected 1 invocation, got %f", v)
	}
	if v := testutil.ToFloat64(metrics.PanicCounter("cons-1")); v != 0 {
		t.Fatalf("expected 0 panics, got %f", v)
	}
}

func TestConsumerWrapperAutoRecycleOnPanic(t *testing.T) {
	recycler, _ := newTrackingRecycler(t)
	metrics := consumer.NewConsumerMetrics(prometheus.NewRegistry())
	wrapper := consumer.NewWrapper("cons-2", recycler, metrics)

	ev := &events.Event{Kind: events.KindMarketData, RoutingVersion: 20}
	err := wrapper.Invoke(context.Background(), ev, func(ctx context.Context, event *events.Event) error {
		panic("boom")
	})
	if err == nil {
		t.Fatalf("expected panic error")
	}
	if !strings.Contains(err.Error(), "consumer panic") {
		t.Fatalf("panic error missing prefix: %v", err)
	}
	if !strings.Contains(err.Error(), "consumer_test.go") {
		t.Fatalf("expected stack trace in panic error: %v", err)
	}
	if !recycler.WasRecycled(ev) {
		t.Fatalf("expected event to be recycled after panic")
	}
	if v := testutil.ToFloat64(metrics.PanicCounter("cons-2")); v != 1 {
		t.Fatalf("expected 1 panic, got %f", v)
	}
}

func TestConsumerWrapperFiltering(t *testing.T) {
	recycler, _ := newTrackingRecycler(t)
	metrics := consumer.NewConsumerMetrics(prometheus.NewRegistry())
	wrapper := consumer.NewWrapper("cons-3", recycler, metrics)
	wrapper.UpdateMinVersion(100)

	ev := &events.Event{Kind: events.KindMarketData, RoutingVersion: 50}
	called := false
	if err := wrapper.Invoke(context.Background(), ev, func(ctx context.Context, event *events.Event) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("invoke returned error: %v", err)
	}
	if called {
		t.Fatalf("expected lambda not to be invoked for stale event")
	}
	if !recycler.WasRecycled(ev) {
		t.Fatalf("expected stale event to be recycled")
	}
	if v := testutil.ToFloat64(metrics.FilteredCounter("cons-3")); v != 1 {
		t.Fatalf("expected filtered counter to increment, got %f", v)
	}

	critical := &events.Event{Kind: events.KindExecReport, RoutingVersion: 1}
	criticalCalled := false
	if err := wrapper.Invoke(context.Background(), critical, func(ctx context.Context, event *events.Event) error {
		criticalCalled = true
		return nil
	}); err != nil {
		t.Fatalf("critical invoke returned error: %v", err)
	}
	if !recycler.WasRecycled(critical) {
		t.Fatalf("expected critical event recycle")
	}
	if !criticalCalled {
		t.Fatalf("expected critical event to bypass filter")
	}
}

func TestConsumerWrapperCriticalKindsAlwaysProcess(t *testing.T) {
	recycler, _ := newTrackingRecycler(t)
	wrapper := consumer.NewWrapper("cons-critical", recycler, nil)
	wrapper.UpdateMinVersion(100)

	criticalKinds := []events.EventKind{events.KindExecReport, events.KindControlAck, events.KindControlResult}
	for _, kind := range criticalKinds {
		ev := &events.Event{Kind: kind, RoutingVersion: 1}
		if !wrapper.ShouldProcess(ev) {
			t.Fatalf("expected critical kind %v to process", kind)
		}
	}
}

func TestConsumerWrapperUpdateMinVersion(t *testing.T) {
	recycler, _ := newTrackingRecycler(t)
	wrapper := consumer.NewWrapper("cons-update", recycler, nil)

	event := &events.Event{Kind: events.KindMarketData, RoutingVersion: 1}
	if !wrapper.ShouldProcess(event) {
		t.Fatalf("expected default min version to process event")
	}

	wrapper.UpdateMinVersion(50)
	if wrapper.ShouldProcess(event) {
		t.Fatalf("expected event below min version to be filtered")
	}

	updated := &events.Event{Kind: events.KindMarketData, RoutingVersion: 100}
	if !wrapper.ShouldProcess(updated) {
		t.Fatalf("expected event meeting min version to process")
	}
}

func TestConsumerRegistryInvokesWrapper(t *testing.T) {
	recycler, _ := newTrackingRecycler(t)
	metrics := consumer.NewConsumerMetrics(prometheus.NewRegistry())
	wrapper := consumer.NewWrapper("registry-consumer", recycler, metrics)
	registry := consumer.NewRegistry()
	registry.Register(wrapper)

	ev := &events.Event{Kind: events.KindMarketData, RoutingVersion: 10}
	called := false
	if err := registry.Invoke(context.Background(), "registry-consumer", ev, func(ctx context.Context, event *events.Event) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("registry invoke returned err: %v", err)
	}
	if !called {
		t.Fatalf("expected registry to invoke lambda")
	}
	if !recycler.WasRecycled(ev) {
		t.Fatalf("expected registry-invoked event to recycle")
	}
}
