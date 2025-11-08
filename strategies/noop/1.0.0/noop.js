module.exports = {
  metadata: {
    name: "noop",
    tag: "1.0.0",
    displayName: "No-Op",
    description: "Pass-through strategy that performs no actions.",
    config: [
      {
        name: "dry_run",
        type: "bool",
        description: "When true, strategy logs intended orders without submitting them",
        default: true,
        required: false
      }
    ],
    events: ["Trade", "Ticker", "BookSnapshot", "BalanceUpdate", "RiskControl"]
  },
  create: function () {
    function noop() {}

    return {
      wantsCrossProviderEvents: function () {
        return true;
      },
      onTrade: noop,
      onTicker: noop,
      onBookSnapshot: noop,
      onInstrumentUpdate: noop,
      onBalanceUpdate: noop,
      onOrderFilled: noop,
      onOrderRejected: noop,
      onOrderPartialFill: noop,
      onOrderCancelled: noop,
      onOrderAcknowledged: noop,
      onOrderExpired: noop,
      onKlineSummary: noop,
      onRiskControl: noop
    };
  }
};
