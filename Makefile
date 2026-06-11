DB_URL=postgres://otherworld:otherworld@localhost:55432/fabric?sslmode=disable

.PHONY: dev test test-db golden up sqlc

dev: ## run the world locally with fake brains
	cd fabric && DATABASE_URL=$(DB_URL) go run ./cmd/fabricd -brains fake -addr :8080

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
