#!/usr/bin/env bash
set -euo pipefail

# OrbitJob etcd vs memory benchmark comparison script.
# Usage: ./scripts/bench-etcd.sh [--count N] [--output DIR]
#
# Requires: docker compose, Go toolchain
# Optional: benchstat (go install golang.org/x/perf/cmd/benchstat@latest)

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

COUNT=5
OUTPUT_DIR=""

while [[ $# -gt 0 ]]; do
	case $1 in
		--count)
			COUNT="$2"
			shift 2
			;;
		--output)
			OUTPUT_DIR="$2"
			shift 2
			;;
		--help|-h)
			echo "Usage: $0 [--count N] [--output DIR]"
			exit 0
			;;
		*)
			echo "Unknown option: $1"
			exit 1
			;;
	esac
done

if [[ -n "$OUTPUT_DIR" ]]; then
	mkdir -p "$OUTPUT_DIR"
fi

MEMORY_OUT="${OUTPUT_DIR:-.}/bench-memory.txt"
ETCD_OUT="${OUTPUT_DIR:-.}/bench-etcd.txt"

# ---------------------------------------------------------------------------
# 1. Ensure etcd is running
# ---------------------------------------------------------------------------
echo "=== Checking etcd ==="
if ! docker compose ps etcd >/dev/null 2>&1 || ! docker compose exec -T etcd etcdctl endpoint health >/dev/null 2>&1; then
	echo "Starting etcd via docker compose..."
	docker compose up -d etcd
	# Wait for etcd to be healthy.
	for i in {1..30}; do
		if docker compose exec -T etcd etcdctl endpoint health >/dev/null 2>&1; then
			echo "etcd is healthy"
			break
		fi
		echo "Waiting for etcd... ($i/30)"
		sleep 1
	done
fi

# ---------------------------------------------------------------------------
# 2. Memory baseline (no etcd tag)
# ---------------------------------------------------------------------------
echo ""
echo "=== Memory baseline ==="
go test -bench=. -benchmem -count="$COUNT" ./internal/platform/election/ ./internal/platform/discovery/ > "$MEMORY_OUT"
cat "$MEMORY_OUT"

# ---------------------------------------------------------------------------
# 3. etcd benchmark (with etcd tag)
# ---------------------------------------------------------------------------
echo ""
echo "=== etcd with real cluster ==="
ETCD_ERR="${OUTPUT_DIR:-.}/bench-etcd.err"
if go test -tags etcd -bench=. -benchmem -count="$COUNT" ./internal/platform/election/ ./internal/platform/discovery/ > "$ETCD_OUT" 2>"$ETCD_ERR"; then
	cat "$ETCD_OUT"
else
	echo "etcd benchmark failed. stderr:"
	cat "$ETCD_ERR"
	exit 1
fi

# ---------------------------------------------------------------------------
# 4. benchstat comparison
# ---------------------------------------------------------------------------
echo ""
echo "=== Comparison ==="
if command -v benchstat >/dev/null 2>&1; then
	benchstat "$MEMORY_OUT" "$ETCD_OUT"
else
	echo "benchstat not found. Install with:"
	echo "  go install golang.org/x/perf/cmd/benchstat@latest"
	echo ""
	echo "Raw results saved to:"
	echo "  Memory: $MEMORY_OUT"
	echo "  etcd:   $ETCD_OUT"
fi
