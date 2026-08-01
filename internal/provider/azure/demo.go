package azure

import (
	"context"
	"time"
)

// DemoInventory simulates idle Azure VNets commonly left behind by Bengaluru devops teams.
type DemoInventory struct{}

// ListNATGateways returns demo NAT gateways for supported locations.
func (DemoInventory) ListNATGateways(_ context.Context, location string) ([]NATGateway, error) {
	if location != "centralindia" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []NATGateway{
		{
			ID: "/subscriptions/demo/resourceGroups/rg-dev-blr/providers/Microsoft.Network/natGateways/nat-aks-dev",
			Name: "nat-aks-dev", ResourceGroup: "rg-dev-blr", Location: location,
			CreatedAt: now.Add(-45 * 24 * time.Hour),
			Tags:      map[string]string{"environment": "development", "owner": "platform-blr"},
		},
		{
			ID: "/subscriptions/demo/resourceGroups/rg-sandbox/providers/Microsoft.Network/natGateways/nat-sandbox",
			Name: "nat-sandbox", ResourceGroup: "rg-sandbox", Location: location,
			CreatedAt: now.Add(-12 * time.Hour), // too young → protected
			Tags:      map[string]string{"environment": "sandbox"},
		},
	}, nil
}

// ListPublicIPs returns demo public IP addresses for supported locations.
func (DemoInventory) ListPublicIPs(_ context.Context, location string) ([]PublicIP, error) {
	if location != "centralindia" {
		return nil, nil
	}
	return []PublicIP{
		{
			ID: "/subscriptions/demo/resourceGroups/rg-dev-blr/providers/Microsoft.Network/publicIPAddresses/pip-abandoned",
			Name: "pip-abandoned", Location: location, Associated: false,
			Tags: map[string]string{"environment": "development"},
		},
		{
			ID: "/subscriptions/demo/resourceGroups/rg-prod/providers/Microsoft.Network/publicIPAddresses/pip-appgw",
			Name: "pip-appgw", Location: location, Associated: true,
			Tags: map[string]string{"environment": "production"},
		},
	}, nil
}

// DemoMetrics provides fixed utilization metrics for demo Azure resources.
type DemoMetrics struct{}

// NATBytesPerHour returns demo bytes-per-hour values keyed by resource ID.
func (DemoMetrics) NATBytesPerHour(_ context.Context, resourceID string, _ time.Duration) (float64, error) {
	if contains(resourceID, "nat-aks-dev") {
		return 50_000, nil
	}
	if contains(resourceID, "nat-sandbox") {
		return 0, nil
	}
	return 20_000_000, nil
}

// DemoMutator records deleted Azure resources for demo remediation.
type DemoMutator struct {
	Deleted []string // resource IDs "deleted" during demo apply
}

// DeleteNATGateway appends the resource ID to Deleted and succeeds without calling Azure.
func (m *DemoMutator) DeleteNATGateway(_ context.Context, resourceID string) error {
	m.Deleted = append(m.Deleted, resourceID)
	return nil
}

// DeletePublicIP appends the resource ID to Deleted and succeeds without calling Azure.
func (m *DemoMutator) DeletePublicIP(_ context.Context, resourceID string) error {
	m.Deleted = append(m.Deleted, resourceID)
	return nil
}
