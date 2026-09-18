# Database configuration

OrbitJob configures its database once per installation. Every process and every
replica inherits that configuration; nobody configures a DSN per process.

## What the bootstrap account needs

One account is used once, to create the roles the workloads run as. It needs:

- `LOGIN` and `CREATEROLE`
- ownership of the target database

That is all. It does not need `SUPERUSER`: `pgcrypto` is a trusted extension in
PostgreSQL 13 and later, so the database owner can create it, and the schema
grants in `owner-init` work from database ownership.

If the roles already exist and were created by a different account, the
bootstrap account also needs `ADMIN OPTION` on them — `CREATEROLE` alone only
covers roles the account created. `scripts/kind-db.sh` grants this when
switching an existing local installation over.

`owner-init` creates and manages:

```text
orbitjob_table_owner   orbitjob_migrator   orbitjob_admin   orbitjob_runtime
orbitjob_operator      orbitjob_owner      orbitjob_reader
```

Only `orbitjob_migrator`, `orbitjob_admin` and `orbitjob_runtime` have `LOGIN`;
`orbitjob_table_owner`, `orbitjob_operator`, `orbitjob_owner` and
`orbitjob_reader` are `NOLOGIN` identities. Business processes never receive
the bootstrap DSN. They get the migrator, admin and runtime roles, each subject
to row level security.

## Bundled PostgreSQL

`cmd/configure setup` has two modes. In bundled mode it assumes a PostgreSQL
server is reachable at a fixed endpoint — the default is
`postgres://pg:5432/orbitjob`, override with `--bundled-endpoint` — and
generates the installation state without contacting the server:

```bash
go run ./cmd/configure setup --mode bundled
```

`configure setup` writes:

- `.runtime/database.env` — the derived DSNs for the bootstrap owner, migrator,
  admin and runtime roles, plus the generated passwords
- `.runtime/database.json` — installation state, so a repeated setup reuses the
  passwords

Both paths are gitignored and both files are `0600`.

Running `configure setup` again does not change the passwords:

```text
Existing database configuration is valid. Runtime configuration refreshed.
```

`--reconfigure` replaces existing installation state — that is what you want if
the owner password and the installation state have diverged.

## External PostgreSQL

Put the bootstrap DSN in a file only you can read:

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full' \
  > /tmp/orbitjob-bootstrap-dsn
```

Then:

```bash
go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

For a local kind cluster, `make kind-db` does all of this and creates the
Secret, using an `orbitjob_bootstrap` account that owns the database rather than
the image's superuser.

## Kubernetes and replicas

One installation uses one `orbitjob-database` Secret, and every pod reads the
same one. Going from one scheduler replica to ten does not touch the database
configuration:

```bash
kubectl scale deployment/orbitjob-scheduler -n orbitjob-system --replicas=10
kubectl rollout status deployment/orbitjob-scheduler -n orbitjob-system
```

The chart takes a Secret name and a fixed set of key names. It does not ask for
a password and a DSN for the same role.

Changing the Secret does not restart the pods that read it, and the chart cannot
hash a Secret it does not create. `make kind-db` rolls the workloads when the
contents actually change; a hand-edited Secret needs a restart of your own.

## Standalone processes

Without Helm, the generated `.runtime/database.env` is the single source:

```bash
set -a
. /opt/orbitjob/etc/database.env
set +a
./bin/admin-api
./bin/scheduler
./bin/operator
```

`admin-api` reads `ADMIN_DSN`. The runtime processes — scheduler and operator —
share `RUNTIME_DSN`; the operator accepts `OPERATOR_DSN` first and falls back
to `RUNTIME_DSN`.

> **Renamed variables.** `DATABASE_DSN` and `SCHEDULER_DSN` are accepted only
> as legacy fallbacks for `ADMIN_DSN` and `RUNTIME_DSN`; new deployments must
> not use them. The old `DISPATCHER_DSN` and `WORKER_DSN` were removed together
> with the dispatcher and worker processes.

## TLS

An external DSN keeps its PostgreSQL query parameters:

```text
postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full&sslrootcert=/run/secrets/ca.crt
```

The tooling derives the internal DSNs with `net/url`, so a password containing
`@:/?#%` or a space needs no manual encoding.

## Troubleshooting

### Local configuration is missing

Standalone processes read `.runtime/database.env`. Generate it once with
`go run ./cmd/configure setup`; do not hand-write it.

### External authentication fails

Check the user, password, TLS parameters and database in the bootstrap DSN. The
tooling prints only a redacted endpoint and never the password.

### Permission denied

```text
ensure database roles: pq: permission denied to alter role (42501)
```

The bootstrap account cannot manage the OrbitJob roles. It needs `CREATEROLE`,
and `ADMIN OPTION` on roles it did not create itself. PostgreSQL lets only a
superuser change the `SUPERUSER`, `CREATEROLE` or `BYPASSRLS` attributes, so a
role that has drifted needs a superuser to repair it — `owner-init` names the
role and the attribute rather than doing it silently.

### Switching databases

A normal setup never overwrites existing installation state. Pointing at a
different endpoint changes the authoritative data source and is not a routine
configuration change: back up and migrate separately.
