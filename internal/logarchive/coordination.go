package logarchive

import "context"

// Coordinators pin an archive's segment names while a read is in progress.
// Writers take the same archive coordinator before the global metadata lock.
// Entries exist only while held or queued, so expired archives leave no locks.
type coordinator struct {
	queue chan struct{}
	refs  int
}

func (s *Store) coordinate(ctx context.Context, key string, capacity int) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.coordinators == nil {
		s.coordinators = make(map[string]*coordinator)
	}
	c := s.coordinators[key]
	if c == nil {
		c = &coordinator{queue: make(chan struct{}, capacity)}
		s.coordinators[key] = c
	}
	c.refs++
	s.mu.Unlock()
	releaseRef := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		c.refs--
		if c.refs == 0 {
			delete(s.coordinators, key)
		}
	}
	select {
	case c.queue <- struct{}{}:
		return func() { <-c.queue; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}
