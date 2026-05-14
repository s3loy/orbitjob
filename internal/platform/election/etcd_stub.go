//go:build !etcd

package election

import "errors"

// NewEtcd returns an error when etcd support is not compiled in.
// Build with -tags etcd to enable the real etcd-backed implementation.
func NewEtcd(_ EtcdConfig) (Coordinator, error) {
	return nil, errors.New("etcd support not compiled: build with -tags etcd")
}
