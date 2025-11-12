// Package strategiestest provides helper utilities for tests that need stub strategy modules.
package strategiestest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	json "github.com/goccy/go-json"
)

const (
	noopModuleSource = `module.exports = {
	  metadata: {
	    name: "noop",
	    tag: "v1.0.0",
	    displayName: "No-Op",
	    description: "No-op strategy for tests",
	    config: [
	      { name: "dry_run", type: "bool", required: false, "default": true }
	    ],
	    events: [
	      "Trade",
	      "Ticker",
	      "ExecReport",
	      "KlineSummary",
	      "InstrumentUpdate",
	      "BalanceUpdate",
	      "BookSnapshot",
	      "RiskControl",
	      "Extension"
	    ]
	  },
	  create: function (env) {
	    var events = (env && env.metadata && env.metadata.events) || [];
	    function noop() {}
	    return {
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
	      onRiskControl: noop,
	      onExtensionEvent: noop,
	      subscribedEvents: function () {
	        return events.slice ? events.slice() : [];
	      },
	      wantsCrossProviderEvents: function () { return false; }
	    };
	  }
	};
	`

	loggingModuleSource = `module.exports = {
	  metadata: {
	    name: "logging",
	    tag: "v1.0.0",
	    displayName: "Logging",
	    description: "Logs received callbacks for diagnostics",
	    config: [
	      { name: "logger_prefix", type: "string", required: false, "default": "" },
	      { name: "dry_run", type: "bool", required: false, "default": true }
	    ],
	    events: [
	      "Trade",
	      "Ticker",
	      "ExecReport",
	      "KlineSummary",
	      "InstrumentUpdate",
	      "BalanceUpdate",
	      "BookSnapshot",
	      "RiskControl",
	      "Extension"
	    ]
	  },
	  create: function (env) {
	    var logHelper = (env && env.helpers && typeof env.helpers.log === "function") ? env.helpers.log : null;
	    var prefix = (env && env.config && env.config.logger_prefix) || "";
	    var events = (env && env.metadata && env.metadata.events) || [];

	    function emit(eventName, payload) {
	      if (!logHelper) {
	        return;
	      }
	      var text;
	      if (payload && typeof payload === "object") {
	        try {
	          text = JSON.stringify(payload);
	        } catch (err) {
	          text = "[object]";
	        }
	      } else {
	        text = String(payload || "");
	      }
	      if (prefix) {
	        logHelper(prefix + " " + eventName + " " + text);
	      } else {
	        logHelper(eventName + " " + text);
	      }
	    }

	    function wrap(eventName) {
	      return function (_ctx, _evt, payload) {
	        emit(eventName, payload || {});
	      };
	    }

	    return {
	      onTrade: wrap("trade"),
	      onTicker: wrap("ticker"),
	      onBookSnapshot: wrap("book"),
	      onKlineSummary: wrap("kline"),
	      onInstrumentUpdate: wrap("instrument"),
	      onBalanceUpdate: wrap("balance"),
	      onOrderFilled: wrap("orderFilled"),
	      onOrderRejected: function (_ctx, _evt, payload, reason) {
	        emit("orderRejected", { payload: payload || {}, reason: reason || "" });
	      },
	      onOrderPartialFill: wrap("orderPartial"),
	      onOrderCancelled: wrap("orderCancelled"),
	      onOrderAcknowledged: wrap("orderAck"),
	      onOrderExpired: wrap("orderExpired"),
	      onRiskControl: wrap("risk"),
	      onExtensionEvent: wrap("extension"),
	      subscribedEvents: function () {
	        return events.slice ? events.slice() : [];
	      },
	      wantsCrossProviderEvents: function () { return false; }
	    };
	  }
	};
	`

	delayModuleSource = `module.exports = {
	  metadata: {
	    name: "delay",
	    tag: "v1.0.0",
	    displayName: "Delay",
	    description: "Applies a synthetic delay to callbacks for testing",
	    config: [
	      { name: "delay_ms", type: "number", required: false, "default": 0 },
	      { name: "dry_run", type: "bool", required: false, "default": true }
	    ],
	    events: [
	      "Trade",
	      "Ticker",
	      "ExecReport",
	      "KlineSummary",
	      "InstrumentUpdate",
	      "BalanceUpdate",
	      "BookSnapshot",
	      "RiskControl",
	      "Extension"
	    ]
	  },
	  create: function (env) {
	    var delayMs = Number((env && env.config && env.config.delay_ms) || 0);
	    if (!delayMs || delayMs < 0) {
	      delayMs = 0;
	    }
	    var events = (env && env.metadata && env.metadata.events) || [];

	    function invoke() {
	      if (delayMs <= 0) {
	        return;
	      }
	      var start = Date.now();
	      while (Date.now() - start < delayMs) {}
	    }

	    return {
	      onTrade: invoke,
	      onTicker: invoke,
	      onBookSnapshot: invoke,
	      onKlineSummary: invoke,
	      onInstrumentUpdate: invoke,
	      onBalanceUpdate: invoke,
	      onOrderFilled: invoke,
	      onOrderRejected: invoke,
	      onOrderPartialFill: invoke,
	      onOrderCancelled: invoke,
	      onOrderAcknowledged: invoke,
	      onOrderExpired: invoke,
	      onRiskControl: invoke,
	      onExtensionEvent: invoke,
	      subscribedEvents: function () {
	        return events.slice ? events.slice() : [];
	      },
	      wantsCrossProviderEvents: function () { return false; }
	    };
	  }
	};
	`
)

type stubModule struct {
	name   string
	tag    string
	source string
}

// WriteStubStrategies creates a temporary strategy directory populated with
// the noop and logging strategy modules used in tests. The returned directory
// is cleaned up automatically by the test framework.
func WriteStubStrategies(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	modules := []stubModule{
		{name: "noop", tag: "v1.0.0", source: noopModuleSource},
		{name: "logging", tag: "v1.0.0", source: loggingModuleSource},
		{name: "delay", tag: "v1.0.0", source: delayModuleSource},
	}
	infos := make([]moduleInfo, 0, len(modules))
	for _, module := range modules {
		infos = append(infos, writeStubModule(t, dir, module))
	}
	writeRegistry(t, dir, infos)
	return dir
}

type moduleInfo struct {
	name    string
	tag     string
	hash    string
	relPath string
}

func writeStubModule(t testing.TB, root string, module stubModule) moduleInfo {
	t.Helper()
	moduleDir := filepath.Join(root, module.name, module.tag)
	if err := os.MkdirAll(moduleDir, 0o750); err != nil {
		t.Fatalf("create module directory %s: %v", moduleDir, err)
	}
	filename := fmt.Sprintf("%s.js", module.name)
	fullPath := filepath.Join(moduleDir, filename)
	if err := os.WriteFile(fullPath, []byte(module.source), 0o600); err != nil {
		t.Fatalf("write strategy module %s: %v", fullPath, err)
	}
	sum := sha256.Sum256([]byte(module.source))
	hash := "sha256:" + hex.EncodeToString(sum[:])
	rel := filepath.ToSlash(filepath.Join(module.name, module.tag, filename))
	return moduleInfo{name: module.name, tag: module.tag, hash: hash, relPath: rel}
}

func writeRegistry(t testing.TB, root string, modules []moduleInfo) {
	t.Helper()
	type registryLocation struct {
		Tag  string `json:"tag"`
		Path string `json:"path"`
	}
	type registryEntry struct {
		Tags   map[string]string           `json:"tags"`
		Hashes map[string]registryLocation `json:"hashes"`
	}

	registry := make(map[string]registryEntry, len(modules))
	for _, mod := range modules {
		tags := map[string]string{
			"latest": mod.hash,
		}
		if mod.tag != "" {
			tags[mod.tag] = mod.hash
		}
		hashes := map[string]registryLocation{
			mod.hash: {Tag: mod.tag, Path: mod.relPath},
		}
		registry[mod.name] = registryEntry{Tags: tags, Hashes: hashes}
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	path := filepath.Join(root, "registry.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write registry %s: %v", path, err)
	}
}
