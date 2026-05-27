// Package postgres provides PostgreSQL-backed store implementations
// for the core write side.
package postgres

import internal "orbitjob/internal/core/store/postgres"

// SchedulerRepository owns scheduler-side persistence operations.
type SchedulerRepository = internal.SchedulerRepository

// DispatchRepository owns dispatcher-side persistence operations.
type DispatchRepository = internal.DispatchRepository

// ExecutorRepository owns executor-side persistence operations.
type ExecutorRepository = internal.ExecutorRepository

// WorkerRepository owns worker persistence operations.
type WorkerRepository = internal.WorkerRepository

// JobRepository owns job write-side persistence operations.
type JobRepository = internal.JobRepository

// InstanceRepository owns instance write-side persistence operations.
type InstanceRepository = internal.InstanceRepository

// EventListener wraps PostgreSQL LISTEN for event-driven wake.
type EventListener = internal.EventListener

var (
	NewSchedulerRepository = internal.NewSchedulerRepository
	NewDispatchRepository  = internal.NewDispatchRepository
	NewExecutorRepository  = internal.NewExecutorRepository
	NewWorkerRepository    = internal.NewWorkerRepository
	NewJobRepository       = internal.NewJobRepository
	NewInstanceRepository  = internal.NewInstanceRepository
	NewEventListener       = internal.NewEventListener
	ClassifyError          = internal.ClassifyError
)
