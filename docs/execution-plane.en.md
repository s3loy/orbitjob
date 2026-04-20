# Execution Plane Contract

[Back to README](../README.en.md)

This document defines the data model, state semantics, and behavioral contracts for the OrbitJob execution plane, providing a deterministic specification for collaboration between the scheduler, dispatcher, and worker components.

## Current Implementation Status (2026-04-20)

**Implemented:**

- Execution routing fields on job definitions (`priority`, `partition_key`, `handler_type`, `handler_payload`) wired end-to-end
- `job_instances` create and claim semantics with domain model, repository, and tests
- `workers` heartbeat and lease upsert with domain model, repository, and tests
- Scheduler MVP tick loop (`cmd/scheduler` + `core/app/schedule` + `SchedulerRepository`)
- Deterministic misfire policy evaluator (skip / fire_now / catch_up)
- Atomic scheduling transaction (claim + insert instance + update cursor in a single tx)
- Dispatcher runtime (`cmd/dispatcher` + `core/app/dispatch`):
  - Atomic claim using `FOR UPDATE SKIP LOCKED` to prevent duplicate dispatch
  - Concurrency policy as a pure decision function: `DecideDispatch(input) -> dispatch / skip / replace`
  - Priority aging: pending instances gain +1 effective priority per minute of wait time, capped at base priority + 60
  - Lease expiry recovery: orphaned dispatching instances are reclaimed to pending at the start of each tick
  - Graceful shutdown on SIGINT / SIGTERM
  - Environment-variable configuration (`DISPATCHER_WORKER_ID` / `DISPATCHER_TENANT_ID` / `DISPATCHER_BATCH_SIZE` / `DISPATCHER_TICK_INTERVAL_SEC` / `DISPATCHER_LEASE_DURATION_SEC`)

**Not yet implemented:**

- Worker executor runtime (receive task, execute, write back result)
- Manual trigger API
- Instance query API
- Full `job_instance_attempts` write chain

## Component Boundaries

```mermaid
flowchart LR
    Control["Control Plane<br/>job definitions"] --> Scheduler["Scheduler"]
    Scheduler --> InstanceRepo["job_instances"]
    Manual["Manual Trigger API<br/>(planned)"] -.-> InstanceRepo
    Dispatcher["Dispatcher"] --> InstanceRepo
    Dispatcher --> WorkerRepo["workers"]
    Worker["Worker<br/>(planned)"] -.-> WorkerRepo
    Worker -.-> InstanceRepo
```

## Job Definition Routing Fields

| Field | Purpose |
| --- | --- |
| `priority` | Base priority used by the dispatcher to order runnable instances; higher values take precedence |
| `partition_key` | Logical shard key for worker routing, queue partitioning, or tenant isolation |
| `handler_type` | Executor type identifier (e.g., `http` or `worker`) |
| `handler_payload` | Handler-specific configuration interpreted and executed by the worker |

## Job Instance State Machine

```mermaid
stateDiagram-v2
    [*] --> pending
    pending --> dispatching: dispatcher claim
    retry_wait --> dispatching: retry eligible + dispatcher claim
    dispatching --> running: worker start
    dispatching --> pending: lease expired (recovery)
    running --> success: worker reports success
    running --> retry_wait: worker reports failure, retries remaining
    running --> failed: worker reports failure, no retries remaining
    running --> canceled: replace policy / control action
```

## State Semantics

| State | Meaning |
| --- | --- |
| `pending` | Created and waiting for dispatcher claim |
| `dispatching` | Claimed by dispatcher with `worker_id` and `lease_expires_at` assigned; awaiting worker pickup |
| `running` | Worker has started execution |
| `retry_wait` | Previous attempt finished with retries remaining; waits until `retry_at` to re-enter the dispatch queue |
| `success` | Terminal -- execution succeeded |
| `failed` | Terminal -- execution failed with no retries remaining |
| `canceled` | Terminal -- canceled by replace policy, control action, or recovery action |

## Dispatcher Claim Flow

Each dispatcher tick executes a bounded batch in two phases:

### Phase 1: Lease Expiry Recovery

Before normal dispatch begins, the dispatcher reclaims all instances where `status = 'dispatching'` and `lease_expires_at < now()`, resetting them to `pending` and clearing `worker_id` and `lease_expires_at`. This ensures that tasks are not lost when a dispatcher crashes after claiming.

### Phase 2: Per-Instance Dispatch

For each candidate instance, a single database transaction performs the following steps:

1. **Lock candidate**: Select one instance from `job_instances` ordered by priority, using `FOR UPDATE SKIP LOCKED` to prevent concurrent claims
2. **Lock job row**: Acquire a `FOR UPDATE` lock on the corresponding `jobs` row and read `concurrency_policy`
3. **Count running**: Query the number of `dispatching` + `running` instances for that job
4. **Policy decision**: Call the pure function `DecideDispatch(input)` to obtain a decision
5. **Execute decision**: Carry out the dispatch / skip / replace action

### Candidate Ordering and Priority Aging

Candidate instances are selected in the following order:

```
effective_priority DESC, scheduled_at ASC, id ASC
```

Effective priority is calculated as:

```
effective_priority = min(base_priority + floor(minutes_since_scheduled), base_priority + 60)
```

A pending instance gains +1 effective priority for each minute it has been waiting, capped at base priority + 60. This mechanism prevents low-priority tasks from starving indefinitely.

### Candidate States

| Candidate Condition | Rule |
| --- | --- |
| `pending` | Directly eligible |
| `retry_wait` | Must satisfy `retry_at <= now()` and `attempt < max_attempt` |

### Claim Writes

| Operation | Description |
| --- | --- |
| Normal claim | Sets `status = 'dispatching'`, writes `worker_id` and `lease_expires_at` |
| Claim from `retry_wait` | Additionally increments `attempt` and clears `retry_at`, `started_at`, `finished_at`, `result_code`, `error_msg` |

## Concurrency Policy Decision

After claiming a candidate instance but before writing the dispatching status, the dispatcher evaluates the job's `concurrency_policy` field:

| Policy | Condition | Decision |
| --- | --- | --- |
| `allow` | Any | dispatch -- permits multiple instances to run concurrently |
| `forbid` | `running_count = 0` | dispatch |
| `forbid` | `running_count > 0` | skip -- candidate remains pending for the next tick |
| `replace` | `running_count = 0` | dispatch |
| `replace` | `running_count > 0` | replace -- cancel existing dispatching/running instances, then dispatch the new one |
| Unknown | Any | falls back to allow behavior |

The decision logic is implemented as the pure function `DecideDispatch`, free of side effects and straightforward to test in isolation.

## Worker Heartbeat / Lease Rules

Workers use a single upsert operation for both initial registration and heartbeat refresh:

| Field | Rule |
| --- | --- |
| `worker_id` | Stable worker identifier, unique within a tenant |
| `status` | `online`, `offline`, or `draining` |
| `capacity` | Concurrent processing capacity; must be `>= 1` |
| `labels` | JSON object for routing and scheduling filters |
| `lease_expires_at` | Explicit lease deadline supplied by the worker during heartbeat |

Constraints:

- Heartbeat refreshes `last_heartbeat_at`
- `(tenant_id, worker_id)` is handled with upsert semantics
- A `draining` worker may continue to heartbeat; whether the dispatcher assigns new work is governed by scheduling policy

## Retry Boundaries

- The `job_instance_attempts` table is reserved for an immutable per-attempt audit trail; the full write chain is not yet implemented
- When re-claiming from `retry_wait`, the attempt counter is atomically incremented at the SQL layer

## Code Locations

| Path | Purpose |
| --- | --- |
| `cmd/dispatcher/main.go` | Dispatcher process entry point, configuration loading, and tick loop |
| `internal/core/app/dispatch/tick.go` | Dispatcher tick use case: lease recovery + bounded batch |
| `internal/core/domain/instance/dispatch.go` | `DecideDispatch` pure function and concurrency policy decision |
| `internal/core/store/postgres/dispatch_repository.go` | Dispatch transaction: candidate selection, policy lookup, decision execution, lease recovery |
| `internal/core/domain/instance/claim.go` | `ClaimSpec` and claim input validation |
| `cmd/scheduler/main.go` | Scheduler process entry point |
| `internal/core/app/schedule/` | Scheduler tick use case and misfire policies |

## Follow-up Work

- Worker executor runtime: receive dispatching instances, execute, and write back results
- Manual trigger API: on-demand creation of job instances
- Instance query API: instance listing and detail endpoints
- Full `job_instance_attempts` attempt trail writes
- Production observability convergence and alerting
