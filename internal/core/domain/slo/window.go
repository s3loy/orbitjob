package slo

import "time"

// Window represents a time window for SLO evaluation.
type Window struct {
	Start time.Time
	End   time.Time
}

// CalculateWindow computes the evaluation window for an SLO.
func CalculateWindow(windowType string, duration time.Duration, now time.Time) Window {
	switch windowType {
	case WindowTypeCalendar:
		return calculateCalendarWindow(now)
	case WindowTypeQuarterly:
		return calculateQuarterlyWindow(now)
	default:
		return calculateRollingWindow(duration, now)
	}
}

func calculateRollingWindow(duration time.Duration, now time.Time) Window {
	start := now.Add(-duration)
	return Window{Start: start, End: now}
}

func calculateCalendarWindow(now time.Time) Window {
	// Calendar month window: [1st of current month 00:00:00, 1st of next month 00:00:00)
	year, month, _ := now.Date()
	start := time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 1, 0)
	return Window{Start: start, End: end}
}

func calculateQuarterlyWindow(now time.Time) Window {
	// Quarterly window: [1st of current quarter 00:00:00, 1st of next quarter 00:00:00)
	year, month, _ := now.Date()
	quarterStartMonth := time.Month((int(month)-1)/3*3 + 1)
	start := time.Date(year, quarterStartMonth, 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 3, 0)
	return Window{Start: start, End: end}
}

// WindowDuration returns the actual duration of the window.
func (w Window) Duration() time.Duration {
	return w.End.Sub(w.Start)
}

// Elapsed returns the elapsed time since the window start up to the given time.
func (w Window) Elapsed(now time.Time) time.Duration {
	if now.Before(w.Start) {
		return 0
	}
	if now.After(w.End) {
		return w.Duration()
	}
	return now.Sub(w.Start)
}

// Remaining returns the remaining time in the window from the given time.
func (w Window) Remaining(now time.Time) time.Duration {
	if now.After(w.End) {
		return 0
	}
	if now.Before(w.Start) {
		return w.Duration()
	}
	return w.End.Sub(now)
}
