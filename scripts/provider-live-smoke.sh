#!/usr/bin/env bash
set -euo pipefail

# Explicitly opt-in Provider smoke entrypoint.  The default path is a truthful
# no-op so ordinary CI and the 48-trial Fixture demo never contact a provider.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "${LIVE_PROVIDER_SMOKE:-0}" != "1" ]]; then
  echo "SKIP: set LIVE_PROVIDER_SMOKE=1 to run the controlled Provider Adapter smoke"
  exit 0
fi

provider="${LIVE_PROVIDER:-}"
model="${LIVE_PROVIDER_MODEL:-}"
case "${provider}" in
  openai) credential_name="OPENAI_API_KEY"; credential_file="${OPENAI_API_KEY_FILE:-}" ;;
  anthropic) credential_name="ANTHROPIC_API_KEY"; credential_file="${ANTHROPIC_API_KEY_FILE:-}" ;;
  *)
    echo "FAIL CLOSED: LIVE_PROVIDER must be openai or anthropic" >&2
    exit 2
    ;;
esac
if [[ -z "${model}" ]]; then
  echo "FAIL CLOSED: LIVE_PROVIDER_MODEL must name the exact model" >&2
  exit 2
fi
if [[ -n "${credential_file}" ]]; then
  if [[ ! -r "${credential_file}" ]] || [[ ! -s "${credential_file}" ]] || [[ -z "$(tr -d '[:space:]' <"${credential_file}")" ]]; then
    echo "FAIL CLOSED: ${credential_name}_FILE is missing, empty or unreadable" >&2
    exit 2
  fi
elif [[ -z "${!credential_name:-}" ]]; then
  echo "FAIL CLOSED: ${credential_name} is required; no fixture fallback is allowed" >&2
  exit 2
fi

# Hard ceilings.  Values may be lowered for a stricter run but never raised.
max_tokens="${LIVE_PROVIDER_MAX_TOKENS:-64}"
timeout_seconds="${LIVE_PROVIDER_TIMEOUT_SECONDS:-30}"
max_cost_usd="${LIVE_PROVIDER_MAX_COST_USD:-0.05}"
if ! [[ "${max_tokens}" =~ ^[0-9]+$ ]] || (( max_tokens < 1 || max_tokens > 64 )); then
  echo "FAIL CLOSED: LIVE_PROVIDER_MAX_TOKENS must be an integer in 1..64" >&2
  exit 2
fi
if ! awk -v value="${timeout_seconds}" 'BEGIN { exit !(value > 0 && value <= 30) }'; then
  echo "FAIL CLOSED: LIVE_PROVIDER_TIMEOUT_SECONDS must be in (0,30]" >&2
  exit 2
fi
if ! awk -v value="${max_cost_usd}" 'BEGIN { exit !(value > 0 && value <= 0.05) }'; then
  echo "FAIL CLOSED: LIVE_PROVIDER_MAX_COST_USD must be in (0,0.05]" >&2
  exit 2
fi

python_bin="python3"
if [[ -x "${ROOT_DIR}/workers/python/.venv/bin/python" ]]; then
  python_bin="${ROOT_DIR}/workers/python/.venv/bin/python"
fi

export LIVE_PROVIDER LIVE_PROVIDER_MODEL
export LIVE_PROVIDER_MAX_TOKENS="${max_tokens}"
export LIVE_PROVIDER_TIMEOUT_SECONDS="${timeout_seconds}"
export LIVE_PROVIDER_MAX_COST_USD="${max_cost_usd}"
PYTHONPATH="${ROOT_DIR}/workers/python:${ROOT_DIR}/workers/python/gen" \
  "${python_bin}" -m pytest -q "${ROOT_DIR}/workers/python/tests/test_provider_live.py" \
  --maxfail=1

if [[ "${LIVE_PROVIDER_FULL_CHAIN:-0}" == "1" ]]; then
  echo "PREFLIGHT: full-chain requested with caps cases=1 tokens<=${max_tokens} timeout<=${timeout_seconds}s cost<=${max_cost_usd}" \
    "but no controlled temporary matched manifest was authorized; no real full-chain result is claimed." >&2
  # The separate test is an explicit, fail-closed marker.  It remains skipped
  # after adapter smoke unless a future authorized harness is supplied.
  PYTHONPATH="${ROOT_DIR}/workers/python:${ROOT_DIR}/workers/python/gen" \
    "${python_bin}" -m pytest -q "${ROOT_DIR}/workers/python/tests/test_provider_live.py" \
    -k full_chain_preflight --maxfail=1
fi
