// Package config provides platform configuration utilities.
package config

import internal "orbitjob/internal/platform/config"

type (
	Watcher    = internal.Watcher
	NopWatcher = internal.NopWatcher
)

var (
	LoadDotenv         = internal.LoadDotenv
	LoadPositiveIntEnv = internal.LoadPositiveIntEnv
)
