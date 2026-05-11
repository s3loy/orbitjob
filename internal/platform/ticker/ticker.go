package ticker

import "time"

// WallClock wraps a time.Ticker to provide a common interface for
// scheduler, dispatcher, and worker main loops.
type WallClock struct {
	t *time.Ticker
}

// New creates a WallClock that ticks at the given interval.
func New(d time.Duration) *WallClock {
	return &WallClock{t: time.NewTicker(d)}
}

// Chan returns the ticker channel.
func (w *WallClock) Chan() <-chan time.Time { return w.t.C }

// Stop stops the ticker.
func (w *WallClock) Stop() { w.t.Stop() }
