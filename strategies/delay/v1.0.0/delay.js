const DEFAULT_MIN_DELAY = "100ms";
const DEFAULT_MAX_DELAY = "500ms";

function parseDuration(value, fallback) {
  if (value === undefined || value === null || value === "") {
    return fallback;
  }
  const text = String(value).trim().toLowerCase();
  if (text === "") {
    return fallback;
  }
  const match = text.match(/^(-?\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)?$/);
  if (!match) {
    return fallback;
  }
  const magnitude = Number(match[1]);
  if (!Number.isFinite(magnitude) || magnitude < 0) {
    return fallback;
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
      return fallback;
  }
}

function clampDurations(minMs, maxMs) {
  let min = Math.max(0, minMs);
  let max = Math.max(0, maxMs);
  if (max < min) {
    max = min;
  }
  return { min, max };
}

function randomDelay(minMs, maxMs) {
  if (maxMs <= minMs) {
    return minMs;
  }
  const offset = Math.random() * (maxMs - minMs);
  return minMs + offset;
}

module.exports = {
  metadata: {
    name: "delay",
    version: "1.0.0",
    displayName: "Delay",
    description: "Simulates processing latency with a configurable random delay window.",
    config: [
      {
        name: "min_delay",
        type: "duration",
        description: "Lower bound for the random delay interval",
        default: DEFAULT_MIN_DELAY,
        required: false
      },
      {
        name: "max_delay",
        type: "duration",
        description: "Upper bound for the random delay interval",
        default: DEFAULT_MAX_DELAY,
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
      "KlineSummary",
      "BalanceUpdate",
      "RiskControl",
      "InstrumentUpdate",
      "ExecReport"
    ]
  },
  create: function (env) {
    const minConfig = parseDuration(env.config.min_delay ?? DEFAULT_MIN_DELAY, 100);
    const maxConfig = parseDuration(env.config.max_delay ?? DEFAULT_MAX_DELAY, 500);
    const { min, max } = clampDurations(minConfig, maxConfig);

    function sleepRandom() {
      const durationMs = randomDelay(min, max);
      env.helpers.sleep(durationMs);
    }

    return {
      wantsCrossProviderEvents: function () {
        return true;
      },
      onTrade: function () {
        sleepRandom();
      },
      onTicker: function () {
        sleepRandom();
      },
      onBookSnapshot: function () {
        sleepRandom();
      },
      onOrderFilled: function () {
        sleepRandom();
      },
      onOrderRejected: function () {
        sleepRandom();
      },
      onOrderPartialFill: function () {
        sleepRandom();
      },
      onOrderCancelled: function () {
        sleepRandom();
      },
      onOrderAcknowledged: function () {
        sleepRandom();
      },
      onOrderExpired: function () {
        sleepRandom();
      },
      onKlineSummary: function () {
        sleepRandom();
      },
      onInstrumentUpdate: function () {
        sleepRandom();
      },
      onBalanceUpdate: function () {
        sleepRandom();
      },
      onRiskControl: function () {
        sleepRandom();
      }
    };
  }
};
