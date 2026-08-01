package aws_test

import (
	"context"
	"testing"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/provider/aws"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

func TestAWSDemoScanGateways(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers["aws"] = config.ProviderConfig{Enabled: true, Regions: []string{"ap-south-1"}, Account: "acct"}
	s := aws.NewScanner(cfg, aws.DemoInventory{}, aws.DemoMetrics{}, "acct")
	findings, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[finops.ResourceKind]bool{}
	for _, f := range findings {
		kinds[f.Kind] = true
	}
	for _, want := range []finops.ResourceKind{
		finops.KindNATGateway, finops.KindInternetGateway, finops.KindTransitGateway,
		finops.KindVPNGateway, finops.KindPublicIP, finops.KindPrivateEndpoint, finops.KindLoadBalancer,
		finops.KindInstance, finops.KindDisk, finops.KindDatabase, finops.KindFunction,
	} {
		if !kinds[want] {
			t.Fatalf("missing kind %s", want)
		}
	}
	r := aws.NewRemediator(&aws.DemoMutator{})
	res, err := r.Apply(context.Background(), findings[0], finops.ActionShutdown, true)
	if err != nil || !res.Success || !res.DryRun {
		t.Fatalf("dry-run failed: %+v err=%v", res, err)
	}
}
