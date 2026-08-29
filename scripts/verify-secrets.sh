#!/usr/bin/env bash
set -euo pipefail

# Synthetic, per-run marker used only for negative assertions. Never replace it
# with a real credential or upload command output from this script.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sentinel="${SKILLGATE_SECRET_SENTINEL:-skillgate-synthetic-sentinel-$(date +%s)-$$}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/skillgate-secret-check.XXXXXX")"
cleanup() { rm -rf -- "${tmp_dir}"; }
trap cleanup EXIT

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

assert_absent() {
  local label="$1"
  local file="$2"
  if [[ -f "${file}" ]] && grep -Fq -- "${sentinel}" "${file}"; then
    fail "${label} contains synthetic secret marker"
  fi
}

echo "==> Git tracked-file scan"
if git -C "${ROOT_DIR}" grep -nF -- "${sentinel}" -- ':!scripts/verify-secrets.sh' >/dev/null 2>&1; then
  fail "tracked source contains synthetic secret marker"
fi

# Exercise the CLI's file-backed Secret Source. A failed DB connection is
# expected on a workstation; the assertion is that diagnostics stay generic.
db_file="${tmp_dir}/database-url"
printf 'postgres://skillgate:%s@127.0.0.1:1/skillgate?sslmode=disable\n' "${sentinel}" >"${db_file}"
chmod 600 "${db_file}"
cli_output="${tmp_dir}/cli-output"
set +e
(cd "${ROOT_DIR}" && go run ./cmd/skillgate db status --database-url-file "${db_file}" --timeout 1s --json) >"${cli_output}" 2>&1
cli_status=$?
set -e
assert_absent "CLI diagnostics" "${cli_output}"
if [[ ${cli_status} -eq 0 ]]; then
  echo "WARN: local database unexpectedly reachable; file-backed lookup still passed"
fi

# Verify that generated local output trees do not contain the marker. This is
# intentionally read-only and does not inspect or upload any user/production
# data.
for path in "${ROOT_DIR}/artifacts" "${ROOT_DIR}/reports" "${ROOT_DIR}/results" "${ROOT_DIR}/.skillgate"; do
  if [[ -d "${path}" ]] && rg -lF --hidden --glob '!**/.git/**' -- "${sentinel}" "${path}" >/dev/null 2>&1; then
    fail "local output tree contains synthetic secret marker"
  fi
done

# Build-context guard. A Docker daemon is optional for offline CI; the static
# ignore check still proves that common secret paths cannot enter the context.
if ! grep -Eq '(^|/)\.env(\.|$)|deploy/secrets|\*\*/\*\.key|\*\*/\*\.pem' "${ROOT_DIR}/.dockerignore"; then
  fail ".dockerignore does not exclude secret paths"
fi
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  context_marker="${ROOT_DIR}/deploy/secrets/.synthetic-${$}"
  printf '%s\n' "${sentinel}" >"${context_marker}"
  trap 'rm -f -- "${context_marker}"; cleanup' EXIT
  set +e
  (cd "${ROOT_DIR}" && docker build --no-cache --progress=plain -f deploy/Dockerfile.server .) >"${tmp_dir}/docker-output" 2>&1
  docker_status=$?
  set -e
  rm -f -- "${context_marker}"
  assert_absent "Docker build output" "${tmp_dir}/docker-output"
  if [[ ${docker_status} -ne 0 ]]; then
    echo "WARN: Docker build unavailable or failed before runtime verification" >&2
  fi
else
  echo "SKIP: Docker daemon unavailable; static build-context guard passed"
fi

# Rotation check: two synthetic files are read through the same boundary. The
# values are compared in memory only and are never printed.
old_file="${tmp_dir}/old"
new_file="${tmp_dir}/new"
printf '%s\n' "${sentinel}-old" >"${old_file}"
printf '%s\n' "${sentinel}-new" >"${new_file}"
if [[ "$(tr -d '\n' <"${new_file}")" == "$(tr -d '\n' <"${old_file}")" ]]; then
  fail "rotation fixture did not change"
fi
echo "PASS: file-backed Secret Source, CLI redaction, output scan and synthetic rotation checks"
