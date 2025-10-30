# Trading Strategies

This package contains various trading strategy implementations for the Lambda framework.

## Built-in Strategies

### 1. NoOp Strategy
**File:** `noop.go`

A no-operation strategy that does nothing. Useful for:
- Monitoring market data without trading
- Testing lambda infrastructure
- Base template for new strategies

**Usage:**
```go
strategy := &strategies.NoOp{}
lambda := core.NewBaseLambda(id, config, bus, control, provider, pools, strategy)
```

### 2. Logging Strategy
**File:** `logging.go`

Logs all market events for debugging and analysis.

**Features:**
- Logs trades with prices
- Logs ticker updates
- Logs order book changes
- Logs all order lifecycle events

**Usage:**
```go
strategy := &strategies.Logging{
    Logger: log.Default(),
}
lambda := core.NewBaseLambda(id, config, bus, control, provider, pools, strategy)
```

## Demo Strategies

### 3. Market Making Strategy
**File:** `marketmaking.go`

Places buy and sell orders around the mid-price to capture the spread.

**Configuration:**
- `SpreadBps`: Spread in basis points (e.g., 50 = 0.5%)
- `OrderSize`: Size of each order
- `MaxOpenOrders`: Maximum orders per side

**Algorithm:**
1. Calculates mid-price from bid/ask
2. Places buy order at mid × (1 - spread)
3. Places sell order at mid × (1 + spread)
4. Requotes when price moves significantly

**Usage:**
```go
strategy := &strategies.MarketMaking{
    Lambda:        baseLambda,
    SpreadBps:     50,        // 0.5% spread
    OrderSize:     "1.0",
    MaxOpenOrders: 3,
}
```

### 4. Momentum Strategy
**File:** `momentum.go`

Trades in the direction of price momentum.

**Configuration:**
- `LookbackPeriod`: Number of trades to analyze
- `MomentumThreshold`: Minimum % change to trigger
- `OrderSize`: Size of each order
- `Cooldown`: Minimum time between trades

**Algorithm:**
1. Tracks price history over lookback period
2. Calculates momentum as % change
3. Buys when momentum > threshold (uptrend)
4. Sells when momentum < -threshold (downtrend)

**Usage:**
```go
strategy := &strategies.Momentum{
    Lambda:            baseLambda,
    LookbackPeriod:    20,
    MomentumThreshold: 0.5,    // 0.5% momentum
    OrderSize:         "1.0",
    Cooldown:          5 * time.Second,
}
```

### 5. Mean Reversion Strategy
**File:** `meanreversion.go`

Trades when price deviates from its moving average, expecting reversion.

**Configuration:**
- `WindowSize`: Moving average window
- `DeviationThreshold`: Deviation % to trigger
- `OrderSize`: Size of each order

**Algorithm:**
1. Calculates moving average over window
2. Measures deviation from MA
3. Buys when price < MA - threshold
4. Sells when price > MA + threshold
5. Closes when price reverts to MA

**Usage:**
```go
strategy := &strategies.MeanReversion{
    Lambda:             baseLambda,
    WindowSize:         50,
    DeviationThreshold: 1.0,    // 1% deviation
    OrderSize:          "1.0",
}
```

### 6. Grid Trading Strategy
**File:** `grid.go`

Places orders at regular price intervals forming a grid.

**Configuration:**
- `GridLevels`: Number of levels above/below base
- `GridSpacing`: Spacing between levels as %
- `OrderSize`: Size per grid level
- `BasePrice`: Center price (auto-set if 0)

**Algorithm:**
1. Sets base price at current market price
2. Places buy orders below at intervals
3. Places sell orders above at intervals
4. When filled, places opposite order at same level
5. Profits from price oscillation

**Usage:**
```go
strategy := &strategies.Grid{
    Lambda:      baseLambda,
    GridLevels:  5,
    GridSpacing: 0.5,    // 0.5% spacing
    OrderSize:   "0.5",
    BasePrice:   0,      // Auto-set to current price
}
```

## Creating Custom Strategies

### 1. Implement the TradingStrategy Interface

```go
package strategies

import (
    "context"
    "log"

    "github.com/coachpo/meltica/internal/app/lambda/core"
    "github.com/coachpo/meltica/internal/domain/schema"
)

type MyStrategy struct {
    Lambda interface {
        Logger() *log.Logger
        GetLastPrice() float64
        // Add other methods you need
    }
    
    // Your configuration fields
    MyParam float64
}

// Implement all 8 methods
func (s *MyStrategy) OnTrade(ctx context.Context, evt *schema.Event, payload schema.TradePayload, price float64) {
    // Your logic here
}

// ... implement other 7 methods
```

### 2. Create a Constructor

```go
func NewMyStrategy(lambda *core.BaseLambda, myParam float64) *MyStrategy {
    return &MyStrategy{
        Lambda:  lambda,
        MyParam: myParam,
    }
}
```

### 3. Use Your Strategy

```go
baseLambda := core.NewBaseLambda(id, config, bus, control, provider, pools, &NoOp{})
strategy := NewMyStrategy(baseLambda, 42.0)

// Replace the strategy
baseLambda.strategy = strategy
```

## Best Practices

### 1. State Management
- Use `sync.Mutex` for thread-safe state
- Store minimal state; derive from market data when possible
- Reset state on order rejections/cancellations

### 2. Error Handling
- Always check errors from `SubmitOrder`
- Log errors with context
- Implement fallback behavior

### 3. Risk Management
- Set position limits
- Implement cooldown periods
- Validate prices before submitting orders
- Track open orders to avoid over-trading

### 4. Performance
- Avoid blocking operations in callbacks
- Use atomic operations for simple counters
- Keep computations lightweight

### 5. Logging
- Log all trading decisions with reasoning
- Include key metrics (price, spread, momentum, etc.)
- Use structured logging with prefixes

## Testing Strategies

```go
func TestMyStrategy(t *testing.T) {
    mockLambda := &MockLambda{
        lastPrice: 100.0,
        tradingActive: true,
    }
    
    strategy := &MyStrategy{
        Lambda: mockLambda,
        MyParam: 1.0,
    }
    
    // Test your strategy logic
    ctx := context.Background()
    evt := &schema.Event{Type: schema.EventTypeTrade}
    payload := schema.TradePayload{Price: "100.5"}
    
    strategy.OnTrade(ctx, evt, payload, 100.5)
    
    // Assert expected behavior
}
```

## Strategy Comparison

| Strategy | Style | Complexity | Risk | Best Market |
|----------|-------|------------|------|-------------|
| NoOp | None | Low | None | Any |
| Logging | None | Low | None | Any |
| Market Making | Market Neutral | Medium | Medium | High liquidity, low volatility |
| Momentum | Directional | Medium | High | Trending markets |
| Mean Reversion | Directional | Medium | Medium | Range-bound markets |
| Grid | Market Neutral | Low | Low | Sideways markets |

## Next Steps

1. Study the demo strategies
2. Backtest strategies with historical data
3. Paper trade before live trading
4. Monitor and adjust parameters based on performance
5. Implement proper risk management
6. Add position tracking and P&L calculation
