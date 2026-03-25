# OrbitJob

OrbitJob is a job scheduling system written in Go.

This repository currently contains the admin API and the first phase of a context-first refactor. The main path is now:

`cmd/admin-api -> internal/admin/http -> internal/admin/app -> internal/admin/store/postgres`

## Status

OrbitJob is still under active refactoring.

The admin boundary is mostly in place. The `core` context, worker-side processes, and the remaining compatibility cleanup are still in progress.

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
go test -tags integration ./internal/admin/store/postgres
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
internal/domain
internal/platform
db/migrations
```

## License

See `LICENSE`.
