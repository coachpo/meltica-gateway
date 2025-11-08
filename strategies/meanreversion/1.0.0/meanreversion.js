const DEFAULT_WINDOW = 20;
const DEFAULT_THRESHOLD = 0.5; // percent
const DEFAULT_ORDER_SIZE = "1";

function parseIntConfig(value, fallback) {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) {
    return fallback;
  }
  return Math.floor(num);
}

function parseFloatConfig(value, fallback) {
  const num = Number(value);
  if (!Number.isFinite(num)) {
    return fallback;
  }
  return num;
}

function selectsProvider(runtime) {
  if (!runtime) {
    return null;
  }
  const providers = typeof runtime.providers === "function" ? runtime.providers() : [];
  if (!Array.isArray(providers) || providers.length === 0) {
    return null;
  }
  const selector = typeof runtime.selectProvider === "function" ? runtime.selectProvider : null;
  if (selector) {
    try {
      const chosen = selector(Date.now());
      if (chosen) {
        return chosen;
      }
    } catch (err) {
      // ignored, fall back to first provider
    }
  }
  return providers[0];
}

function normalizePrice(value) {
  if (typeof value === "number") {
    return Number.isFinite(value) ? value : NaN;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : NaN;
}

function absolute(value) {
  return value < 0 ? -value : value;
}

module.exports = {
  metadata: {
    name: "meanreversion",
    version: "1.0.0",
    displayName: "Mean Reversion",
    description: "Trades when price deviates from its moving average.",
    config: [
      {
        name: "window_size",
        type: "int",
        description: "Moving average window size",
        default: DEFAULT_WINDOW,
        required: false
      },
      {
        name: "deviation_threshold",
        type: "float",
        description: "Deviation percentage required to open a position",
        default: DEFAULT_THRESHOLD,
        required: false
      },
      {
        name: "order_size",
        type: "string",
        description: "Order size when entering a position",
        default: DEFAULT_ORDER_SIZE,
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
      "ExecReport",
      "BalanceUpdate",
      "RiskControl"
    ]
  },
  create: function (env) {
    const config = env.config ?? {};
    const runtime = env.runtime ?? {};

    const windowSize = Math.max(2, parseIntConfig(config.window_size, DEFAULT_WINDOW));
    const threshold = parseFloatConfig(config.deviation_threshold, DEFAULT_THRESHOLD);
    const orderSize = String(config.order_size ?? DEFAULT_ORDER_SIZE).trim() || DEFAULT_ORDER_SIZE;
    const configuredDryRun = config.dry_run !== undefined ? Boolean(config.dry_run) : true;

    const log = (...parts) => env.helpers.log("[MEAN_REV]", ...parts);

    const prices = [];
    let hasPosition = false;

    function isTradingActive() {
      if (runtime && typeof runtime.isTradingActive === "function") {
        try {
          return Boolean(runtime.isTradingActive());
        } catch (err) {
          log("isTradingActive helper failed:", err?.message ?? err);
        }
      }
      return false;
    }

    function isDryRun() {
      if (configuredDryRun) {
        return true;
      }
      if (runtime && typeof runtime.isDryRun === "function") {
        try {
          return Boolean(runtime.isDryRun());
        } catch (err) {
          log("isDryRun helper failed:", err?.message ?? err);
        }
      }
      return false;
    }

    function addPrice(price) {
      prices.push(price);
      if (prices.length > windowSize) {
        prices.splice(0, prices.length - windowSize);
      }
    }

    function movingAverage() {
      if (prices.length === 0) {
        return NaN;
      }
      const total = prices.reduce((acc, value) => acc + value, 0);
      return total / prices.length;
    }

    function deviationPercent(price, average) {
      if (!Number.isFinite(price) || !Number.isFinite(average) || average === 0) {
        return 0;
      }
      return ((price - average) / average) * 100;
    }

    function submitLimitOrder(provider, side, price) {
      if (!runtime || typeof runtime.submitOrder !== "function") {
        throw new Error("submitOrder helper unavailable");
      }
      return runtime.submitOrder(provider, side, orderSize, price);
    }

    function handleSignal(side, provider, price, deviation) {
      const action = side === "buy" ? "BUY" : "SELL";
      if (isDryRun()) {
        log(
          "[DRY-RUN] Would",
          action,
          `on ${provider}: price ${price.toFixed(2)} vs MA ${movingAverage().toFixed(2)} (${deviation.toFixed(2)}%) size=${orderSize}`
        );
        hasPosition = true;
        return;
      }
      try {
        submitLimitOrder(provider, side, price);
        log(
          `${action} on ${provider}: price ${price.toFixed(2)} vs MA ${movingAverage().toFixed(2)} (${deviation.toFixed(2)}%)`
        );
        hasPosition = true;
      } catch (err) {
        log(`Failed to ${action.toLowerCase()}:`, err?.message ?? err);
      }
    }

    function currentProvider() {
      const provider = selectsProvider(runtime);
      if (!provider) {
        log("No provider available for trading");
      }
      return provider;
    }

    return {
      wantsCrossProviderEvents: function () {
        return false;
      },
      onTrade: function (_ctx, _evt, _payload, price) {
        if (!isTradingActive()) {
          return;
        }
        const numericPrice = normalizePrice(price);
        if (!Number.isFinite(numericPrice) || numericPrice <= 0) {
          return;
        }

        addPrice(numericPrice);
        if (prices.length < windowSize) {
          return;
        }

        const average = movingAverage();
        const deviation = deviationPercent(numericPrice, average);
        log(`Price: ${numericPrice.toFixed(2)} MA: ${average.toFixed(2)} Deviation: ${deviation.toFixed(2)}%`);

        if (deviation < -threshold && !hasPosition) {
          const provider = currentProvider();
          if (!provider) {
            return;
          }
          handleSignal("buy", provider, numericPrice, deviation);
          return;
        }

        if (deviation > threshold && !hasPosition) {
          const provider = currentProvider();
          if (!provider) {
            return;
          }
          handleSignal("sell", provider, numericPrice, deviation);
          return;
        }

        if (hasPosition && absolute(deviation) < threshold / 2) {
          log("Price reverted to MA, position closed");
          hasPosition = false;
        }
      },
      onOrderFilled: function (_ctx, _evt, payload) {
        log(
          "Order filled:",
          `side=${payload?.side ?? ""}`,
          `price=${payload?.avgFillPrice ?? payload?.avg_fill_price ?? ""}`
        );
      },
      onOrderRejected: function (_ctx, _evt, _payload, reason) {
        hasPosition = false;
        log("Order rejected:", reason ?? "");
      },
      onOrderPartialFill: function (_ctx, _evt, payload) {
        log("Partial fill:", payload?.filledQuantity ?? payload?.filled_quantity ?? "");
      },
      onOrderCancelled: function () {
        hasPosition = false;
        log("Order cancelled");
      },
      onBalanceUpdate: function (_ctx, _evt, payload) {
        log(
          "Balance update:",
          `currency=${payload?.currency ?? ""}`,
          `total=${payload?.total ?? ""}`,
          `available=${payload?.available ?? ""}`
        );
      },
      onRiskControl: function (_ctx, _evt, payload) {
        hasPosition = false;
        log(
          "Risk control notification:",
          `status=${payload?.status ?? ""}`,
          `breach=${payload?.breachType ?? payload?.breach_type ?? ""}`,
          `reason=${payload?.reason ?? ""}`
        );
      },
      onTicker: function () {},
      onBookSnapshot: function () {},
      onOrderAcknowledged: function () {},
      onOrderExpired: function () {},
      onKlineSummary: function () {},
      onInstrumentUpdate: function () {}
    };
  }
};
