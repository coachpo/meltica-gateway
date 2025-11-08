const DEFAULT_GRID_LEVELS = 3;
const DEFAULT_GRID_SPACING = 0.5; // percent
const DEFAULT_ORDER_SIZE = "1";
const DEFAULT_BASE_PRICE = 0;

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

function selectProvider(runtime) {
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
      const choice = selector(Date.now());
      if (choice) {
        return choice;
      }
    } catch (err) {
      // fall back to first provider below
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

module.exports = {
  metadata: {
    name: "grid",
    tag: "1.0.0",
    displayName: "Grid",
    description: "Places a symmetric buy/sell grid around the reference price.",
    config: [
      {
        name: "grid_levels",
        type: "int",
        description: "Number of grid levels on each side",
        default: DEFAULT_GRID_LEVELS,
        required: false
      },
      {
        name: "grid_spacing",
        type: "float",
        description: "Grid spacing expressed as percent",
        default: DEFAULT_GRID_SPACING,
        required: false
      },
      {
        name: "order_size",
        type: "string",
        description: "Order size per level",
        default: DEFAULT_ORDER_SIZE,
        required: false
      },
      {
        name: "base_price",
        type: "float",
        description: "Optional base price for the grid",
        default: DEFAULT_BASE_PRICE,
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
    const log = (...parts) => env.helpers.log("[GRID]", ...parts);

    const gridLevels = Math.max(1, parseIntConfig(config.grid_levels, DEFAULT_GRID_LEVELS));
    const gridSpacingPct = parseFloatConfig(config.grid_spacing, DEFAULT_GRID_SPACING);
    const orderSize = String(config.order_size ?? DEFAULT_ORDER_SIZE).trim() || DEFAULT_ORDER_SIZE;
    let basePrice = parseFloatConfig(config.base_price, DEFAULT_BASE_PRICE);
    const configuredDryRun = config.dry_run !== undefined ? Boolean(config.dry_run) : true;

    const activeGrids = new Map();
    let initialized = false;

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

    function placeOrder(provider, side, price) {
      if (isDryRun()) {
        activeGrids.set(price, true);
        log(`[DRY-RUN] Would place ${side.toUpperCase()} order at ${price.toFixed(2)} on ${provider}`);
        return;
      }
      if (!runtime || typeof runtime.submitOrder !== "function") {
        throw new Error("submitOrder helper unavailable");
      }
      try {
        runtime.submitOrder(provider, side, orderSize, price);
        activeGrids.set(price, true);
        log(`Placed ${side.toUpperCase()} order at ${price.toFixed(2)} on ${provider}`);
      } catch (err) {
        log(`Failed to place ${side} order at ${price.toFixed(2)}:`, err?.message ?? err);
      }
    }

    function placeGridOrders(ctx) {
      const spacingMultiplier = gridSpacingPct / 100;
      for (let i = 1; i <= gridLevels; i += 1) {
        const buyPrice = basePrice * (1 - spacingMultiplier * i);
        const providerBuy = selectProvider(runtime);
        if (!providerBuy) {
          log("Unable to select provider for buy order");
        } else {
          placeOrder(providerBuy, "buy", buyPrice);
        }

        const sellPrice = basePrice * (1 + spacingMultiplier * i);
        const providerSell = selectProvider(runtime);
        if (!providerSell) {
          log("Unable to select provider for sell order");
        } else {
          placeOrder(providerSell, "sell", sellPrice);
        }
      }
    }

    function handleInitialGrid(ctx, price) {
      if (basePrice <= 0) {
        basePrice = price;
        log(`Base price set to ${basePrice.toFixed(2)}`);
      }
      if (!initialized) {
        activeGrids.clear();
        placeGridOrders(ctx);
        initialized = true;
      }
    }

    function handleOppositeOrder(fillPrice, side, provider) {
      const opposite = side === "sell" ? "buy" : "sell";
      const selectedProvider = provider || selectProvider(runtime);
      if (!selectedProvider) {
        log("Failed to select provider for opposite order");
        return;
      }
      placeOrder(selectedProvider, opposite, fillPrice);
    }

    function removeGrid(price) {
      activeGrids.delete(price);
    }

    return {
      wantsCrossProviderEvents: function () {
        return true;
      },
      onTrade: function (ctx, _evt, _payload, price) {
        if (!isTradingActive()) {
          return;
        }
        const numericPrice = normalizePrice(price);
        if (!Number.isFinite(numericPrice) || numericPrice <= 0) {
          return;
        }
        handleInitialGrid(ctx, numericPrice);
      },
      onOrderFilled: function (_ctx, evt, payload) {
        const priceStr = payload?.avgFillPrice ?? payload?.avg_fill_price;
        const fillPrice = normalizePrice(priceStr);
        if (!Number.isFinite(fillPrice)) {
          log("Failed to parse fill price", priceStr ?? "");
          return;
        }
        removeGrid(fillPrice);
        const provider = evt?.provider ?? evt?.Provider ?? "";
        handleOppositeOrder(fillPrice, (payload?.side ?? payload?.Side ?? "").toLowerCase(), provider);
        log(
          "Filled order:",
          `side=${payload?.side ?? payload?.Side ?? ""}`,
          `price=${priceStr ?? payload?.avg_fill_price ?? ""}`,
          `provider=${provider}`
        );
      },
      onOrderRejected: function (_ctx, _evt, _payload, reason) {
        log("Order rejected:", reason ?? "");
      },
      onOrderPartialFill: function (_ctx, _evt, payload) {
        log("Partial fill:", payload?.filledQuantity ?? payload?.filled_quantity ?? "");
      },
      onOrderCancelled: function () {
        log("Order cancelled");
      },
      onBalanceUpdate: function (_ctx, _evt, payload) {
        log(
          "Balance update:",
          `currency=${payload?.currency ?? ""}`,
          `available=${payload?.available ?? ""}`,
          `total=${payload?.total ?? ""}`
        );
      },
      onRiskControl: function (_ctx, _evt, payload) {
        log(
          "Risk control trigger:",
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
