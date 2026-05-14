//go:build etcd

package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// NewEtcd creates a production Registry backed by etcd.
func NewEtcd(endpoints []string) (Registry, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to etcd: %w", err)
	}

	return &etcdRegistry{
		client: cli,
	}, nil
}

type etcdRegistry struct {
	client *clientv3.Client
	mu     sync.Mutex
}

func (e *etcdRegistry) Register(ctx context.Context, serviceName, instanceID string, ttl time.Duration) (KeepAliveFn, error) {
	session, err := concurrency.NewSession(e.client, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	key := fmt.Sprintf("/orbitjob/services/%s/%s", serviceName, instanceID)
	if _, err := e.client.Put(ctx, key, instanceID, clientv3.WithLease(session.Lease())); err != nil {
		session.Close()
		return nil, fmt.Errorf("put instance: %w", err)
	}

	return func(ctx context.Context) error {
		// Session auto-renews via etcd keepalive; calling this is a no-op
		// but can be used to verify the session is still alive.
		select {
		case <-session.Done():
			return ErrSessionExpired
		default:
			return nil
		}
	}, nil
}

func (e *etcdRegistry) Deregister(ctx context.Context, serviceName, instanceID string) error {
	key := fmt.Sprintf("/orbitjob/services/%s/%s", serviceName, instanceID)
	if _, err := e.client.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	return nil
}

func (e *etcdRegistry) ListInstances(ctx context.Context, serviceName string) ([]Instance, error) {
	prefix := fmt.Sprintf("/orbitjob/services/%s/", serviceName)
	resp, err := e.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	var out []Instance
	for _, kv := range resp.Kvs {
		id := strings.TrimPrefix(string(kv.Key), prefix)
		out = append(out, Instance{ID: id})
	}
	return out, nil
}

func (e *etcdRegistry) WatchInstances(ctx context.Context, serviceName string) (<-chan Event, error) {
	prefix := fmt.Sprintf("/orbitjob/services/%s/", serviceName)
	ch := make(chan Event, 10)
	watchCh := e.client.Watch(ctx, prefix, clientv3.WithPrefix())

	go func() {
		defer close(ch)
		for wresp := range watchCh {
			for _, ev := range wresp.Events {
				id := strings.TrimPrefix(string(ev.Kv.Key), prefix)
				typ := "put"
				if ev.Type == clientv3.EventTypeDelete {
					typ = "delete"
				}
				select {
				case ch <- Event{Type: typ, Instance: Instance{ID: id}}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

func (e *etcdRegistry) Close() error {
	return e.client.Close()
}
