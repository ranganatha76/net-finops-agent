package policy_test

import (
	"testing"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/policy"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

func base(kind finops.ResourceKind, val float64, unit string) finops.Finding {
	return finops.Finding{
		Cloud: finops.CloudAWS, Kind: kind, MetricValue: val, MetricUnit: unit,
		CreatedAt: time.Now().Add(-48 * time.Hour),
		Tags:      map[string]string{"environment": "development"},
	}
}

func TestIdleNATAndDRG(t *testing.T) {
	ev := policy.New(config.Defaults())
	f := base(finops.KindNATGateway, 100_000, "bytes/hour")
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown {
		t.Fatalf("nat: %s", f.Recommended)
	}
	f = base(finops.KindDRG, 0, "attachments")
	f.Cloud = finops.CloudOCI
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown || f.Kind != finops.KindDynamicRoutingGateway {
		t.Fatalf("drg: kind=%s action=%s", f.Kind, f.Recommended)
	}
}

func TestIdleComputeAndStorage(t *testing.T) {
	ev := policy.New(config.Defaults())
	f := base(finops.KindInstance, 1.0, "cpu_percent")
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown {
		t.Fatalf("instance: %s (%s)", f.Recommended, f.Rationale)
	}
	f = base(finops.KindDisk, 0, "attached")
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown {
		t.Fatalf("disk: %s", f.Recommended)
	}
	f = base(finops.KindFunction, 0, "invocations_per_hour")
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown {
		t.Fatalf("function: %s", f.Recommended)
	}
}

func TestK8sWorkloads(t *testing.T) {
	ev := policy.New(config.Defaults())
	f := base(finops.KindDeployment, 0, "replicas")
	f.Cloud = finops.CloudK8s
	f.Namespace = "scratch"
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown {
		t.Fatalf("deployment: %s", f.Recommended)
	}
	f = base(finops.KindDeployment, 10, "replicas")
	f.Cloud = finops.CloudK8s
	f.Namespace = "scratch"
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionDownsize {
		t.Fatalf("want downsize for over-replicas, got %s", f.Recommended)
	}
	f = base(finops.KindPVC, 0, "in_use")
	f.Cloud = finops.CloudK8s
	f.Namespace = "scratch"
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown {
		t.Fatalf("pvc: %s", f.Recommended)
	}
}

func TestCustomUnknownResource(t *testing.T) {
	ev := policy.New(config.Defaults())
	f := base("my_custom_saas_connector", 0, "utilization")
	ev.Evaluate(&f)
	if f.Recommended != finops.ActionShutdown {
		t.Fatalf("custom resource should still evaluate, got %s", f.Recommended)
	}
}

func TestProtectProduction(t *testing.T) {
	ev := policy.New(config.Defaults())
	f := base(finops.KindInstance, 0, "cpu_percent")
	f.Tags = map[string]string{"environment": "production"}
	ev.Evaluate(&f)
	if !f.Protected {
		t.Fatal("expected protection")
	}
}
