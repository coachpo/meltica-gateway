GO111MODULE=on

.PHONY: test bench lint vet tidy build build-linux-arm64 standards ci clean binance-ws-test coverage-architecture coverage-full coverage contract-ws-routing

lint:
	golangci-lint run --config .golangci.yml

vet:
	go vet ./...

test:
	go test ./... -race -count=1 -timeout=30s

coverage:
	@echo "Running tests with coverage (≥70% required by TS-01)..."
	@go test ./... -race -coverprofile=coverage.out -count=1 -timeout=30s
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Total coverage: $$COVERAGE%"; \
	if [ $$(echo "$$COVERAGE < 70.0" | bc -l) -eq 1 ]; then \
		echo "ERROR: Coverage $$COVERAGE% is below 70% threshold (TS-01)"; \
		exit 1; \
	fi; \
	echo "✓ Coverage threshold satisfied: $$COVERAGE% ≥ 70%"
	@echo "Generate HTML report with: go tool cover -html=coverage.out"

bench:
	go test -bench . -benchmem ./...

contract-ws-routing:
	go test ./tests/contract/ws-routing -race -count=1

build:
	go build -o out/ ./...

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o out/linux-arm64/ ./...

tidy:
	go mod tidy

coverage-architecture:
	mkdir -p coverage
	go test ./tests/architecture -covermode=atomic -count=1 -coverprofile=coverage/architecture.out

coverage-full:
	mkdir -p coverage
	go test ./... -covermode=atomic -coverprofile=coverage/full.out

clean:
	rm -rf out/
	rm -rf bin/
