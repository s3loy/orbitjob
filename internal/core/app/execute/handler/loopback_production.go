//go:build !integration

package handler

// allowLoopback is always false in production builds.
var allowLoopback bool
