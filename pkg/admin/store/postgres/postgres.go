// Package postgres provides PostgreSQL-backed read stores for the admin API.
package postgres

import internal "orbitjob/internal/admin/store/postgres"

// JobRepository is the admin read store for jobs.
type JobRepository = internal.JobRepository

// InstanceRepository is the admin read store for instances.
type InstanceRepository = internal.InstanceRepository

var (
	NewJobRepository      = internal.NewJobRepository
	NewInstanceRepository = internal.NewInstanceRepository
	Open                  = internal.Open
)
