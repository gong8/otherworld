DB_URL=postgres://otherworld:otherworld@localhost:55432/fabric?sslmode=disable

.PHONY: dev test test-db up sqlc

dev: ## run the world locally with fake brains
	cd fabric && DATABASE_URL=$(DB_URL) go run ./cmd/fabricd -brains fake -addr :8080

test: ## unit tests (no db)
	cd fabric && go test ./... -short

test-db: ## integration tests (compose postgres must be up)
	cd fabric && DATABASE_URL=$(DB_URL) go test ./... -run Integration -v

up:
	docker compose up -d postgres

sqlc:
	cd fabric && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
