package gcp

import (
	"context"
	"time"
)

// DemoInventory provides sample GCP Cloud NAT and address resources for demo mode.
type DemoInventory struct{}

// ListCloudNATs returns demo Cloud NAT gateways for supported regions.
func (DemoInventory) ListCloudNATs(_ context.Context, region string) ([]CloudNAT, error) {
	if region != "asia-south1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []CloudNAT{
		{
			ID: "nat-gke-dev-blr", Name: "gke-dev-nat", Network: "vpc-dev-blr",
			CreatedAt: now.Add(-35 * 24 * time.Hour),
			Tags:      map[string]string{"environment": "development", "team": "platform-blr"},
		},
		{
			ID: "nat-prod-keep", Name: "prod-nat", Network: "vpc-prod",
			CreatedAt: now.Add(-200 * 24 * time.Hour),
			Tags:      map[string]string{"environment": "production", "finops": "keep"},
		},
	}, nil
}

// ListAddresses returns demo static IP addresses for supported regions.
func (DemoInventory) ListAddresses(_ context.Context, region string) ([]Address, error) {
	if region != "asia-south1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []Address{
		{Name: "scratch-ip", Address: "34.93.10.20", InUse: false, CreatedAt: now.Add(-10 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}},
	}, nil
}

// DemoMetrics provides fixed utilization metrics for demo GCP resources.
type DemoMetrics struct{}

// NATBytesPerHour returns demo bytes-per-hour values keyed by NAT ID.
func (DemoMetrics) NATBytesPerHour(_ context.Context, _, natID string, _ time.Duration) (float64, error) {
	if natID == "nat-gke-dev-blr" {
		return 80_000, nil
	}
	return 50_000_000, nil
}

// DemoMutator records deleted GCP resources for demo remediation.
type DemoMutator struct {
	Deleted []string // prefixed resource IDs "deleted" during demo apply
}

// DeleteCloudNAT appends the NAT ID to Deleted and succeeds without calling GCP.
func (m *DemoMutator) DeleteCloudNAT(_ context.Context, _, natID string) error {
	m.Deleted = append(m.Deleted, "nat:"+natID)
	return nil
}

// ReleaseAddress appends the address name to Deleted and succeeds without calling GCP.
func (m *DemoMutator) ReleaseAddress(_ context.Context, _, name string) error {
	m.Deleted = append(m.Deleted, "ip:"+name)
	return nil
}
