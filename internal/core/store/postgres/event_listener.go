package postgres

import (
	"log/slog"
	"time"

	"github.com/lib/pq"
)

// EventListener wraps pq.Listener to provide a channel-based notification
// interface for dispatcher event-driven wake. Notifications are de-duplicated
// via a buffered channel of size 1.
type EventListener struct {
	listener *pq.Listener
	ch       chan struct{}
}

// NewEventListener creates a new listener on the given DSN that listens to
// the 'job_events' channel. The listener reconnects automatically on disconnect.
func NewEventListener(dsn string) (*EventListener, error) {
	report := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			slog.Error("pg listener event", "type", ev, "error", err)
		}
	}
	l := pq.NewListener(dsn, 2*time.Second, 10*time.Second, report)
	if err := l.Listen("job_events"); err != nil {
		return nil, err
	}

	el := &EventListener{
		listener: l,
		ch:       make(chan struct{}, 1),
	}
	go el.loop()
	return el, nil
}

func (el *EventListener) loop() {
	for n := range el.listener.NotificationChannel() {
		if n == nil {
			continue // reconnection event
		}
		select {
		case el.ch <- struct{}{}:
		default:
		}
	}
}

// C returns the notification channel. Receiving from this channel signals
// that at least one job event has occurred since the last receive.
func (el *EventListener) C() <-chan struct{} { return el.ch }

// Close shuts down the listener and its background goroutine.
func (el *EventListener) Close() error {
	if el == nil {
		return nil
	}
	return el.listener.Close()
}
