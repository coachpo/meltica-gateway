module.exports = {
  metadata: {
    name: "logging",
    tag: "1.0.0",
    displayName: "Logging",
    description: "Emits detailed logs for all inbound events.",
    config: [
      {
        name: "logger_prefix",
        type: "string",
        description: "Prefix prepended to each log message",
        default: "[Logging] ",
        required: false
      },
      {
        name: "dry_run",
        type: "bool",
        description: "When true, strategy logs intended orders without submitting them",
        default: true,
        required: false
      }
    ],
    events: [
      "Trade",
      "Ticker",
      "BookSnapshot",
      "ExecReport",
      "KlineSummary",
      "BalanceUpdate",
      "RiskControl",
      "InstrumentUpdate"
    ]
  },
  create: function (env) {
    const prefixSource = (env.config.logger_prefix ?? "[Logging] ").trim();
    const prefix = prefixSource.length > 0 ? prefixSource : "[Logging] ";
    const log = (...parts) => env.helpers.log(`${prefix}${parts.join(" ")}`);

    function formatPrice(price) {
      if (price === undefined || price === null) {
        return "";
      }
      if (typeof price === "number" && Number.isFinite(price)) {
        return price.toFixed(2);
      }
      return String(price);
    }

    function summarizeOrders(orders, side) {
      if (!Array.isArray(orders) || orders.length === 0) {
        return;
      }
      const limit = Math.min(orders.length, 5);
      for (let i = 0; i < limit; i += 1) {
        const level = orders[i] || {};
        log(`${side}[${i}]:`, `${level.quantity ?? level.qty ?? ""} @ ${level.price ?? ""}`);
      }
    }

    return {
      wantsCrossProviderEvents: function () {
        return true;
      },
      onTrade: function (_ctx, evt, _payload, price) {
        log("========== JavaScript version ==========");
        log(
          "Trade received:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `price=${formatPrice(price)}`
        );
      },
      onTicker: function (_ctx, evt, payload) {
        log("========== JavaScript version ==========");
        log(
          "Ticker:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `last=${payload?.lastPrice ?? ""}`,
          `bid=${payload?.bidPrice ?? ""}`,
          `ask=${payload?.askPrice ?? ""}`
        );
      },
      onBookSnapshot: function (_ctx, evt, payload) {
        log("========== JavaScript version ==========");
        const bids = payload?.bids ?? payload?.Bids ?? [];
        const asks = payload?.asks ?? payload?.Asks ?? [];
        log(
          "Book snapshot:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `${bids.length} bids, ${asks.length} asks`
        );
        summarizeOrders(bids, "BID");
        summarizeOrders(asks, "ASK");
      },
      onOrderFilled: function (_ctx, evt, payload) {
        log(
          "Order filled:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `id=${payload?.clientOrderId ?? ""}`,
          `qty=${payload?.filledQuantity ?? ""}`,
          `price=${payload?.avgFillPrice ?? ""}`
        );
      },
      onOrderRejected: function (_ctx, evt, payload, reason) {
        log(
          "Order rejected:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `id=${payload?.clientOrderId ?? ""}`,
          `reason=${reason ?? payload?.rejectReason ?? ""}`
        );
      },
      onOrderPartialFill: function (_ctx, evt, payload) {
        log(
          "Order partial fill:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `id=${payload?.clientOrderId ?? ""}`,
          `filled=${payload?.filledQuantity ?? ""}`,
          `remaining=${payload?.remainingQty ?? ""}`
        );
      },
      onOrderCancelled: function (_ctx, evt, payload) {
        log(
          "Order cancelled:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `id=${payload?.clientOrderId ?? ""}`
        );
      },
      onOrderAcknowledged: function (_ctx, evt, payload) {
        log(
          "Order acknowledged:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `id=${payload?.clientOrderId ?? ""}`
        );
      },
      onOrderExpired: function (_ctx, evt, payload) {
        log(
          "Order expired:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `id=${payload?.clientOrderId ?? ""}`
        );
      },
      onKlineSummary: function (_ctx, evt, payload) {
        log(
          "Kline:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${evt?.symbol ?? ""}`,
          `open=${payload?.openPrice ?? ""}`,
          `close=${payload?.closePrice ?? ""}`,
          `high=${payload?.highPrice ?? ""}`,
          `low=${payload?.lowPrice ?? ""}`,
          `vol=${payload?.volume ?? ""}`
        );
      },
      onInstrumentUpdate: function (_ctx, evt, payload) {
        const instrument = payload?.instrument ?? payload?.Instrument ?? {};
        log(
          "Instrument updated:",
          `provider=${evt?.provider ?? ""}`,
          `symbol=${instrument.symbol ?? instrument.Symbol ?? ""}`
        );
      },
      onBalanceUpdate: function (_ctx, evt, payload) {
        log(
          "Balance update:",
          `provider=${evt?.provider ?? ""}`,
          `currency=${payload?.currency ?? ""}`,
          `total=${payload?.total ?? ""}`,
          `available=${payload?.available ?? ""}`
        );
      },
      onRiskControl: function (_ctx, _evt, payload) {
        log(
          "Risk control:",
          `strategy=${payload?.strategyID ?? payload?.strategyId ?? ""}`,
          `status=${payload?.status ?? ""}`,
          `breach=${payload?.breachType ?? payload?.breach_type ?? ""}`,
          `reason=${payload?.reason ?? ""}`,
          `metrics=${JSON.stringify(payload?.metrics ?? {})}`,
          `killSwitch=${payload?.killSwitchEngaged ?? payload?.kill_switch_engaged ?? false}`,
          `circuitBreaker=${payload?.circuitBreakerOpen ?? payload?.circuit_breaker_open ?? false}`
        );
      }
    };
  }
};
