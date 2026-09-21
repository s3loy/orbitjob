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
FROM base AS build-admin
ARG TARGETOS=linux
ARG TARGETARCH
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/admin-api ./cmd/admin-api/

FROM base AS build-scheduler
ARG TARGETOS=linux
ARG TARGETARCH
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler/

FROM base AS build-operator
ARG TARGETOS=linux
ARG TARGETARCH
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/operator ./cmd/operator/

FROM base AS build-healthcheck
ARG TARGETOS=linux
ARG TARGETARCH
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck/

FROM base AS build-bootstrap
ARG TARGETOS=linux
ARG TARGETARCH
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/bootstrap ./cmd/bootstrap/

FROM base AS build-migrate
ARG TARGETOS=linux
ARG TARGETARCH
COPY . .
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
