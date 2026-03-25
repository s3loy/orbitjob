# OrbitJob

OrbitJob is a job scheduling system written in Go.

This repository is in the middle of a context-first refactor.

## Status

- `admin` owns the control-plane HTTP API and read models.
- `core` owns job domain rules and write-side persistence.
- `worker` and runtime processes are still being carved out.

## Development

Requirements:

- Go
- PostgreSQL

Run tests:

```bash
go test ./...
```

Run integration build checks:

```bash
go test -tags integration ./internal/admin/store/postgres ./internal/core/store/postgres
```

Run the admin API:

```bash
go run ./cmd/admin-api
```

Environment variables:

- `DATABASE_DSN`
- `APP_ENV`
- `TEST_DATABASE_DSN` for integration tests

## Repository Layout

```text
cmd/admin-api
internal/admin
internal/core
internal/domain
internal/platform
db/migrations
```

## License

See `LICENSE`.
