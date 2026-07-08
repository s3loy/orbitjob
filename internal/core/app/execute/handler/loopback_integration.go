//go:build integration

package handler

// allowLoopback can be toggled for integration tests only.
var allowLoopback bool

// SetAllowLoopbackForTest enables loopback URLs only in integration-test builds.
// It must never be called from production code.
func SetAllowLoopbackForTest(v bool) {
	allowLoopback = v
}
