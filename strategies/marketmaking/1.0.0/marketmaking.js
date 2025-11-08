const DEFAULT_SPREAD_BPS = 25;
const DEFAULT_ORDER_SIZE = "1";
const DEFAULT_MAX_OPEN = 2;

function parseFloatConfig(value, fallback) {
  const num = Number(value);
  if (!Number.isFinite(num)) {
    return fallback;
  }
  return num;
}

function parseIntConfig(value, fallback) {
  const num = Number(value);
  if (!Number.isFinite(num) || num < 0) {
    return fallback;
  }
  return Math.floor(num);
}

function selectProvider(runtime) {
  if (!runtime) {
    return null;
  }
  const providers = typeof runtime.providers === 'function' ? runtime.providers() : [];
  if (!Array.isArray(providers) || providers.length === 0) {
    return null;
  }
  const selector = typeof runtime.selectProvider === 'function' ? runtime.selectProvider : null;
  if (selector) {
    try {
      const choice = selector(Date.now());
      if (choice) {
        return choice;
      }
    } catch (err) {
      // fall back below
    }
  }
  return providers[0];
}

function toNumber(value) {
  if (typeof value === 'number') {
    return Number.isFinite(value) ? value : NaN;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : NaN;
}

module.exports = {
  metadata: {
    name: 'marketmaking',
    version: '1.0.0',
    displayName: 'Market Making',
    description: 'Quotes bid/ask orders around the mid price to capture spread.',
    config: [
      {
        name: 'spread_bps',
        type: 'float',
        description: 'Spread in basis points',
        default: DEFAULT_SPREAD_BPS,
        required: false,
      },
      {
        name: 'order_size',
        type: 'string',
        description: 'Quoted order size',
        default: DEFAULT_ORDER_SIZE,
        required: false,
      },
      {
        name: 'max_open_orders',
        type: 'int',
        description: 'Maximum concurrent orders per side',
        default: DEFAULT_MAX_OPEN,
        required: false,
      },
      {
        name: 'dry_run',
        type: 'bool',
        description: 'When true, strategy logs intended orders without submitting them',
        default: true,
        required: false,
      },
    ],
    events: ['Trade', 'Ticker', 'ExecReport', 'BalanceUpdate', 'RiskControl'],
  },
  create: function (env) {
    const config = env.config ?? {};
    const runtime = env.runtime ?? {};
    const log = (...parts) => env.helpers.log('[MM]', ...parts);

    const spreadBps = parseFloatConfig(config.spread_bps, DEFAULT_SPREAD_BPS);
    const orderSize = String(config.order_size ?? DEFAULT_ORDER_SIZE).trim() || DEFAULT_ORDER_SIZE;
    const maxOpen = Math.max(0, parseIntConfig(config.max_open_orders, DEFAULT_MAX_OPEN));
    const configuredDryRun = config.dry_run !== undefined ? Boolean(config.dry_run) : true;

    let activeBuy = 0;
    let activeSell = 0;
    let lastQuotePrice = 0;

    function isTradingActive() {
      if (runtime && typeof runtime.isTradingActive === 'function') {
        try {
          return Boolean(runtime.isTradingActive());
        } catch (err) {
          log('isTradingActive helper failed:', err?.message ?? err);
        }
      }
      return false;
    }

    function isDryRun() {
      if (configuredDryRun) {
        return true;
      }
      if (runtime && typeof runtime.isDryRun === 'function') {
        try {
          return Boolean(runtime.isDryRun());
        } catch (err) {
          log('isDryRun helper failed:', err?.message ?? err);
        }
      }
      return false;
    }

    function marketState() {
      if (runtime && typeof runtime.getMarketState === 'function') {
        try {
          return runtime.getMarketState();
        } catch (err) {
          log('getMarketState helper failed:', err?.message ?? err);
        }
      }
      return {
        bid: runtime && typeof runtime.getBidPrice === 'function' ? runtime.getBidPrice() : NaN,
        ask: runtime && typeof runtime.getAskPrice === 'function' ? runtime.getAskPrice() : NaN,
      };
    }

    function submitLimit(provider, side, price) {
      if (isDryRun()) {
        if (side === 'buy') {
          activeBuy += 1;
        } else {
          activeSell += 1;
        }
        log(`[DRY-RUN] Would place ${side.toUpperCase()} order on ${provider}: price=${price.toFixed(2)} size=${orderSize}`);
        return;
      }
      if (!runtime || typeof runtime.submitOrder !== 'function') {
        throw new Error('submitOrder helper unavailable');
      }
      runtime.submitOrder(provider, side, orderSize, price);
      if (side === 'buy') {
        activeBuy += 1;
      } else {
        activeSell += 1;
      }
      log(`Placed ${side.toUpperCase()} order on ${provider}: price=${price.toFixed(2)} size=${orderSize}`);
    }

    function placeQuotes(state) {
      const provider = selectProvider(runtime);
      if (!provider) {
        log('Unable to select provider');
        return;
      }
      const bid = toNumber(state?.bid);
      const ask = toNumber(state?.ask);
      if (!Number.isFinite(bid) || !Number.isFinite(ask) || bid <= 0 || ask <= 0) {
        return;
      }
      const mid = (bid + ask) / 2;
      if (!Number.isFinite(mid) || mid <= 0) {
        return;
      }

      const spreadMultiplier = spreadBps / 10000;
      const buyPrice = mid * (1 - spreadMultiplier);
      const sellPrice = mid * (1 + spreadMultiplier);

      if (activeBuy < maxOpen) {
        submitLimit(provider, 'buy', buyPrice);
      }
      if (activeSell < maxOpen) {
        submitLimit(provider, 'sell', sellPrice);
      }

      lastQuotePrice = mid;
    }

    function decrement(side) {
      if (side === 'buy') {
        activeBuy = Math.max(0, activeBuy - 1);
      } else if (side === 'sell') {
        activeSell = Math.max(0, activeSell - 1);
      }
    }

    return {
      wantsCrossProviderEvents: function () {
        return false;
      },
      onTrade: function (ctx, _evt, _payload, price) {
        if (!isTradingActive()) {
          return;
        }
        const numericPrice = toNumber(price);
        if (!Number.isFinite(numericPrice) || numericPrice <= 0) {
          return;
        }
        const state = marketState();
        if (!Number.isFinite(state?.bid) || !Number.isFinite(state?.ask)) {
          return;
        }
        if (lastQuotePrice === 0) {
          placeQuotes(state);
          return;
        }
        const priceMoveBps = Math.abs(numericPrice - lastQuotePrice) / lastQuotePrice * 10000;
        if (priceMoveBps > spreadBps / 2) {
          log(`Price moved ${priceMoveBps.toFixed(2)} bps, requoting`);
          placeQuotes(state);
        }
      },
      onTicker: function () {
        if (!isTradingActive()) {
          return;
        }
        const state = marketState();
        if (!Number.isFinite(state?.bid) || !Number.isFinite(state?.ask)) {
          return;
        }
        placeQuotes(state);
      },
      onOrderFilled: function (_ctx, _evt, payload) {
        decrement((payload?.side ?? payload?.Side ?? '').toLowerCase());
        log(
          'Order filled:',
          `side=${payload?.side ?? payload?.Side ?? ''}`,
          `price=${payload?.avgFillPrice ?? payload?.avg_fill_price ?? ''}`,
          `qty=${payload?.filledQuantity ?? payload?.filled_quantity ?? ''}`
        );
      },
      onOrderRejected: function (_ctx, _evt, payload, reason) {
        decrement((payload?.side ?? payload?.Side ?? '').toLowerCase());
        log('Order rejected:', reason ?? '');
      },
      onOrderPartialFill: function (_ctx, _evt, payload) {
        log(
          'Partial fill:',
          `side=${payload?.side ?? payload?.Side ?? ''}`,
          `filled=${payload?.filledQuantity ?? payload?.filled_quantity ?? ''}`,
          `remaining=${payload?.remainingQty ?? payload?.remaining_qty ?? ''}`
        );
      },
      onOrderCancelled: function (_ctx, _evt, payload) {
        decrement((payload?.side ?? payload?.Side ?? '').toLowerCase());
        log('Order cancelled:', payload?.side ?? payload?.Side ?? '');
      },
      onBalanceUpdate: function (_ctx, _evt, payload) {
        log(
          'Balance update:',
          `currency=${payload?.currency ?? ''}`,
          `total=${payload?.total ?? ''}`,
          `available=${payload?.available ?? ''}`
        );
      },
      onRiskControl: function (_ctx, _evt, payload) {
        activeBuy = 0;
        activeSell = 0;
        lastQuotePrice = 0;
        log(
          'Risk control notification:',
          `status=${payload?.status ?? ''}`,
          `breach=${payload?.breachType ?? payload?.breach_type ?? ''}`,
          `reason=${payload?.reason ?? ''}`
        );
      },
      onBookSnapshot: function () {},
      onOrderAcknowledged: function () {},
      onOrderExpired: function () {},
      onKlineSummary: function () {},
      onInstrumentUpdate: function () {},
    };
  },
};
