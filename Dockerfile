# syntax=docker/dockerfile:1

# ============================================================
# Builder base: shared module cache
# ============================================================
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS base
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# ============================================================
# Build stages
# ============================================================
FROM --platform=$BUILDPLATFORM node:26-alpine AS build-console
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

# Keep Go build inputs independent of Console and repository tooling changes.
FROM base AS go-source
COPY api/ ./api/
COPY cmd/ ./cmd/
COPY internal/ ./internal/

FROM go-source AS build-admin
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/admin-api ./cmd/admin-api/

FROM go-source AS build-identity
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/orbitjob-identity ./cmd/orbitjob-identity/

FROM go-source AS build-scheduler
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler/

FROM go-source AS build-operator
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/operator ./cmd/operator/

FROM go-source AS build-log-archive
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/log-archive ./cmd/log-archive/

FROM go-source AS build-admission
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/admission ./cmd/admission/

FROM go-source AS build-healthcheck
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck/

FROM go-source AS build-bootstrap
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/bootstrap ./cmd/bootstrap/

FROM go-source AS build-migrate
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate/

# ============================================================
# Final images (FROM scratch)
# ============================================================
FROM scratch AS migrate
COPY --from=build-migrate /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-migrate /etc/passwd /etc/passwd
COPY --from=build-migrate /out/migrate /migrate
USER 65534:65534
ENTRYPOINT ["/migrate"]

FROM scratch AS admin
COPY --from=build-admin /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-admin /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-admin /etc/passwd /etc/passwd
COPY --from=build-healthcheck /out/healthcheck /healthcheck
COPY --from=build-admin /out/admin-api /admin-api
COPY --from=build-identity /out/orbitjob-identity /orbitjob-identity
COPY --from=build-console /src/web/dist /console
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

FROM scratch AS bootstrap
COPY --from=build-bootstrap /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-bootstrap /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-bootstrap /etc/passwd /etc/passwd
COPY --from=build-bootstrap /out/bootstrap /bootstrap
USER 65534:65534
ENTRYPOINT ["/bootstrap"]

FROM scratch AS operator
COPY --from=build-operator /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-operator /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-operator /etc/passwd /etc/passwd
COPY --from=build-operator /out/operator /operator
USER 65534:65534
ENTRYPOINT ["/operator"]

FROM scratch AS log-archive
COPY --from=build-log-archive /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-log-archive /out/log-archive /log-archive
USER 65534:65534
ENTRYPOINT ["/log-archive"]

FROM scratch AS admission
COPY --from=build-admission /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-admission /etc/passwd /etc/passwd
COPY --from=build-admission /out/admission /admission
USER 65534:65534
ENTRYPOINT ["/admission"]
