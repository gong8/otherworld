DB_URL=postgres://otherworld:otherworld@localhost:55432/fabric?sslmode=disable

.PHONY: dev dev-real test test-db golden up sqlc

dev: ## run the world locally with fake brains
	cd fabric && DATABASE_URL=$(DB_URL) go run ./cmd/fabricd -brains fake -addr :8080 -fresh

# dev-real keeps the default debounce (real pacing — thinks take seconds) and
# requires anthropic model access enabled in the aws bedrock console; the boot
# preflight refuses clearly if it is not. See fabric/README.md "real brains".
dev-real: ## run the world locally with real bedrock brains (still -fresh)
	cd fabric && DATABASE_URL=$(DB_URL) go run ./cmd/fabricd -brains bedrock -addr :8080 -fresh

test: ## unit tests (no db)
	cd fabric && go test ./... -short

test-db: ## integration tests (compose postgres must be up)
	cd fabric && DATABASE_URL=$(DB_URL) go test ./... -run Integration -v

# golden runs the orchestrator golden-transcript tests (deterministic, no db)
# plus the headless e2e demo against compose postgres (requires DATABASE_URL or
# the default compose url; runs with -race -count=1 so the result is definitive).
golden: ## orchestrator golden tests + headless e2e (compose postgres must be up)
	cd fabric && go test ./internal/orchestrator/... -v -count=1
	cd fabric && DATABASE_URL=$(DB_URL) go test ./cmd/fabricd -tags integration -race -v -count=1

up:
	docker compose up -d postgres

sqlc:
	cd fabric && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
