# syntax=docker/dockerfile:1

# ============================================================
# Builder base: shared module cache
# ============================================================
FROM golang:1.26-alpine AS base
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# ============================================================
# Build stages
# ============================================================
FROM base AS build-admin
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/admin-api ./cmd/admin-api/

FROM base AS build-scheduler
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler/

FROM base AS build-dispatcher
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/dispatcher ./cmd/dispatcher/

FROM base AS build-worker
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker/

FROM base AS build-healthcheck
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck/

FROM base AS build-bootstrap
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/bootstrap ./cmd/bootstrap/

# ============================================================
# Final images (FROM scratch)
# ============================================================
FROM scratch AS admin
COPY --from=build-admin /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-admin /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-admin /etc/passwd /etc/passwd
COPY --from=build-healthcheck /out/healthcheck /healthcheck
COPY --from=build-admin /out/admin-api /admin-api
USER 65534:65534
ENTRYPOINT ["/admin-api"]

FROM scratch AS scheduler
COPY --from=build-scheduler /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-scheduler /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-scheduler /etc/passwd /etc/passwd
COPY --from=build-healthcheck /out/healthcheck /healthcheck
COPY --from=build-scheduler /out/scheduler /scheduler
USER 65534:65534
ENTRYPOINT ["/scheduler"]

FROM scratch AS dispatcher
COPY --from=build-dispatcher /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-dispatcher /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-dispatcher /etc/passwd /etc/passwd
COPY --from=build-healthcheck /out/healthcheck /healthcheck
COPY --from=build-dispatcher /out/dispatcher /dispatcher
USER 65534:65534
ENTRYPOINT ["/dispatcher"]

FROM scratch AS worker
COPY --from=build-worker /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-worker /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-worker /etc/passwd /etc/passwd
COPY --from=build-healthcheck /out/healthcheck /healthcheck
COPY --from=build-worker /out/worker /worker
USER 65534:65534
ENTRYPOINT ["/worker"]

FROM scratch AS bootstrap
COPY --from=build-bootstrap /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-bootstrap /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-bootstrap /etc/passwd /etc/passwd
COPY --from=build-bootstrap /out/bootstrap /bootstrap
USER 65534:65534
ENTRYPOINT ["/bootstrap"]
