module.exports = {
  metadata: {
    name: "extension-listener",
    tag: "1.0.0",
    displayName: "Extension Listener",
    description: "Logs custom Extension events and echoes payload metadata.",
    config: [
      {
        name: "log_prefix",
        type: "string",
        description: "Optional prefix added to every log entry.",
        default: "[ExtensionListener] ",
        required: false
      }
    ],
    events: ["Extension"]
  },
  create: function (env) {
    const prefixSource = (env?.config?.log_prefix ?? "[ExtensionListener] ").trim();
    const prefix = prefixSource.length > 0 ? prefixSource : "[ExtensionListener] ";
    const log = (...parts) => {
      const message = `${prefix}${parts.join(" ")}`;
      if (env?.helpers?.log) {
        env.helpers.log(message);
      } else {
        // eslint-disable-next-line no-console
        console.log(message);
      }
    };

    function noop() {}

    return {
      wantsCrossProviderEvents: function () {
        return true;
      },
      onExtensionEvent: function (_ctx, evt, payload) {
        const provider = evt?.provider ?? "unknown";
        const symbol = evt?.symbol ?? "";
        const data = payload ?? {};
        log(
          "extension payload",
          `provider=${provider}`,
          symbol ? `symbol=${symbol}` : "",
          `payload=${JSON.stringify(data)}`
        );
      },
      onTrade: noop,
      onTicker: noop,
      onBookSnapshot: noop,
      onKlineSummary: noop,
      onInstrumentUpdate: noop,
      onBalanceUpdate: noop,
      onOrderFilled: noop,
      onOrderRejected: noop,
      onOrderPartialFill: noop,
      onOrderCancelled: noop,
      onOrderAcknowledged: noop,
      onOrderExpired: noop,
      onRiskControl: noop
    };
  }
};
