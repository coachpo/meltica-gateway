# Testing Guide for Meltica

This guide explains where to place different types of tests in the project.

## Test Types and Locations

### 1. Unit Tests → Same Directory as Source Code ✅

**Convention**: Place `*_test.go` files alongside the code they test

```
pkg/events/
  ├── event.go           # Source code
  └── event_test.go      # Unit tests ← HERE

internal/config/
  ├── config.go
  └── config_test.go     # Unit tests ← HERE
```

**Example**: See `pkg/events/event_test.go`

**When to use**:
- Testing individual functions/methods
- Testing package-level logic
- Testing internal unexported functions
- Benchmarking individual components

**Run with**:
```bash
go test ./pkg/events         # Test single package
go test ./...                # Test all packages
go test -bench=. ./pkg/events  # Run benchmarks
```

---

### 2. Integration Tests → `/test/integration/` 📦

**Location**: External integration tests that span multiple packages

```
test/integration/
  ├── databus_integration_test.go
  ├── dispatcher_flow_test.go
  └── producer_consumer_test.go
```

**Example**: See `test/integration/databus_integration_test.go`

**When to use**:
- Testing interactions between multiple packages
- Testing complete workflows
- Database or external service integration
- System-level behavior

**Run with**:
```bash
go test ./test/integration/...
go test -short ./...           # Skip integration tests (use t.Skip in short mode)
```

---

### 3. End-to-End Tests → `/test/e2e/` 🔄

**Location**: Full application tests

```
test/e2e/
  ├── gateway_startup_test.go
  ├── trading_flow_test.go
  └── fixtures/
      └── test_config.yaml
```

**When to use**:
- Testing the complete application
- Testing with real configuration
- Testing deployment scenarios
- Performance testing under load

---

### 4. Contract/Behavior Tests → `/test/contract/`

**Location**: API contract and behavior verification

```
test/contract/
  ├── consumer_contract_test.go
  ├── provider_contract_test.go
  └── ws_routing_test.go
```

**When to use**:
- Verifying API contracts
- Testing expected behaviors
- Compatibility testing

---

## Best Practices

### Unit Tests
```go
// pkg/events/event_test.go
package events  // Same package - can test private functions

func TestEventReset(t *testing.T) {
    ev := &Event{TraceID: "test"}
    ev.Reset()
    
    if ev.TraceID != "" {
        t.Errorf("expected empty TraceID")
    }
}

func BenchmarkEventReset(b *testing.B) {
    ev := &Event{TraceID: "test"}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ev.Reset()
    }
}
```

### Integration Tests
```go
// test/integration/databus_integration_test.go
package integration  // Different package - tests public API only

import "github.com/coachpo/meltica/internal/bus/databus"

func TestDatabusPubSub(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    // Test multiple components together
}
```

### Table-Driven Tests
```go
func TestEventKindString(t *testing.T) {
    tests := []struct {
        name string
        kind EventKind
        want string
    }{
        {"market data", KindMarketData, "market_data"},
        {"exec report", KindExecReport, "exec_report"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := tt.kind.String(); got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## Running Tests

```bash
# Run all tests
make test

# Run with coverage
make coverage

# Run specific package
go test ./pkg/events -v

# Run specific test
go test ./pkg/events -run TestEventReset -v

# Run benchmarks
go test -bench=. ./pkg/events

# Skip slow tests
go test -short ./...

# Run with race detector
go test -race ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## Coverage Requirements

Per TS-01: **Minimum 70% code coverage required**

```bash
make coverage  # Enforces 70% threshold
```

---

## Summary

| Test Type | Location | Package Name | Access |
|-----------|----------|--------------|--------|
| **Unit Tests** | Same dir as code | `package foo` | Private + Public |
| **Integration** | `/test/integration/` | `package integration` | Public only |
| **E2E Tests** | `/test/e2e/` | `package e2e` | Application level |
| **Contract** | `/test/contract/` | `package contract` | Public API |

**Golden Rule**: If you're testing a single package's internal logic, put the test next to the code. If you're testing multiple packages together, use `/test/`.
