//go:build etcd

package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"orbitjob/internal/platform/metrics"
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
}

func (e *etcdRegistry) Register(ctx context.Context, serviceName, instanceID string, ttl time.Duration) (KeepAliveFn, error) {
	start := time.Now()

	session, err := concurrency.NewSession(e.client, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		metrics.EtcdOperationErrorsTotal.WithLabelValues("register_session").Inc()
		return nil, fmt.Errorf("create session: %w", err)
	}

	key := fmt.Sprintf("/orbitjob/services/%s/%s", serviceName, instanceID)
	if _, err := e.client.Put(ctx, key, instanceID, clientv3.WithLease(session.Lease())); err != nil {
		session.Close()
		metrics.EtcdOperationErrorsTotal.WithLabelValues("register_put").Inc()
		return nil, fmt.Errorf("put instance: %w", err)
	}

	metrics.EtcdRegisterDuration.Observe(time.Since(start).Seconds())

	return func(ctx context.Context) error {
		// Session auto-renews via etcd keepalive; verify it is still alive.
		select {
		case <-session.Done():
			metrics.EtcdSessionExpiresTotal.WithLabelValues("discovery").Inc()
			return ErrSessionExpired
		default:
			return nil
		}
	}, nil
}

func (e *etcdRegistry) Deregister(ctx context.Context, serviceName, instanceID string) error {
	start := time.Now()

	key := fmt.Sprintf("/orbitjob/services/%s/%s", serviceName, instanceID)
	if _, err := e.client.Delete(ctx, key); err != nil {
		metrics.EtcdOperationErrorsTotal.WithLabelValues("deregister").Inc()
		return fmt.Errorf("delete instance: %w", err)
	}

	metrics.EtcdDeregisterDuration.Observe(time.Since(start).Seconds())
	return nil
}

func (e *etcdRegistry) ListInstances(ctx context.Context, serviceName string) ([]Instance, error) {
	start := time.Now()

	prefix := fmt.Sprintf("/orbitjob/services/%s/", serviceName)
	resp, err := e.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		metrics.EtcdOperationErrorsTotal.WithLabelValues("list_instances").Inc()
		return nil, fmt.Errorf("list instances: %w", err)
	}

	metrics.EtcdListInstancesDuration.Observe(time.Since(start).Seconds())

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

	go func() {
		defer close(ch)
		for {
			watchCh := e.client.Watch(ctx, prefix, clientv3.WithPrefix())
			for wresp := range watchCh {
				if wresp.Err() != nil {
					metrics.EtcdOperationErrorsTotal.WithLabelValues("watch").Inc()
					continue
				}
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
					default:
						// Channel full, caller not reading — drop event to prevent goroutine leak.
					}
				}
			}
			// Watch channel closed (e.g. etcd reconnection). Restart unless ctx is done.
			select {
			case <-ctx.Done():
				return
			default:
				// Brief back-off before restarting watch.
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	return ch, nil
}

func (e *etcdRegistry) Close() error {
	return e.client.Close()
}
