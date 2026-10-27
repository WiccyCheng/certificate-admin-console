#!/usr/bin/env bash
# Generate the separate 100,000-certificate demo database.
# The default data/demo.db is never touched.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DB="${1:-data/large-demo.db}"
CERTIFICATES="${CERTIFICATES:-100000}"

if [ -e "$DB" ]; then
  echo "Refusing to overwrite existing $DB. Remove it or choose a new path." >&2
  exit 1
fi

go run ./cmd/seed-large --db "$DB" --tenant east --certificates "$CERTIFICATES"
