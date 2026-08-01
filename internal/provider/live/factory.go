// Package live wires provider scanners and remediators for demo and live modes.
package live

import (
	"fmt"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/provider"
	"github.com/ranganatha76/net-finops-agent/internal/provider/aws"
	"github.com/ranganatha76/net-finops-agent/internal/provider/azure"
	"github.com/ranganatha76/net-finops-agent/internal/provider/gcp"
	"github.com/ranganatha76/net-finops-agent/internal/provider/generic"
	"github.com/ranganatha76/net-finops-agent/internal/provider/k8s"
	"github.com/ranganatha76/net-finops-agent/internal/provider/registry"
)

// Mode selects inventory backends.
type Mode string

const (
	// ModeDemo uses in-memory demo inventory clients.
	ModeDemo Mode = "demo"
	// ModeLive uses real cloud SDK clients (where implemented).
	ModeLive Mode = "live"
)

// init registers built-in CSP factories with the provider registry.
func init() {
	// Built-in CSPs. Additional clouds register the same way from any package.
	registry.Register("aws", factoryAWS)
	registry.Register("azure", factoryAzure)
	registry.Register("gcp", factoryGCP)
	registry.Register("kubernetes", factoryK8s)
	// Inventory-file backed — works for OCI, Alibaba, DigitalOcean, IBM, private clouds, …
	registry.Register("oci", factoryGeneric("oci"))
	registry.Register("alibaba", factoryGeneric("alibaba"))
	registry.Register("digitalocean", factoryGeneric("digitalocean"))
	registry.Register("ibm", factoryGeneric("ibm"))
	registry.Register("generic", factoryGeneric("generic"))
}

// Build assembles scanners/remediators for every enabled provider in config.
func Build(cfg *config.Config, mode Mode) ([]provider.Scanner, []provider.Remediator, error) {
	if mode == "" {
		mode = ModeDemo
	}
	switch mode {
	case ModeDemo, ModeLive:
		return registry.BuildEnabled(cfg, string(mode))
	default:
		return nil, nil, fmt.Errorf("unknown mode %q (want demo|live)", mode)
	}
}

// accountOf returns the configured account ID for a provider or a fallback value.
func accountOf(cfg *config.Config, id, fallback string) string {
	if p, ok := cfg.Provider(id); ok && p.Account != "" {
		return p.Account
	}
	return fallback
}

// inventoryPath returns the options.inventory_file path for a provider, if set.
func inventoryPath(cfg *config.Config, id string) string {
	if p, ok := cfg.Provider(id); ok && p.Options != nil {
		return p.Options["inventory_file"]
	}
	return ""
}

// factoryAWS builds AWS scanner and remediator for the requested mode.
func factoryAWS(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error) {
	if mode == string(ModeLive) {
		return nil, nil, fmt.Errorf("live AWS SDK adapter not wired yet; use --mode=demo or implement InventoryClient/MetricsClient/Mutator")
	}
	return aws.NewScanner(cfg, aws.DemoInventory{}, aws.DemoMetrics{}, accountOf(cfg, "aws", "demo-aws")),
		aws.NewRemediator(&aws.DemoMutator{}), nil
}

// factoryAzure builds Azure scanner and remediator for the requested mode.
func factoryAzure(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error) {
	if mode == string(ModeLive) {
		return nil, nil, fmt.Errorf("live Azure SDK adapter not wired yet; use --mode=demo")
	}
	return azure.NewScanner(cfg, azure.DemoInventory{}, azure.DemoMetrics{}, accountOf(cfg, "azure", "demo-azure")),
		azure.NewRemediator(&azure.DemoMutator{}), nil
}

// factoryGCP builds GCP scanner and remediator for the requested mode.
func factoryGCP(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error) {
	if mode == string(ModeLive) {
		return nil, nil, fmt.Errorf("live GCP SDK adapter not wired yet; use --mode=demo")
	}
	return gcp.NewScanner(cfg, gcp.DemoInventory{}, gcp.DemoMetrics{}, accountOf(cfg, "gcp", "demo-gcp")),
		gcp.NewRemediator(&gcp.DemoMutator{}), nil
}

// factoryK8s builds Kubernetes scanner and remediator for the requested mode.
func factoryK8s(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error) {
	if mode == string(ModeLive) {
		return nil, nil, fmt.Errorf("live Kubernetes client-go adapter not wired yet; use --mode=demo")
	}
	return k8s.NewScanner(cfg, k8s.DemoInventory{}, k8s.DemoMetrics{}, accountOf(cfg, "kubernetes", "blr-dev-cluster")),
		k8s.NewRemediator(&k8s.DemoMutator{}), nil
}

// factoryGeneric returns a registry factory for inventory-file-backed clouds.
func factoryGeneric(cloud string) registry.Factory {
	return func(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error) {
		path := inventoryPath(cfg, cloud)
		var fallback *generic.Inventory
		if path == "" && mode == string(ModeDemo) {
			fallback = demoInventoryFor(cloud)
		}
		if path == "" && fallback == nil {
			return nil, nil, fmt.Errorf("%s: set providers.%s.options.inventory_file to a normalized inventory YAML (see configs/inventory.example.yaml)", cloud, cloud)
		}
		return generic.NewScanner(cfg, cloud, accountOf(cfg, cloud, "demo-"+cloud), path, fallback),
			generic.NewRemediator(cloud), nil
	}
}

// demoInventoryFor returns built-in demo inventory for supported generic clouds.
func demoInventoryFor(cloud string) *generic.Inventory {
	now := time.Now().UTC()
	switch cloud {
	case "oci":
		return &generic.Inventory{
			Cloud: "oci", Account: "ocid1.tenancy.demo",
			Items: []generic.InventoryItem{
				{Kind: "nat_gateway", Name: "nat-oke-dev", ResourceID: "ocid1.natgateway.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-40 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "bytes_out_per_hour", MetricValue: 200_000, MetricUnit: "bytes/hour"},
				{Kind: "drg", Name: "drg-lab-orphaned", ResourceID: "ocid1.drg.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-70 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "attachment_count", MetricValue: 0, MetricUnit: "attachments"},
				{Kind: "igw", Name: "igw-scratch", ResourceID: "ocid1.igw.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-55 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "bytes_in_per_hour", MetricValue: 10_000, MetricUnit: "bytes/hour"},
				{Kind: "sgw", Name: "sgw-object-idle", ResourceID: "ocid1.sgw.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-33 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "bytes_to_services_per_hour", MetricValue: 0, MetricUnit: "bytes/hour"},
				{Kind: "public_ip", Name: "orphan-fip", ResourceID: "ocid1.publicip.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-15 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "associated", MetricValue: 0, MetricUnit: "bool"},
				{Kind: "instance", Name: "dev-oke-node-spare", ResourceID: "ocid1.instance.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-28 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "CPUUtilization", MetricValue: 0.8, MetricUnit: "cpu_percent"},
				{Kind: "disk", Name: "unattached-boot", ResourceID: "ocid1.bootvolume.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-90 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "attached", MetricValue: 0, MetricUnit: "attached", PolicyClass: "storage_idle"},
				{Kind: "database", Name: "atp-dev-idle", ResourceID: "ocid1.autonomousdatabase.demo", Region: "ap-mumbai-1",
					CreatedAt: now.Add(-50 * 24 * time.Hour), Tags: map[string]string{"environment": "development"},
					MetricName: "CPUUtilization", MetricValue: 3, MetricUnit: "cpu_percent"},
			},
		}
	default:
		return nil
	}
}
