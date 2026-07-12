.PHONY: dev build build-all test test-cover test-race bench integration
.PHONY: lint vet check openapi-check openapi-gen tidy-check
.PHONY: docker-build docker-up docker-down docker-status observability-status
.PHONY: kind-up kind-status kind-down kind-v020-verify
.PHONY: helm-migrations-sync helm-migrations-check helm-check
.PHONY: env-init env-check env-clean bootstrap-key grafana-password docker-reset
.PHONY: migrate-up migrate-down migrate-version
.PHONY: clean

DEV_DSN      ?= postgres://postgres:postgres@localhost:5432/orbitjob?sslmode=disable
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/orbitjob?sslmode=disable

# ---- Development ----
dev:
	DEV_DSN=$(DEV_DSN) go run ./cmd/devserver

# ---- Build ----
build:
	go build ./...

build-all:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/admin-api   ./cmd/admin-api/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/scheduler   ./cmd/scheduler/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/dispatcher  ./cmd/dispatcher/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/worker      ./cmd/worker/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/bootstrap   ./cmd/bootstrap/

# ---- Test ----
test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

test-race:
	go test -race ./...

bench:
	go test -bench=. -benchmem ./...

bench-etcd-memory:
	go test -bench=. -benchmem -count=5 ./internal/platform/election/ ./internal/platform/discovery/

bench-etcd:
	go test -tags etcd -bench=. -benchmem -count=5 ./internal/platform/election/ ./internal/platform/discovery/

bench-etcd-compare:
	bash scripts/bench-etcd.sh

integration:
	go test -count=1 -tags integration ./db/migrations ./internal/platform/postgrestest ./internal/admin/bootstrap ./internal/admin/http ./internal/admin/store/postgres ./internal/core/store/postgres ./test/integration

# ---- Lint & Vet ----
lint:
	golangci-lint run

vet:
	go vet ./...

# ---- Quality Checks ----
check: lint vet test-race openapi-check tidy-check
	@echo "All checks passed."

openapi-check:
	go run ./cmd/openapi-gen -check -out api/openapi.yaml

openapi-gen:
	go run ./cmd/openapi-gen -out api/openapi.yaml

tidy-check:
	go mod tidy && git diff --exit-code go.sum

# ---- Local environment ----
env-init:
	@go run ./scripts/env-init.go
	@echo "Local environment is initialized and validated."

env-check:
	@go run ./scripts/env-init.go -check

env-clean:
	@printf "Type 'delete-env' to remove .env: "; read answer; \
	[ "$$answer" = "delete-env" ] || { echo "Cancelled."; exit 1; }; \
	rm -f .env

# ---- Docker ----
docker-build:
	docker build --target admin      -t orbitjob-admin:latest      .
	docker build --target scheduler  -t orbitjob-scheduler:latest  .
	docker build --target dispatcher -t orbitjob-dispatcher:latest .
	docker build --target worker     -t orbitjob-worker:latest     .

docker-up: env-init
	docker compose config >/dev/null
	docker compose up -d --build
	@bash scripts/docker-wait.sh
	@set -a; . ./.env; set +a; bash scripts/observability-status.sh
	@printf "\nOrbitJob is ready.\n\nAdmin API:  http://localhost:8080\nPrometheus: http://localhost:9090\nGrafana:    http://localhost:3000\n\nExport the bootstrap key:\n  export ORBITJOB_API_KEY=\"$$(make --no-print-directory bootstrap-key)\"\n\n"

docker-status:
	docker compose ps -a
	@curl -fsS http://localhost:8080/healthz >/dev/null && echo "Admin API: healthy"
	@set -a; . ./.env; set +a; bash scripts/observability-status.sh

observability-status: env-check
	@set -a; . ./.env; set +a; bash scripts/observability-status.sh

bootstrap-key:
	@docker compose run --rm --no-deps secret-init cat /run/secrets/orbitjob/bootstrap-api-key/api-key

grafana-password: env-check
	@set -a; . ./.env; set +a; printf '%s\n' "$$GRAFANA_PASSWORD"

docker-down:
	docker compose down

docker-reset:
	@printf "Type 'delete-volumes' to remove OrbitJob containers and volumes: "; read answer; \
	[ "$$answer" = "delete-volumes" ] || { echo "Cancelled."; exit 1; }; \
	docker compose down -v

# ---- Local Kubernetes ----
helm-migrations-sync:
	bash scripts/helm-migrations.sh sync

helm-migrations-check:
	bash scripts/helm-migrations.sh check

helm-check: helm-migrations-check
	helm lint charts/orbitjob
	helm template orbitjob charts/orbitjob >/dev/null

kind-up:
	@if kind get clusters | grep -qx orbitjob-dev; then \
		echo "kind cluster orbitjob-dev already exists."; \
	else \
		kind create cluster --config deploy/kind/orbitjob-dev.yaml --wait 5m; \
	fi
	@kubectl config use-context kind-orbitjob-dev >/dev/null
	@$(MAKE) --no-print-directory kind-status

kind-status:
	@test "$$(kubectl config current-context)" = "kind-orbitjob-dev"
	@kubectl wait --for=condition=Ready node/orbitjob-dev-control-plane --timeout=60s
	@kubectl get nodes -o wide
	@kubectl get pods -A

kind-v020-verify: helm-check
	bash deploy/kind/verify-v020.sh

kind-down:
	@printf "Type 'delete-kind' to remove the orbitjob-dev cluster: "; read answer; \
	[ "$$answer" = "delete-kind" ] || { echo "Cancelled."; exit 1; }; \
	kind delete cluster --name orbitjob-dev

# ---- Database Migrations ----
migrate-up:
	golang-migrate -path db/migrations -database "$(DATABASE_URL)" up

migrate-down:
	golang-migrate -path db/migrations -database "$(DATABASE_URL)" down 1

migrate-version:
	golang-migrate -path db/migrations -database "$(DATABASE_URL)" version

# ---- Clean ----
clean:
	rm -f coverage.out
	rm -rf bin/
