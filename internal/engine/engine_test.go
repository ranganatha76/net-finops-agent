package engine_test

import (
	"context"
	"testing"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/engine"
	"github.com/ranganatha76/net-finops-agent/internal/provider/live"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

func TestDemoScanFindsWaste(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers["aws"] = config.ProviderConfig{Enabled: true, Regions: []string{"ap-south-1"}, Account: "demo"}
	cfg.Providers["azure"] = config.ProviderConfig{Enabled: true, Regions: []string{"centralindia"}, Account: "demo"}
	cfg.Providers["gcp"] = config.ProviderConfig{Enabled: true, Regions: []string{"asia-south1"}, Account: "demo"}
	cfg.Providers["oci"] = config.ProviderConfig{Enabled: true, Regions: []string{"ap-mumbai-1"}, Account: "demo"}
	cfg.Providers["kubernetes"] = config.ProviderConfig{Enabled: true, Account: "blr-dev-cluster"}

	scanners, remediators, err := live.Build(cfg, live.ModeDemo)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(cfg, scanners, remediators, nil)
	rep, err := eng.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.ActionableCount == 0 {
		t.Fatal("expected actionable findings in demo inventory")
	}
	if rep.TotalWasteUSD <= 0 {
		t.Fatal("expected positive waste estimate")
	}
	if rep.ProtectedCount == 0 {
		t.Fatal("expected some protected findings (prod tag / young / system ns)")
	}

	clouds := map[finops.Cloud]bool{}
	for _, f := range rep.Findings {
		clouds[f.Cloud] = true
	}
	for _, want := range []finops.Cloud{finops.CloudAWS, finops.CloudAzure, finops.CloudGCP, finops.CloudOCI, finops.CloudK8s} {
		if !clouds[want] {
			t.Fatalf("expected findings from %s", want)
		}
	}
}

func TestDemoRemediateDryRun(t *testing.T) {
	cfg := config.Defaults()
	cfg.Remediator.MaxActionsPerRun = 50

	scanners, remediators, err := live.Build(cfg, live.ModeDemo)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(cfg, scanners, remediators, nil)
	rep, err := eng.Remediate(context.Background(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Remediations) == 0 {
		t.Fatal("expected dry-run remediations")
	}
	for _, r := range rep.Remediations {
		if !r.DryRun {
			t.Fatalf("expected dry-run, got live for %s", r.FindingID)
		}
		if !r.Success {
			t.Fatalf("dry-run should succeed: %s", r.Message)
		}
		if r.Action != finops.ActionShutdown && r.Action != finops.ActionDownsize {
			t.Fatalf("unexpected action %s", r.Action)
		}
	}
}
