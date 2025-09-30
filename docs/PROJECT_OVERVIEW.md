# Project Overview

Meltica is a high-performance cryptocurrency exchange adapter framework that provides a unified interface for trading across multiple exchanges.

## Architecture

See [architecture.md](architecture.md) for the complete four-layer architecture (Connection → Routing → Business → Filter).

## Supported Exchanges
- **Binance**: Full implementation with spot, linear futures, and inverse futures support

## Key Design Principles

1. **Unified Interface**: Single API for all exchanges
2. **Type Safety**: Strongly typed interfaces prevent runtime errors
3. **Performance**: Optimized for low-latency trading operations
4. **Extensibility**: Easy to add new exchanges following established patterns
5. **Reliability**: Comprehensive error handling and recovery mechanisms 
