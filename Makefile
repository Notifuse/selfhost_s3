.PHONY: build test test-unit test-integration test-integration-up test-integration-down clean

# Build the binary
build:
	go build -o bin/selfhost_s3 ./cmd/selfhost_s3

# Run all tests (unit + integration if container is running)
test:
	go test ./...

# Run unit tests only (excludes integration tests)
test-unit:
	go test ./internal/... ./cmd/...

# Run integration tests (requires test container)
test-integration:
	go test ./integration/... -v

# Start test container and run integration tests
test-integration-up:
	docker compose -f compose.test.yaml up -d --build
	@echo "Waiting for container to be ready..."
	@sleep 2
	go clean -testcache && go test ./integration/... -v 2>&1 | tee /tmp/integration-test.log; \
	if grep -q "FAIL" /tmp/integration-test.log; then exit 1; fi

# Stop test container
test-integration-down:
	docker compose -f compose.test.yaml down

# Clean build artifacts
clean:
	rm -rf bin/
