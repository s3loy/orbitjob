package discovery

import (
	"context"
	"sync"
	"time"
)

// NewMemory creates an in-memory Registry for unit testing.
func NewMemory() Registry {
	return &memoryRegistry{
		instances: make(map[string]map[string]Instance),
		watches:   make(map[string][]chan Event),
	}
}

type memoryRegistry struct {
	mu        sync.RWMutex
	instances map[string]map[string]Instance // service -> id -> instance
	watches   map[string][]chan Event
}

func (m *memoryRegistry) Register(_ context.Context, serviceName, instanceID string, _ time.Duration) (KeepAliveFn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.instances[serviceName] == nil {
		m.instances[serviceName] = make(map[string]Instance)
	}
	m.instances[serviceName][instanceID] = Instance{ID: instanceID}

	m.notify(serviceName, Event{Type: "put", Instance: Instance{ID: instanceID}})

	return func(context.Context) error { return nil }, nil
}

func (m *memoryRegistry) Deregister(_ context.Context, serviceName, instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if svc, ok := m.instances[serviceName]; ok {
		delete(svc, instanceID)
		m.notify(serviceName, Event{Type: "delete", Instance: Instance{ID: instanceID}})
	}
	return nil
}

func (m *memoryRegistry) ListInstances(_ context.Context, serviceName string) ([]Instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []Instance
	for _, inst := range m.instances[serviceName] {
		out = append(out, inst)
	}
	return out, nil
}

func (m *memoryRegistry) WatchInstances(ctx context.Context, serviceName string) (<-chan Event, error) {
	ch := make(chan Event, 10)
	m.mu.Lock()
	m.watches[serviceName] = append(m.watches[serviceName], ch)
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		m.mu.Lock()
		for i, w := range m.watches[serviceName] {
			if w == ch {
				m.watches[serviceName] = append(m.watches[serviceName][:i], m.watches[serviceName][i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		close(ch)
	}()

	return ch, nil
}

func (m *memoryRegistry) notify(serviceName string, ev Event) {
	for _, ch := range m.watches[serviceName] {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (m *memoryRegistry) Close() error { return nil }
