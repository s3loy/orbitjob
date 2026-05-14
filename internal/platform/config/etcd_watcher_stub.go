//go:build !etcd

package config

import "errors"

// NewEtcdWatcher returns an error when etcd support is not compiled in.
func NewEtcdWatcher(_ []string) (Watcher, error) {
	return nil, errors.New("etcd support not compiled: build with -tags etcd")
}
