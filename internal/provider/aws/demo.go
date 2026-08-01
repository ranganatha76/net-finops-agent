package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// DemoInventory provides sample AWS network and cloud resources for demo mode.
type DemoInventory struct{}

// ListNATGateways returns demo NAT gateways for supported regions.
func (DemoInventory) ListNATGateways(_ context.Context, region string) ([]NATGateway, error) {
	if region != "ap-south-1" && region != "us-east-1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []NATGateway{
		{ID: "nat-0idledev001", Name: "dev-eks-nat", VpcID: "vpc-dev-blr", State: "available", CreatedAt: now.Add(-30 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development", "Name": "dev-eks-nat"}},
		{ID: "nat-0lowqa002", Name: "qa-shared-nat", VpcID: "vpc-qa-blr", State: "available", CreatedAt: now.Add(-60 * 24 * time.Hour),
			Tags: map[string]string{"environment": "qa", "Name": "qa-shared-nat"}},
		{ID: "nat-0prodkeep", Name: "prod-egress-nat", VpcID: "vpc-prod-blr", State: "available", CreatedAt: now.Add(-180 * 24 * time.Hour),
			Tags: map[string]string{"environment": "production", "finops": "keep", "Name": "prod-egress-nat"}},
	}, nil
}

// ListInternetGateways returns demo internet gateways for supported regions.
func (DemoInventory) ListInternetGateways(_ context.Context, region string) ([]InternetGateway, error) {
	if region != "ap-south-1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []InternetGateway{
		{ID: "igw-detached01", Name: "old-lab-igw", Attached: false, CreatedAt: now.Add(-50 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}},
		{ID: "igw-idle02", Name: "dev-igw", VpcID: "vpc-dev-blr", Attached: true, CreatedAt: now.Add(-30 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}},
	}, nil
}

// ListTransitGateways returns demo transit gateways for supported regions.
func (DemoInventory) ListTransitGateways(_ context.Context, region string) ([]TransitGateway, error) {
	if region != "ap-south-1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []TransitGateway{
		{ID: "tgw-orphan", Name: "lab-tgw", Attachments: 0, CreatedAt: now.Add(-80 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}},
		{ID: "tgw-sparse", Name: "qa-tgw", Attachments: 1, CreatedAt: now.Add(-100 * 24 * time.Hour),
			Tags: map[string]string{"environment": "qa"}},
	}, nil
}

// ListVPNGateways returns demo VPN gateways for supported regions.
func (DemoInventory) ListVPNGateways(_ context.Context, region string) ([]VPNGateway, error) {
	if region != "ap-south-1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []VPNGateway{
		{ID: "vgw-idle", Name: "contractor-vpn", Attachments: 0, CreatedAt: now.Add(-45 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}},
	}, nil
}

// ListElasticIPs returns demo elastic IP addresses for supported regions.
func (DemoInventory) ListElasticIPs(_ context.Context, region string) ([]ElasticIP, error) {
	if region != "ap-south-1" {
		return nil, nil
	}
	return []ElasticIP{
		{AllocationID: "eipalloc-idle01", PublicIP: "13.233.10.20", AssociationID: "", Tags: map[string]string{"environment": "development"}},
		{AllocationID: "eipalloc-used01", PublicIP: "13.233.55.90", AssociationID: "eipassoc-abc", Tags: map[string]string{"environment": "production"}},
	}, nil
}

// ListVPCEndpoints returns demo VPC endpoints for supported regions.
func (DemoInventory) ListVPCEndpoints(_ context.Context, region string) ([]VPCEndpoint, error) {
	if region != "ap-south-1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []VPCEndpoint{
		{ID: "vpce-s3idle", Service: "com.amazonaws.ap-south-1.s3", VpcID: "vpc-dev-blr", CreatedAt: now.Add(-40 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}},
	}, nil
}

// ListLoadBalancers returns demo load balancers for supported regions.
func (DemoInventory) ListLoadBalancers(_ context.Context, region string) ([]LoadBalancer, error) {
	if region != "ap-south-1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []LoadBalancer{
		{ARN: "arn:aws:elasticloadbalancing:ap-south-1:123:loadbalancer/app/dev-alb/abc", Name: "dev-alb", Type: "application",
			CreatedAt: now.Add(-25 * 24 * time.Hour), Tags: map[string]string{"environment": "development"}},
	}, nil
}

// ListCloudResources returns demo compute, storage, and data resources for supported regions.
func (DemoInventory) ListCloudResources(_ context.Context, region string) ([]CloudResource, error) {
	if region != "ap-south-1" {
		return nil, nil
	}
	now := time.Now().UTC()
	return []CloudResource{
		{Kind: finops.KindInstance, ID: "i-idledev01", Name: "dev-box", CreatedAt: now.Add(-40 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}, MetricName: "CPUUtilization", MetricValue: 1.2, MetricUnit: "cpu_percent"},
		{Kind: finops.KindDisk, ID: "vol-unattached01", Name: "old-data", CreatedAt: now.Add(-90 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}, MetricName: "attached", MetricValue: 0, MetricUnit: "attached", PolicyClass: finops.ClassStorageIdle},
		{Kind: finops.KindSnapshot, ID: "snap-ancient01", Name: "backup-2024", CreatedAt: now.Add(-400 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}, MetricName: "referenced", MetricValue: 0, MetricUnit: "bool"},
		{Kind: finops.KindDatabase, ID: "db-qa-idle", Name: "qa-postgres", CreatedAt: now.Add(-60 * 24 * time.Hour),
			Tags: map[string]string{"environment": "qa"}, MetricName: "CPUUtilization", MetricValue: 2.0, MetricUnit: "cpu_percent"},
		{Kind: finops.KindFunction, ID: "fn-orphan", Name: "legacy-webhook", CreatedAt: now.Add(-120 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}, MetricName: "Invocations", MetricValue: 0, MetricUnit: "invocations_per_hour"},
		{Kind: finops.KindObjectBucket, ID: "s3://blr-dev-scratch", Name: "blr-dev-scratch", CreatedAt: now.Add(-200 * 24 * time.Hour),
			Tags: map[string]string{"environment": "development"}, MetricName: "NumberOfObjects", MetricValue: 0, MetricUnit: "in_use"},
	}, nil
}

// DemoMetrics provides fixed utilization metrics for demo AWS resources.
type DemoMetrics struct{}

// BytesPerHour returns demo bytes-per-hour values keyed by resource ID.
func (DemoMetrics) BytesPerHour(_ context.Context, _, id string, _ time.Duration) (float64, error) {
	switch id {
	case "nat-0idledev001", "igw-idle02", "vpce-s3idle":
		return 120_000, nil
	case "nat-0lowqa002":
		return 4_000_000, nil
	case "nat-0prodkeep":
		return 80_000_000, nil
	default:
		return 0, nil
	}
}

// RequestRate returns demo request-rate values keyed by load balancer ARN.
func (DemoMetrics) RequestRate(_ context.Context, _, id string, _ time.Duration) (float64, error) {
	if id == "arn:aws:elasticloadbalancing:ap-south-1:123:loadbalancer/app/dev-alb/abc" {
		return 0.001, nil
	}
	return 10, nil
}

// DemoMutator records deleted resources for demo remediation.
type DemoMutator struct {
	Deleted []string // kind:id strings of resources "deleted" during demo apply
}

// DeleteResource appends the resource to Deleted and succeeds without calling AWS.
func (m *DemoMutator) DeleteResource(_ context.Context, _ string, kind finops.ResourceKind, id string) error {
	m.Deleted = append(m.Deleted, fmt.Sprintf("%s:%s", kind, id))
	return nil
}
