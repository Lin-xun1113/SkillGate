#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

echo "==> SkillGate M3 Runner Protocol Demo"
echo "==> 1. Compiling M0 demo experiment..."
go run ./cmd/skillgate compile experiments/csv-analysis-v1-demo.yaml --json

echo "==> 2. Running migrations on local/target PostgreSQL..."
DB_URL_FILE="${SKILLGATE_DATABASE_URL_FILE:-${DATABASE_URL_FILE:-}}"
TEMP_DB_URL_FILE=""
if [ -z "${DB_URL_FILE}" ] && [ -n "${DATABASE_URL:-}" ]; then
  TEMP_DB_URL_FILE="$(mktemp)"
  chmod 600 "${TEMP_DB_URL_FILE}"
  printf '%s\n' "${DATABASE_URL}" >"${TEMP_DB_URL_FILE}"
  DB_URL_FILE="${TEMP_DB_URL_FILE}"
fi
if [ -z "${DB_URL_FILE}" ]; then
  echo "SECRET_MISSING: set SKILLGATE_DATABASE_URL_FILE (or DATABASE_URL_FILE/DATABASE_URL for local use)" >&2
  exit 1
fi
cleanup() {
  if [ -n "${TEMP_DB_URL_FILE}" ]; then
    rm -f -- "${TEMP_DB_URL_FILE}"
  fi
}
trap cleanup EXIT
go run ./cmd/skillgate db migrate --database-url-file "${DB_URL_FILE}" --json

echo "==> 3. Materializing experiment into scheduler queue..."
go run ./cmd/skillgate experiment materialize experiments/csv-analysis-v1-demo.yaml --database-url-file "${DB_URL_FILE}" --json

echo "==> 4. Starting background gRPC Runner Server on :50051..."
go run ./cmd/skillgate serve --grpc-addr "127.0.0.1:50051" --database-url-file "${DB_URL_FILE}" &
SERVER_PID=$!
trap 'kill -9 "${SERVER_PID}" 2>/dev/null || true; cleanup' EXIT

sleep 1

echo "==> 5. Starting Python Fixture Worker (48 trials)..."
PYTHONPATH="workers/python:workers/python/gen" \
  "${ROOT_DIR}/workers/python/.venv/bin/python3" workers/python/fixture_worker/worker.py \
  --server "127.0.0.1:50051" \
  --worker-id "demo-worker-01" \
  --max-trials 48 \
  --delay 0.005

echo "==> 6. M3 Demo Finished Successfully!"
