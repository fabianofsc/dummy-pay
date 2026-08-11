.PHONY: run test test-integration lint db-up db-down

run:
	go run ./cmd/dummypay

# Runs everything go test ./... finds. Currently unit-only: the integration
# harness (schema-per-test against PostgreSQL, ADR-0013) lands in plan step
# 3.1. Once it exists, tests behind it skip automatically when no database is
# reachable, so this target keeps working unchanged.
test:
	go test ./...

# Same command as `test` today. Once plan step 3.1 lands, integration tests
# self-skip without a database; this target additionally requires one to be
# up via `make db-up` first, and is the target CI runs so a skip there means
# what it should: a database was expected and missing.
test-integration: db-up
	go test ./... -v

# go vet today. Grows into the fitness functions from plan phase 12
# (dependency direction, time discipline, assertion discipline) as they land.
lint:
	go vet ./...

db-up:
	docker compose up -d postgres
	@echo "waiting for postgres to be healthy..."
	@until docker compose ps postgres | grep -q healthy; do sleep 1; done

db-down:
	docker compose down
