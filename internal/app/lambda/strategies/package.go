// Package strategies contains trading strategy implementations for the Lambda framework.
//
// Available Strategies:
// - NoOp: Monitoring only, no trading
// - Logging: Debug logging for all events
// - MarketMaking: Places quotes around mid-price
// - Momentum: Trades based on price momentum
// - MeanReversion: Trades when price deviates from MA
// - Grid: Places orders at regular intervals
package strategies
