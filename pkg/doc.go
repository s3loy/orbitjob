// Package pkg provides the public API surface of OrbitJob.
//
// OrbitJob can be embedded as a library in Go applications. Import the
// sub-packages under pkg/ to access domain types, use cases, stores,
// handlers, and platform utilities.
//
// All symbols in pkg/ are thin re-exports of the internal implementation.
// They use Go type aliases and function forwarding, so there is no runtime
// overhead and type identity is preserved.
//
// Quick start:
//
//	import "orbitjob/pkg/schedule"
//	import "orbitjob/pkg/store/postgres"
//
//	repo := postgres.NewSchedulerRepository(db)
//	scheduler := schedule.NewTickUseCase(repo, postgres.ClassifyError)
package pkg
