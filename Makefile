.PHONY: dev build build-all test test-cover test-cover-check test-race bench bench-etcd-memory bench-etcd integration
.PHONY: lint vet check openapi-check openapi-gen tidy-check
.PHONY: docker-build kind-load observability-status
.PHONY: kind-up kind-db kind-status kind-down kind-verify kind-env
.PHONY: helm-migrations-sync helm-migrations-check helm-check
.PHONY: migrate-up migrate-version
.PHONY: loadtest-preflight loadtest-generate loadtest-prepare loadtest-run
.PHONY: loadtest-verify loadtest-report loadtest-clean loadtest-smoke loadtest-long loadtest-full
.PHONY: clean

DEV_DSN      ?= postgres://postgres:postgres@localhost:5432/orbitjob?sslmode=disable
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/orbitjob?sslmode=disable

# ---- Build ----
build:
	go build ./...

build-all:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/admin-api   ./cmd/admin-api/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/scheduler   ./cmd/scheduler/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/operator    ./cmd/operator/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/configure   ./cmd/configure/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/bootstrap   ./cmd/bootstrap/

# ---- Test ----
test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Tiered coverage gate, thresholds defined in CLAUDE.md
test-cover-check: test-cover
	bash scripts/check-coverage.sh coverage.out

test-race:
	go test -race ./...

# Packages that carry benchmarks. `bench` is scoped to them so it stays a
# fast always-on target; plain ./... would rerun every test suite for the
# same measurements.
BENCH_PACKAGES := \
	./internal/core/domain/coordination/ \
	./internal/core/domain/jobrun/ \
	./internal/core/domain/check/ \
	./internal/core/app/execution/ \
	./internal/core/app/checkobserve/ \
	./internal/platform/election/

# -run='^$' skips the test suites: the benchmark run measures, it does not
# re-verify. bench.txt is the file the Benchmark workflow feeds to
# github-action-benchmark, so the tee is part of the contract.
bench:
	go test -run='^$$' -bench=. -benchmem $(BENCH_PACKAGES) | tee bench.txt

# bench-etcd-memory runs the election benchmarks at higher count for
# benchstat-style comparisons. Takes minutes: the contention benchmark holds
# each lock for a millisecond by design.
bench-etcd-memory:
	go test -run='^$$' -bench=. -benchmem -count=5 ./internal/platform/election/

# bench-etcd runs the election benchmarks against a live etcd. Requires an
# etcd reachable at ETCD_ENDPOINTS (default localhost:2379). Against an
# unreachable endpoint the run does not fail fast — the etcd client retries
# in the background — so bring the server up first.
bench-etcd:
	go test -tags etcd -run='^$$' -bench=. -benchmem -count=5 ./internal/platform/election/

integration:
	go test -count=1 -tags integration ./db/migrations ./internal/platform/postgrestest ./internal/admin/bootstrap ./internal/admin/http ./internal/admin/store/postgres ./internal/core/store/postgres

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
# Local builds serve development only. Published images are built and pushed
# by release.yml on tag; helm install pulls ghcr.io/s3loy/orbitjob-* directly.
#
# TAG follows the chart appVersion, so upgrades touch Chart.yaml only.
# Dev loop: make docker-build TAG=dev && make kind-load TAG=dev
REGISTRY ?= ghcr.io/s3loy
APP_VERSION := $(shell sed -n 's/^appVersion: *"\(.*\)"/\1/p' charts/orbitjob/Chart.yaml)
TAG ?= v$(APP_VERSION)

DOCKER_COMPONENTS := admin-api:admin scheduler:scheduler operator:operator migrate:migrate bootstrap:bootstrap

docker-build:
	@set -e; for pair in $(DOCKER_COMPONENTS); do \
	  name=$${pair%%:*}; target=$${pair##*:}; \
	  echo "==> orbitjob-$$name (target $$target)"; \
	  docker build --target $$target -t $(REGISTRY)/orbitjob-$$name:$(TAG) .; \
	done

# Load locally built images into kind to avoid a registry pull
kind-load:
	@set -e; for pair in $(DOCKER_COMPONENTS); do \
	  name=$${pair%%:*}; \
	  echo "==> load orbitjob-$$name"; \
	  kind load docker-image $(REGISTRY)/orbitjob-$$name:$(TAG) --name orbitjob-dev; \
	done

# The Docker Compose local path was removed. The product is Kubernetes-only and
# the operator needs a cluster, so a Compose install could not run it; the
# Dockerfile can no longer build the dispatcher or worker stages either. Use the
# kind path (make kind-up -> kind-db -> docker-build -> kind-load -> helm).
#
# Checks an installation that is already running and port-forwarded.
observability-status:
	@bash scripts/observability-status.sh

# Installs the cluster observability stack: kube-prometheus-stack as its OWN
# helm release into the "monitoring" namespace (never part of charts/orbitjob),
# plus the OrbitJob scrape wiring, alert rules, and Grafana dashboard. Values
# come from the file under deploy/monitoring/, never from --set. Idempotent:
# re-running an upgrade with the same values is a no-op. Never touches the
# orbitjob release.
KPS_VERSION ?= 91.4.1

monitoring-up:
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
	helm repo update prometheus-community >/dev/null
	helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
	  --version $(KPS_VERSION) \
	  --namespace monitoring --create-namespace \
	  -f deploy/monitoring/values-kube-prometheus-stack.yaml
	kubectl apply -f deploy/monitoring/namespace.yaml
	kubectl apply -f deploy/monitoring/orbitjob-observability.yaml
	kubectl apply -f deploy/monitoring/grafana-dashboards.yaml

monitoring-down:
	kubectl delete -f deploy/monitoring/orbitjob-observability.yaml --ignore-not-found
	kubectl delete -f deploy/monitoring/grafana-dashboards.yaml --ignore-not-found
	helm uninstall kube-prometheus-stack --namespace monitoring || true
	kubectl delete namespace monitoring --ignore-not-found

# ---- Local Kubernetes ----
helm-migrations-sync:
	bash scripts/helm-migrations.sh sync

helm-migrations-check:
	bash scripts/helm-migrations.sh check

# operator.namespaceTenants is required, so rendering the chart needs a value.
# The bootstrap tenant owns the default namespace, which is what a local install
# uses too.
HELM_SAMPLE_TENANTS ?= "default=00000000000000000000000001"

helm-check: helm-migrations-check
	helm lint charts/orbitjob --set operator.namespaceTenants=$(HELM_SAMPLE_TENANTS)
	helm template orbitjob charts/orbitjob --set operator.namespaceTenants=$(HELM_SAMPLE_TENANTS) >/dev/null
	bash scripts/chart-assert.sh

# Prepares PostgreSQL and the installation Secret the chart needs. kind-up only
# builds the cluster; without this the Helm hooks fail with
# CreateContainerConfigError and the release lands in `failed`.
kind-db:
	bash scripts/kind-db.sh

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

kind-verify: helm-check
	ORBITJOB_IMAGE_TAG=$(TAG) bash deploy/kind/verify-install.sh

kind-down:
	@printf "Type 'delete-kind' to remove the orbitjob-dev cluster: "; read answer; \
	[ "$$answer" = "delete-kind" ] || { echo "Cancelled."; exit 1; }; \
	kind delete cluster --name orbitjob-dev

kind-env:
	@if ! kubectl config current-context 2>/dev/null | grep -q "kind-orbitjob-dev"; then \
		echo "Error: Not connected to kind-orbitjob-dev cluster"; \
		echo "Run: kubectl config use-context kind-orbitjob-dev"; \
		exit 1; \
	fi
	@echo "# OrbitJob environment variables for kind cluster"
	@echo "# Source this file: source <(make kind-env)"
	@echo ""
	@echo "export ORBITJOB_API_KEY=\"$$(kubectl -n orbitjob-system get secret bootstrap-api-key -o jsonpath='{.data.api-key}' 2>/dev/null | base64 -d || echo 'SECRET_NOT_FOUND')\""
	@echo "export ORBITJOB_API=\"http://localhost:18080\""
	@echo ""
	@echo "# Verify connection:"
	@echo "# curl -H \"Authorization: Bearer \$$ORBITJOB_API_KEY\" \$$ORBITJOB_API/api/v1/tenants"

# ---- Database Migrations ----
migrate-up:
	MIGRATION_MODE=migrate MIGRATOR_DSN="$(DATABASE_URL)" MIGRATIONS_DIR=db/migrations go run ./cmd/migrate

migrate-version:
	psql "$(DATABASE_URL)" -Atqc 'SELECT COALESCE(max(version), 0) FROM schema_migrations'

# ---- Clean ----
clean:
	rm -f coverage.out
	rm -rf bin/

# ---- Load Qualification ----
# Config and profile must stay in sync: standard.yaml→standard,
# smoke.yaml→smoke, long.yaml→long.
LOADTEST_CONFIG ?= test/load/config/standard.yaml
LOADTEST_PROFILE ?= standard
LOADTEST_IMAGES ?= test/load/config/images.lock.yaml
RUN_ID ?= $(shell date -u +%Y%m%dT%H%M%SZ)-$(shell git rev-parse --short HEAD)

loadtest-preflight:
	go run ./scripts/loadtest preflight --config "$(LOADTEST_CONFIG)" --images "$(LOADTEST_IMAGES)" --profile "$(LOADTEST_PROFILE)"

loadtest-generate:
	go run ./scripts/loadtest generate --config "$(LOADTEST_CONFIG)" --images "$(LOADTEST_IMAGES)" --run-id "$(RUN_ID)"

loadtest-prepare:
	kubectl apply -f deploy/load/namespace.yaml
	kubectl apply -f deploy/load/operations-rbac.yaml
	kubectl apply -f deploy/load/fixture-configmap.yaml
	kubectl apply -f deploy/load/fixture-tls-secret.yaml
	kubectl apply -f deploy/load/fixture.yaml
	kubectl apply -f deploy/load/postgres.yaml

loadtest-run:
	@echo "loadtest run requires a live cluster; use make loadtest-full for full flow"

loadtest-verify:
	go run ./scripts/loadtest verify --config "$(LOADTEST_CONFIG)" --run-id "$(RUN_ID)"

loadtest-report:
	go run ./scripts/loadtest report --run-id "$(RUN_ID)"

loadtest-clean:
	@test -n "$(RUN_ID)" || { printf 'RUN_ID is required\n' >&2; exit 2; }
	go run ./scripts/loadtest clean --run-id "$(RUN_ID)" --confirm

loadtest-smoke:
	@echo "NON-STANDARD RUN - NOT A RELEASE QUALIFICATION"
	go run ./scripts/loadtest preflight --config test/load/config/smoke.yaml --images "$(LOADTEST_IMAGES)" --profile smoke --check-only

loadtest-long:
	@echo "NON-STANDARD RUN - NOT A RELEASE QUALIFICATION"
	go run ./scripts/loadtest preflight --config test/load/config/long.yaml --images "$(LOADTEST_IMAGES)" --profile long --check-only

loadtest-full: loadtest-preflight loadtest-generate
	@echo "Full 4-hour qualification run requires manual review of preflight and generated manifest."
