#!/usr/bin/env bash
# Full local verification: formatting, vet, race-enabled tests, frontend build.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

unformatted="$(gofmt -l cmd internal migrations)"
if [ -n "$unformatted" ]; then
  echo "gofmt required for:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go vet ./...
GIN_MODE=release go test -race ./...

if [ ! -d web/node_modules ]; then
  (cd web && npm ci)
fi
(cd web && npm run build)

echo "Verification passed."
