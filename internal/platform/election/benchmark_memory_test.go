//go:build !etcd

package election

func init() {
	benchCoord = NewMemory()
	// Memory implementation needs no external cleanup.
	cleanupKeys = nil
}
