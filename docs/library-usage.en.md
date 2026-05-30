# OrbitJob Library Usage Guide

All public APIs are exposed via `pkg/`. You can assemble the scheduler, dispatcher, and executor in your own Go application without launching standalone processes.

## Full Example

```go
package main

import (
    "context"
    "database/sql"
    "time"

    _ "github.com/lib/pq"

    "orbitjob/pkg/dispatch"
    "orbitjob/pkg/execute"
    "orbitjob/pkg/execute/handler"
    "orbitjob/pkg/schedule"
    "orbitjob/pkg/store/postgres"
)

func main() {
    ctx := context.Background()
    db, _ := sql.Open("postgres", "postgres://...")

    // Scheduler: scans cron jobs and creates pending instances
    schedRepo := postgres.NewSchedulerRepository(db)
    scheduler := schedule.NewTickUseCase(schedRepo, postgres.ClassifyError)

    // Dispatcher: assigns pending instances to workers
    dispatchRepo := postgres.NewDispatchRepository(db)
    dispatcher := dispatch.NewTickUseCase(dispatchRepo)

    // Worker: claims tasks and runs handlers
    handler.Register("my_handler", myCustomHandler{})
    execRepo := postgres.NewExecutorRepository(db)
    handlers := map[string]execute.Handler{
        "exec":      &handler.Exec{},
        "http":      handler.NewHTTP(nil),
        "webhook":   handler.NewWebhook(nil),
        "pg_notify": handler.NewPGNotify(db),
        "my_handler": myCustomHandler{},
    }
    worker := execute.NewTickUseCase(execRepo, handlers)

    // Run ticks...
    counts, _ := scheduler.RunBatch(ctx, time.Now(), 100)
}

type myCustomHandler struct{}

func (h myCustomHandler) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
    return execute.Result{Success: true}
}
```

## Symbol Reference

| Package | Purpose |
|---------|---------|
| `pkg/schedule` | Scheduler use case (cron scanning, instance creation) |
| `pkg/dispatch` | Dispatcher use case (assigning instances to workers) |
| `pkg/execute` | Executor use case (worker claim and execute) |
| `pkg/execute/handler` | Built-in handlers (Exec, HTTP, Webhook, PGNotify) |
| `pkg/store/postgres` | PostgreSQL store implementations |
| `pkg/domain/*` | Domain types and constants (job, instance, worker, validation, resource) |
| `pkg/admin/*` | Control plane API (HTTP handlers, query/command use cases) |
| `pkg/platform/*` | Platform utilities (config, logger, health) |
