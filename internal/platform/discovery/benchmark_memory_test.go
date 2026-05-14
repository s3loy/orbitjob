//go:build !etcd

package discovery

func init() {
	benchReg = NewMemory()
	cleanupKeys = nil
}
