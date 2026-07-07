.PHONY: dev build build-all test test-cover test-race bench integration
.PHONY: lint vet check openapi-check openapi-gen tidy-check
.PHONY: docker-build docker-up docker-down
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
	go test -count=1 -tags integration ./internal/platform/postgrestest ./internal/admin/store/postgres ./internal/core/store/postgres

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

# ---- Docker ----
docker-build:
	docker build --target admin      -t orbitjob-admin:latest      .
	docker build --target scheduler  -t orbitjob-scheduler:latest  .
	docker build --target dispatcher -t orbitjob-dispatcher:latest .
	docker build --target worker     -t orbitjob-worker:latest     .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

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
