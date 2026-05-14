//go:build etcd

package config

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"orbitjob/internal/platform/metrics"
)

// NewEtcdWatcher creates a Watcher backed by etcd.
func NewEtcdWatcher(endpoints []string) (Watcher, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to etcd: %w", err)
	}
	return &etcdWatcher{client: cli}, nil
}

type etcdWatcher struct {
	client *clientv3.Client
}

func (e *etcdWatcher) Watch(ctx context.Context, key string, callback func(value string)) error {
	// Fetch initial value.
	resp, err := e.client.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("get initial value: %w", err)
	}
	if len(resp.Kvs) > 0 {
		callback(string(resp.Kvs[0].Value))
	}

	// Watch for changes with auto-restart on channel closure.
	for {
		watchCh := e.client.Watch(ctx, key)
		for wresp := range watchCh {
			if wresp.Err() != nil {
				metrics.EtcdOperationErrorsTotal.WithLabelValues("config_watch").Inc()
				continue
			}
			for _, ev := range wresp.Events {
				if ev.Type == clientv3.EventTypeDelete {
					callback("")
				} else {
					callback(string(ev.Kv.Value))
				}
			}
		}

		// Watch channel closed — restart unless ctx is done.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Brief back-off before restarting watch.
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (e *etcdWatcher) Close() error {
	return e.client.Close()
}
