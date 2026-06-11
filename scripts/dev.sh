#!/usr/bin/env bash
# one command: postgres (healthy) + fabricd (fake brains) + next dev.
# stop with ctrl-c — the fabric is cleaned up on exit.
set -euo pipefail
cd "$(dirname "$0")/.."

docker compose up -d --wait postgres

export DATABASE_URL="${DATABASE_URL:-postgres://otherworld:otherworld@localhost:55432/fabric?sslmode=disable}"

(cd fabric && go build -o fabricd ./cmd/fabricd)
# dev is a sandbox; every pnpm dev is a new world
./fabric/fabricd -brains fake -addr :8080 -fresh &
FABRIC_PID=$!
trap 'kill "$FABRIC_PID" 2>/dev/null || true' EXIT INT TERM

next dev
