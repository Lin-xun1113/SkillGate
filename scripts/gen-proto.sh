#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

echo "==> Generating Go gRPC/Protobuf code..."
mkdir -p gen/go
protoc --proto_path=proto \
  --go_out=gen/go --go_opt=paths=source_relative \
  --go-grpc_out=gen/go --go-grpc_opt=paths=source_relative \
  proto/runner/v1/runner.proto

echo "==> Generating Python gRPC/Protobuf code..."
mkdir -p workers/python/gen/runner/v1
touch workers/python/gen/__init__.py
touch workers/python/gen/runner/__init__.py
touch workers/python/gen/runner/v1/__init__.py

PYTHON_BIN="python3"
if [ -x "${ROOT_DIR}/workers/python/.venv/bin/python3" ]; then
  PYTHON_BIN="${ROOT_DIR}/workers/python/.venv/bin/python3"
fi

${PYTHON_BIN} -m grpc_tools.protoc \
  -Iproto \
  --python_out=workers/python/gen \
  --grpc_python_out=workers/python/gen \
  proto/runner/v1/runner.proto

echo "==> Protobuf code generation complete."
