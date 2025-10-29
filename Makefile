GO111MODULE=on

.PHONY: test bench lint vet tidy build build-linux-arm64 clean coverage run

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

build:
	go build -o bin/ ./...

build-linux-arm64:
	mkdir -p bin/linux-arm64/ && GOOS=linux GOARCH=arm64 go build -o bin/linux-arm64/ ./... && cp config/*.yaml bin/linux-arm64/ 2>/dev/null || true

tidy:
	go mod tidy

clean:
	rm -rf bin/
	rm -rf coverage/

run:
	go run ./cmd/gateway/main.go

backtest:
	go run ./cmd/backtest/main.go --data=./data.csv --strategy=$(STRATEGY)

