#!/usr/bin/env bash
# One-command local startup: install/build the frontend, then run the Go server.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required (tested with Go 1.22.5)" >&2
  exit 1
fi
if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required (tested with Node 24.3.0 / npm 11.4.2)" >&2
  exit 1
fi

if [ ! -d web/node_modules ]; then
  echo "Installing frontend dependencies..."
  (cd web && npm ci)
fi

if [ "${SKIP_WEB_BUILD:-0}" != "1" ]; then
  echo "Building frontend..."
  (cd web && npm run build)
fi

DB="${DB:-data/demo.db}"
ADDR="${ADDR:-:8080}"
exec go run ./cmd/server --db "$DB" --addr "$ADDR" --web-dir web/dist
