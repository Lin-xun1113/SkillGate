#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
status=0

if [[ -f "${ROOT_DIR}/LICENSE" || -f "${ROOT_DIR}/LICENSE.md" || -f "${ROOT_DIR}/COPYING" ]]; then
  echo "INFO: project license file exists; verify owner approval and SPDX metadata manually"
else
  echo "BLOCKED: project license is not confirmed by the owner; no LICENSE was created" >&2
  status=2
fi

if command -v go-licenses >/dev/null 2>&1; then
  (cd "${ROOT_DIR}" && go-licenses csv ./...)
else
  echo "SKIP: go-licenses is not installed; install it in a controlled local/tooling environment" >&2
fi

if command -v npx >/dev/null 2>&1 && (cd "${ROOT_DIR}" && npx --no-install license-checker --version >/dev/null 2>&1); then
  (cd "${ROOT_DIR}" && npx --no-install license-checker --production --summary)
else
  echo "SKIP: local license-checker is not installed; install it in a controlled local/tooling environment" >&2
fi

if command -v piplicenses >/dev/null 2>&1; then
  (cd "${ROOT_DIR}" && piplicenses --from=mixed --format=csv)
else
  echo "SKIP: piplicenses is not installed; install it in a controlled local/tooling environment" >&2
fi

exit "${status}"
