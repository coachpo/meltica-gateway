package telemetry

import (
	"errors"
	"sync"
	"time"
)

var (
	// ErrProjectAlreadyStarted is returned when attempting to start an already tracked project.
	ErrProjectAlreadyStarted = errors.New("telemetry: project already started")
	// ErrProjectNotStarted is returned when attempting to complete a project without a start timestamp.
	ErrProjectNotStarted = errors.New("telemetry: project not started")
)

// OnboardingSummary captures the lifecycle metrics for a single adapter onboarding effort.
type OnboardingSummary struct {
	Project      string
	StartedAt    time.Time
	CompletedAt  time.Time
	BusinessDays int
	MeetsSLO     bool
}

// OnboardingTracker records adapter onboarding durations to validate SC-001.
type OnboardingTracker struct {
	mu       sync.Mutex
	projects map[string]time.Time
	clock    func() time.Time
	emitter  func(OnboardingSummary)
	target   int
}

// NewOnboardingTracker constructs an onboarding tracker with a five-business-day SLO target.
func NewOnboardingTracker() *OnboardingTracker {
	return &OnboardingTracker{
		mu:       sync.Mutex{},
		projects: make(map[string]time.Time),
		clock:    time.Now,
		emitter:  nil,
		target:   5,
	}
}

// WithClock overrides the internal clock to ease deterministic testing.
func (t *OnboardingTracker) WithClock(clock func() time.Time) *OnboardingTracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	if clock == nil {
		t.clock = time.Now
	} else {
		t.clock = clock
	}
	return t
}

// SetEmitter registers a callback invoked after successful project completion.
func (t *OnboardingTracker) SetEmitter(emitter func(OnboardingSummary)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emitter = emitter
}

// Start records the beginning of an onboarding effort.
func (t *OnboardingTracker) Start(project string, started time.Time) error {
	if project == "" {
		return errors.New("telemetry: project name required")
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.projects[project]; exists {
		return ErrProjectAlreadyStarted
	}
	if started.IsZero() {
		started = t.clock()
	}
	t.projects[project] = started
	return nil
}

// Complete finalizes an onboarding effort, returning a summary and invoking the emitter.
func (t *OnboardingTracker) Complete(project string, completed time.Time) (OnboardingSummary, error) {
	t.mu.Lock()
	started, ok := t.projects[project]
	if !ok {
		t.mu.Unlock()
		return OnboardingSummary{}, ErrProjectNotStarted
	}
	if completed.IsZero() {
		completed = t.clock()
	}
	delete(t.projects, project)
	summary := OnboardingSummary{
		Project:      project,
		StartedAt:    started,
		CompletedAt:  completed,
		BusinessDays: businessDaysBetween(started, completed),
		MeetsSLO:     false,
	}
	if summary.BusinessDays <= t.target && summary.BusinessDays > 0 {
		summary.MeetsSLO = true
	}
	emitter := t.emitter
	t.mu.Unlock()

	if emitter != nil {
		emitter(summary)
	}
	return summary, nil
}

func businessDaysBetween(start, end time.Time) int {
	if end.Before(start) {
		return 0
	}
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())

	days := 0
	for current := startDate; !current.After(endDate); current = current.AddDate(0, 0, 1) {
		switch current.Weekday() {
		case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
			days++
		case time.Saturday, time.Sunday:
			continue
		}
	}
	return days
}
