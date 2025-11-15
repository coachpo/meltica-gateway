package js

import (
	"context"
	"io"
	"log"
	"testing"
)

const configAwareModule = `
module.exports = {
  metadata: {
    name: "config_probe",
    version: "1.0.0",
    displayName: "Config Probe",
    description: "Validates env.config presence.",
    config: [
      {
        name: "logger_prefix",
        type: "string",
        required: false
      }
    ],
    events: ["Trade"]
  },
  create: function(env) {
    if (!env || !env.config) {
      throw new Error("config missing");
    }
    if (env.config.logger_prefix !== "[test]") {
      throw new Error("unexpected config value " + env.config.logger_prefix);
    }
    return {};
  }
};
`

func TestNewStrategyReceivesConfig(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "config_probe", "v1.0.0", []byte(configAwareModule))
	writeRegistry(t, dir, "config_probe", "v1.0.0", modulePath)

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	module, err := loader.Get("config_probe")
	if err != nil {
		t.Fatalf("Get config_probe: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	cfg := map[string]any{"logger_prefix": "[test]"}
	strat, err := NewStrategy(module, cfg, logger)
	if err != nil {
		t.Fatalf("NewStrategy: %v", err)
	}
	t.Cleanup(func() { strat.Close() })
}
