package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ranganatha76/net-finops-agent/internal/config"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
scan:
  lookback_hours: 24
policy:
  nat_idle_bytes_per_hour: 1000
providers:
  aws:
    enabled: true
    regions: [ap-south-1]
remediator:
  dry_run: true
  max_actions_per_run: 5
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scan.LookbackHours != 24 {
		t.Fatalf("lookback=%d", cfg.Scan.LookbackHours)
	}
	if cfg.Remediator.MaxActionsPerRun != 5 {
		t.Fatalf("max=%d", cfg.Remediator.MaxActionsPerRun)
	}
	if cfg.Policy.ExcludeTags["finops"] != "keep" {
		t.Fatal("expected default exclude tags to be filled")
	}
	p, ok := cfg.Provider("aws")
	if !ok || !p.Enabled || p.Regions[0] != "ap-south-1" {
		t.Fatalf("aws provider: %+v", p)
	}
}

func TestHourlyRateFallback(t *testing.T) {
	cfg := config.Defaults()
	if cfg.HourlyRate("some-new-csp", "nat_gateway") != cfg.Cost.Defaults["nat_gateway"] {
		t.Fatal("unknown CSP should use defaults")
	}
	if cfg.HourlyRate("oci", "drg") != 0.05 {
		t.Fatalf("drg alias rate=%v", cfg.HourlyRate("oci", "drg"))
	}
}
