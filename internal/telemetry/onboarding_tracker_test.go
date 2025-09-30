package telemetry

import (
	"testing"
	"time"
)

func TestOnboardingTrackerBusinessDays(t *testing.T) {
	tracker := NewOnboardingTracker()
	start := time.Date(2024, time.January, 1, 9, 0, 0, 0, time.UTC) // Monday
	end := time.Date(2024, time.January, 5, 18, 0, 0, 0, time.UTC)  // Friday
	if err := tracker.Start("binance-options", start); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	summary, err := tracker.Complete("binance-options", end)
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}
	if summary.BusinessDays != 5 {
		t.Fatalf("expected 5 business days, got %d", summary.BusinessDays)
	}
	if !summary.MeetsSLO {
		t.Fatal("expected onboarding effort to meet SLO")
	}
}

func TestOnboardingTrackerWeekendGaps(t *testing.T) {
	tracker := NewOnboardingTracker()
	start := time.Date(2024, time.January, 5, 10, 0, 0, 0, time.UTC) // Friday
	end := time.Date(2024, time.January, 8, 17, 0, 0, 0, time.UTC)   // Monday
	if err := tracker.Start("new-venue", start); err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	summary, err := tracker.Complete("new-venue", end)
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}
	if summary.BusinessDays != 2 {
		t.Fatalf("expected 2 business days, got %d", summary.BusinessDays)
	}
}

func TestOnboardingTrackerEmitter(t *testing.T) {
	tracker := NewOnboardingTracker()
	tracker.WithClock(func() time.Time { return time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC) })

	var emitted OnboardingSummary
	tracker.SetEmitter(func(summary OnboardingSummary) {
		emitted = summary
	})

	if err := tracker.Start("alpha", time.Time{}); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	_, err := tracker.Complete("alpha", time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected complete error: %v", err)
	}
	if emitted.Project != "alpha" {
		t.Fatalf("expected emitter to receive project 'alpha', got %q", emitted.Project)
	}
}

func TestOnboardingTrackerErrors(t *testing.T) {
	tracker := NewOnboardingTracker()
	if err := tracker.Start("duplicate", time.Time{}); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if err := tracker.Start("duplicate", time.Time{}); err == nil {
		t.Fatal("expected duplicate start error")
	}
	if _, err := tracker.Complete("missing", time.Time{}); err != ErrProjectNotStarted {
		t.Fatalf("expected ErrProjectNotStarted, got %v", err)
	}
}
