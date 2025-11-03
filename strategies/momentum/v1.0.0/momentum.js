const DEFAULT_LOOKBACK = 20;
const DEFAULT_THRESHOLD = 0.5; // percent
const DEFAULT_ORDER_SIZE = "1";
const DEFAULT_COOLDOWN = "5s";

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

function parseDurationMs(value, fallbackMs) {
  if (value === undefined || value === null) {
    return fallbackMs;
  }
  const text = String(value).trim().toLowerCase();
  if (text.length === 0) {
    return fallbackMs;
  }
  const match = text.match(/^(-?\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)?$/);
  if (!match) {
    return fallbackMs;
  }
  const magnitude = Number(match[1]);
  if (!Number.isFinite(magnitude) || magnitude < 0) {
    return fallbackMs;
  }
  const unit = match[2] || "ms";
  switch (unit) {
    case "ns":
      return magnitude / 1_000_000;
    case "us":
    case "µs":
      return magnitude / 1_000;
    case "ms":
      return magnitude;
    case "s":
      return magnitude * 1_000;
    case "m":
      return magnitude * 60_000;
    case "h":
      return magnitude * 3_600_000;
    default:
      return fallbackMs;
  }
}

function selectProvider(runtime) {
  const providersFn = runtime && typeof runtime.providers === "function" ? runtime.providers : null;
  const providers = providersFn ? providersFn() : [];
  if (providers.length === 0) {
    return null;
  }
  const selectFn = runtime && typeof runtime.selectProvider === "function" ? runtime.selectProvider : null;
  if (selectFn) {
    try {
      const choice = selectFn(Date.now());
      if (choice) {
        return choice;
      }
    } catch (err) {
      // fall back below on failure
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

function strategyPosition(initial = 0) {
  let pos = initial;
  return {
    get() {
      return pos;
    },
    set(value) {
      pos = value;
    },
    reset() {
      pos = 0;
    }
  };
}

module.exports = {
  metadata: {
    name: "momentum",
    version: "1.0.0",
    displayName: "Momentum",
    description: "Trades in the direction of recent price momentum.",
    config: [
      {
        name: "lookback_period",
        type: "int",
        description: "Number of recent trades used to compute momentum",
        default: DEFAULT_LOOKBACK,
        required: false
      },
      {
        name: "momentum_threshold",
        type: "float",
        description: "Minimum momentum (in percent) required to trigger trades",
        default: DEFAULT_THRESHOLD,
        required: false
      },
      {
        name: "order_size",
        type: "string",
        description: "Quantity for each market order",
        default: DEFAULT_ORDER_SIZE,
        required: false
      },
      {
        name: "cooldown",
        type: "duration",
        description: "Minimum time between trades",
        default: DEFAULT_COOLDOWN,
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

    const lookback = Math.max(2, parseIntConfig(config.lookback_period, DEFAULT_LOOKBACK));
    const threshold = parseFloatConfig(config.momentum_threshold, DEFAULT_THRESHOLD);
    const orderSize = String(config.order_size ?? DEFAULT_ORDER_SIZE).trim() || DEFAULT_ORDER_SIZE;
    const cooldownMs = parseDurationMs(config.cooldown ?? DEFAULT_COOLDOWN, 5_000);
    const configuredDryRun = config.dry_run !== undefined ? Boolean(config.dry_run) : true;

    const log = (...parts) => env.helpers.log("[MOMENTUM]", ...parts);

    const history = [];
    const position = strategyPosition(0);
    let lastTradeAt = 0;

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

    function momentumPercent() {
      if (history.length < 2) {
        return 0;
      }
      const first = history[0];
      const last = history[history.length - 1];
      if (!first || !last || !Number.isFinite(first.price) || first.price === 0) {
        return 0;
      }
      const change = ((last.price - first.price) / first.price) * 100;
      return change;
    }

    function addPrice(price) {
      history.push({ price, timestamp: Date.now() });
      if (history.length > lookback) {
        history.splice(0, history.length - lookback);
      }
    }

    function canTrade() {
      const now = Date.now();
      if (now - lastTradeAt < cooldownMs) {
        return false;
      }
      return true;
    }

    function submitMarketOrder(provider, side, size) {
      if (!runtime || typeof runtime.submitMarketOrder !== "function") {
        throw new Error("submitMarketOrder helper unavailable");
      }
      return runtime.submitMarketOrder(provider, side, size);
    }

    function executeTrade(side, momentumPct) {
      const provider = selectProvider(runtime);
      if (!provider) {
        log("No provider available for", side.toUpperCase());
        return;
      }
      if (isDryRun()) {
        log(`[DRY-RUN] Would ${side.toUpperCase()} on ${provider}: momentum=${momentumPct.toFixed(3)}% size=${orderSize}`);
        position.set(side === "buy" ? 1 : -1);
        lastTradeAt = Date.now();
        return;
      }
      try {
        submitMarketOrder(provider, side, orderSize);
        log(`${side.toUpperCase()} signal on ${provider}: momentum=${momentumPct.toFixed(3)}%`);
        position.set(side === "buy" ? 1 : -1);
        lastTradeAt = Date.now();
      } catch (err) {
        log(`Failed to ${side}:`, err?.message ?? err);
      }
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
        if (history.length < lookback) {
          return;
        }
        if (!canTrade()) {
          return;
        }

        const momentumPct = momentumPercent();
        log(`Current momentum: ${momentumPct.toFixed(3)}%`);

        if (momentumPct > threshold && position.get() <= 0) {
          executeTrade("buy", momentumPct);
        } else if (momentumPct < -threshold && position.get() >= 0) {
          executeTrade("sell", momentumPct);
        }
      },
      onOrderFilled: function (_ctx, _evt, payload) {
        log(
          "Order filled:",
          `side=${payload?.side ?? ""}`,
          `price=${payload?.avgFillPrice ?? payload?.avg_fill_price ?? ""}`,
          `qty=${payload?.filledQuantity ?? payload?.filled_quantity ?? ""}`
        );
      },
      onOrderRejected: function (_ctx, _evt, payload, reason) {
        position.reset();
        log(
          "Order rejected:",
          `side=${payload?.side ?? ""}`,
          `reason=${reason ?? payload?.rejectReason ?? ""}`
        );
      },
      onOrderPartialFill: function (_ctx, _evt, payload) {
        log(
          "Partial fill:",
          `side=${payload?.side ?? ""}`,
          `filled=${payload?.filledQuantity ?? payload?.filled_quantity ?? ""}`
        );
      },
      onOrderCancelled: function (_ctx, _evt, payload) {
        log("Order cancelled:", `side=${payload?.side ?? ""}`);
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
        position.reset();
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
