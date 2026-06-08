package slo

import (
	"testing"
	"time"
)

func TestCalculateWindow_Rolling(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	duration := 30 * 24 * time.Hour

	w := CalculateWindow(WindowTypeRolling, duration, now)

	wantStart := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	wantEnd := now

	if !w.Start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", w.Start, wantStart)
	}
	if !w.End.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", w.End, wantEnd)
	}
}

func TestCalculateWindow_Calendar(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	w := CalculateWindow(WindowTypeCalendar, 0, now)

	wantStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if !w.Start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", w.Start, wantStart)
	}
	if !w.End.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", w.End, wantEnd)
	}
}

func TestCalculateWindow_CalendarYearBoundary(t *testing.T) {
	now := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

	w := CalculateWindow(WindowTypeCalendar, 0, now)

	wantStart := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	if !w.Start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", w.Start, wantStart)
	}
	if !w.End.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", w.End, wantEnd)
	}
}

func TestCalculateWindow_DefaultsToRolling(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	duration := 7 * 24 * time.Hour

	w := CalculateWindow("", duration, now)

	wantStart := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	wantEnd := now

	if !w.Start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", w.Start, wantStart)
	}
	if !w.End.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", w.End, wantEnd)
	}
}

func TestWindow_Duration(t *testing.T) {
	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  time.Duration
	}{
		{
			name:  "one_hour",
			start: time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC),
			end:   time.Date(2026, 6, 4, 11, 0, 0, 0, time.UTC),
			want:  1 * time.Hour,
		},
		{
			name:  "one_day",
			start: time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC),
			want:  24 * time.Hour,
		},
		{
			name:  "zero_duration",
			start: time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC),
			end:   time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC),
			want:  0,
		},
		{
			name:  "negative_duration",
			start: time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC),
			want:  -24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Window{Start: tt.start, End: tt.end}
			got := w.Duration()
			if got != tt.want {
				t.Errorf("Duration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWindow_Elapsed(t *testing.T) {
	start := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	w := Window{Start: start, End: end}

	tests := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{
			name: "before_start",
			now:  time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
			want: 0,
		},
		{
			name: "at_start",
			now:  start,
			want: 0,
		},
		{
			name: "halfway",
			now:  time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC),
			want: 12 * time.Hour,
		},
		{
			name: "at_end",
			now:  end,
			want: 24 * time.Hour,
		},
		{
			name: "after_end",
			now:  time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC),
			want: 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.Elapsed(tt.now)
			if got != tt.want {
				t.Errorf("Elapsed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWindow_Remaining(t *testing.T) {
	start := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	w := Window{Start: start, End: end}

	tests := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{
			name: "before_start",
			now:  time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
			want: 24 * time.Hour,
		},
		{
			name: "at_start",
			now:  start,
			want: 24 * time.Hour,
		},
		{
			name: "halfway",
			now:  time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC),
			want: 12 * time.Hour,
		},
		{
			name: "at_end",
			now:  end,
			want: 0,
		},
		{
			name: "after_end",
			now:  time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC),
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.Remaining(tt.now)
			if got != tt.want {
				t.Errorf("Remaining() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWindow_ElapsedAndRemainingSumToDuration(t *testing.T) {
	start := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	w := Window{Start: start, End: end}

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	elapsed := w.Elapsed(now)
	remaining := w.Remaining(now)
	duration := w.Duration()

	if elapsed+remaining != duration {
		t.Errorf("elapsed(%v) + remaining(%v) != duration(%v)", elapsed, remaining, duration)
	}
}

func TestCalculateWindow_Quarterly(t *testing.T) {
	tests := []struct {
		name      string
		now       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "q1_january",
			now:       time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "q2_april",
			now:       time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "q3_july",
			now:       time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "q4_october",
			now:       time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "q4_december_year_boundary",
			now:       time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			wantStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := CalculateWindow(WindowTypeQuarterly, 0, tt.now)
			if !w.Start.Equal(tt.wantStart) {
				t.Errorf("start = %v, want %v", w.Start, tt.wantStart)
			}
			if !w.End.Equal(tt.wantEnd) {
				t.Errorf("end = %v, want %v", w.End, tt.wantEnd)
			}
		})
	}
}
