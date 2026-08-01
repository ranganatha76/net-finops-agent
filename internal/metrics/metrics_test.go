package metrics_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ranganatha76/net-finops-agent/internal/metrics"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

func TestTenantID(t *testing.T) {
	f := finops.Finding{AccountOrSub: "acct", Cloud: "aws"}
	if metrics.TenantID(f) != "acct" {
		t.Fatalf("got %s", metrics.TenantID(f))
	}
	f.Tags = map[string]string{"tenant": "acme-corp"}
	if metrics.TenantID(f) != "acme-corp" {
		t.Fatalf("tag should win, got %s", metrics.TenantID(f))
	}
}

func TestObserveScanAndRemediation(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	report := &finops.ScanReport{
		GeneratedAt: time.Now().UTC(),
		Findings: []finops.Finding{
			{
				ID: "1", Cloud: "aws", Kind: "nat_gateway", AccountOrSub: "tenant-a",
				Recommended: finops.ActionShutdown, MonthlyWasteUSD: 32.85,
				Tags: map[string]string{"tenant": "tenant-a"},
			},
			{
				ID: "2", Cloud: "oci", Kind: "instance", AccountOrSub: "tenant-b",
				Recommended: finops.ActionShutdown, MonthlyWasteUSD: 10,
				Protected: true,
			},
		},
	}
	rec.ObserveScan(report)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var monthly float64
	for _, mf := range mfs {
		if mf.GetName() == "finops_cost_savings_usd_monthly" {
			for _, m := range mf.GetMetric() {
				monthly += m.GetGauge().GetValue()
			}
		}
	}
	if monthly < 32 {
		t.Fatalf("expected ~32.85 monthly gauge, got %v", monthly)
	}

	rec.ObserveRemediation(report.Findings[0], finops.RemediationResult{
		FindingID: "1", Action: finops.ActionShutdown, DryRun: false, Success: true, SavedUSDEst: 32.85,
	})

	mfs, _ = reg.Gather()
	var shutdowns, savings float64
	for _, mf := range mfs {
		switch mf.GetName() {
		case "finops_resources_shutdown_total":
			for _, m := range mf.GetMetric() {
				shutdowns += m.GetCounter().GetValue()
			}
		case "finops_cost_savings_usd_total":
			for _, m := range mf.GetMetric() {
				savings += m.GetCounter().GetValue()
			}
		}
	}
	if shutdowns != 1 {
		t.Fatalf("shutdowns=%v", shutdowns)
	}
	if savings < 32 {
		t.Fatalf("savings=%v", savings)
	}
}
