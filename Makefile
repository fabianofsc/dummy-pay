.PHONY: run test test-integration lint db-up db-down docker-run docker-stop docker-push

DOCKER_IMAGE ?= fabianofsc/dummy-pay

# run starts postgres and dummypay via docker compose — only Docker required.
# No Go, no .env, no manual setup: clone, make run, smoke-test.
run: docker-run

# Go-native targets (needs Go + .env, used during development)
run-local: db-up
	export $$(grep -v '^#' .env | xargs) && go run ./cmd/dummypay

test:
	go test ./...

test-integration: db-up
	go test ./... -v

lint:
	go vet ./...
	go test ./internal/fitness/ -run 'TestDependencyDirection|TestTimeDiscipline|TestAssertionDiscipline|TestSuiteWallClockBudget' -count=1

suite-budget:
	@timeout 120s go test ./... -race -count=1 || (echo "suite exceeded wall-clock budget of 120s"; exit 1)

# docker-run builds the dummypay image and starts postgres + dummypay.
# Credentials are set in docker-compose.yml — edit there if needed.
docker-run:
	docker compose up --build -d
	@echo "dummypay listening on :8080"

docker-stop:
	docker compose down

# docker-push builds and publishes the fixed latest tag. Run `docker login`
# before invoking this target.
docker-push:
	docker build -t $(DOCKER_IMAGE):latest .
	docker push $(DOCKER_IMAGE):latest

# database-only targets (legacy, used by run-local and test-integration)
db-up:
	docker compose up -d postgres
	@echo "waiting for postgres to be healthy..."
	@until docker compose ps postgres | grep -q healthy; do sleep 1; done

db-down:
	docker compose down
