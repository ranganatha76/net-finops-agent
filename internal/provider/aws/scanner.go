// Package aws implements the AWS FinOps scanner and remediator.
package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// InventoryClient lists billable AWS network gateways and related resources.
type InventoryClient interface {
	ListNATGateways(ctx context.Context, region string) ([]NATGateway, error)
	ListInternetGateways(ctx context.Context, region string) ([]InternetGateway, error)
	ListTransitGateways(ctx context.Context, region string) ([]TransitGateway, error)
	ListVPNGateways(ctx context.Context, region string) ([]VPNGateway, error)
	ListElasticIPs(ctx context.Context, region string) ([]ElasticIP, error)
	ListVPCEndpoints(ctx context.Context, region string) ([]VPCEndpoint, error)
	ListLoadBalancers(ctx context.Context, region string) ([]LoadBalancer, error)
	ListCloudResources(ctx context.Context, region string) ([]CloudResource, error)
}

// MetricsClient fetches CloudWatch-style utilization metrics for AWS resources.
type MetricsClient interface {
	BytesPerHour(ctx context.Context, region, resourceID string, lookback time.Duration) (float64, error)
	RequestRate(ctx context.Context, region, resourceID string, lookback time.Duration) (float64, error)
}

// Mutator deletes or downsizes AWS resources during remediation.
type Mutator interface {
	DeleteResource(ctx context.Context, region string, kind finops.ResourceKind, id string) error
}

// NATGateway represents an AWS NAT gateway inventory record.
type NATGateway struct {
	ID, Name, VpcID, State string
	CreatedAt              time.Time
	Tags                   map[string]string
}

// InternetGateway represents an AWS internet gateway inventory record.
type InternetGateway struct {
	ID, Name, VpcID string
	Attached        bool // true when attached to a VPC
	CreatedAt       time.Time
	Tags            map[string]string
}

// TransitGateway represents an AWS transit gateway inventory record.
type TransitGateway struct {
	ID, Name    string
	Attachments int // number of VPC/VPN attachments
	CreatedAt   time.Time
	Tags        map[string]string
}

// VPNGateway represents an AWS virtual private gateway inventory record.
type VPNGateway struct {
	ID, Name    string
	Attachments int // number of VPN connections
	CreatedAt   time.Time
	Tags        map[string]string
}

// ElasticIP represents an AWS elastic IP address inventory record.
type ElasticIP struct {
	AllocationID, PublicIP, AssociationID string
	Tags                                  map[string]string
}

// VPCEndpoint represents an AWS VPC interface or gateway endpoint.
type VPCEndpoint struct {
	ID, Service, VpcID string
	CreatedAt          time.Time
	Tags               map[string]string
}

// LoadBalancer represents an AWS ELB/ALB/NLB inventory record.
type LoadBalancer struct {
	ARN, Name, Type string
	CreatedAt       time.Time
	Tags            map[string]string
}

// CloudResource is any non-network AWS resource (compute, storage, data, …).
type CloudResource struct {
	Kind        finops.ResourceKind
	ID, Name    string
	CreatedAt   time.Time
	Tags        map[string]string
	MetricName  string
	MetricValue float64
	MetricUnit  string
	PolicyClass finops.KindClass // optional policy override
}

// Scanner discovers idle or wasteful AWS network and cloud resources.
type Scanner struct {
	cfg       *config.Config
	inventory InventoryClient
	metrics   MetricsClient
	account   string // AWS account ID
}

// NewScanner constructs an AWS scanner with the given clients and account ID.
func NewScanner(cfg *config.Config, inventory InventoryClient, metrics MetricsClient, account string) *Scanner {
	return &Scanner{cfg: cfg, inventory: inventory, metrics: metrics, account: account}
}

// Name returns the provider identifier "aws".
func (s *Scanner) Name() string { return "aws" }

// Cloud returns finops.CloudAWS.
func (s *Scanner) Cloud() finops.Cloud { return finops.CloudAWS }

// regions returns configured AWS regions or a default region.
func (s *Scanner) regions() []string {
	if p, ok := s.cfg.Provider("aws"); ok && len(p.Regions) > 0 {
		return p.Regions
	}
	return []string{"ap-south-1"}
}

// Scan enumerates AWS resources across configured regions and emits findings.
func (s *Scanner) Scan(ctx context.Context) ([]finops.Finding, error) {
	lookback := time.Duration(s.cfg.Scan.LookbackHours) * time.Hour
	var out []finops.Finding

	for _, region := range s.regions() {
		nats, err := s.inventory.ListNATGateways(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, n := range nats {
			if n.State != "" && n.State != "available" {
				continue
			}
			bps, err := s.metrics.BytesPerHour(ctx, region, n.ID, lookback)
			if err != nil {
				return nil, err
			}
			out = append(out, s.finding(region, finops.KindNATGateway, coalesce(n.Name, n.ID), n.ID, n.Tags, n.CreatedAt, "BytesOut_avg_per_hour", bps, "bytes/hour"))
		}

		igws, err := s.inventory.ListInternetGateways(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, g := range igws {
			assoc := 1.0
			if !g.Attached {
				assoc = 0
			}
			// Detached IGW → association metric; attached uses traffic bytes.
			if !g.Attached {
				out = append(out, s.finding(region, finops.KindInternetGateway, coalesce(g.Name, g.ID), g.ID, g.Tags, g.CreatedAt, "attached", assoc, "bool"))
			} else {
				bps, err := s.metrics.BytesPerHour(ctx, region, g.ID, lookback)
				if err != nil {
					return nil, err
				}
				out = append(out, s.finding(region, finops.KindInternetGateway, coalesce(g.Name, g.ID), g.ID, g.Tags, g.CreatedAt, "NetworkIn_avg_per_hour", bps, "bytes/hour"))
			}
		}

		tgws, err := s.inventory.ListTransitGateways(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, t := range tgws {
			out = append(out, s.finding(region, finops.KindTransitGateway, coalesce(t.Name, t.ID), t.ID, t.Tags, t.CreatedAt, "attachment_count", float64(t.Attachments), "attachments"))
		}

		vgws, err := s.inventory.ListVPNGateways(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, v := range vgws {
			out = append(out, s.finding(region, finops.KindVPNGateway, coalesce(v.Name, v.ID), v.ID, v.Tags, v.CreatedAt, "attachment_count", float64(v.Attachments), "attachments"))
		}

		eips, err := s.inventory.ListElasticIPs(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, eip := range eips {
			assoc := 1.0
			if eip.AssociationID == "" {
				assoc = 0
			}
			out = append(out, s.finding(region, finops.KindPublicIP, eip.PublicIP, eip.AllocationID, eip.Tags, time.Time{}, "associated", assoc, "bool"))
		}

		eps, err := s.inventory.ListVPCEndpoints(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, ep := range eps {
			bps, err := s.metrics.BytesPerHour(ctx, region, ep.ID, lookback)
			if err != nil {
				return nil, err
			}
			out = append(out, s.finding(region, finops.KindPrivateEndpoint, ep.Service, ep.ID, ep.Tags, ep.CreatedAt, "BytesProcessed_avg_per_hour", bps, "bytes/hour"))
		}

		lbs, err := s.inventory.ListLoadBalancers(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, lb := range lbs {
			rps, err := s.metrics.RequestRate(ctx, region, lb.ARN, lookback)
			if err != nil {
				return nil, err
			}
			out = append(out, s.finding(region, finops.KindLoadBalancer, lb.Name, lb.ARN, lb.Tags, lb.CreatedAt, "RequestCount_avg_rps", rps, "rps"))
		}

		resources, err := s.inventory.ListCloudResources(ctx, region)
		if err != nil {
			return nil, err
		}
		for _, r := range resources {
			f := s.finding(region, r.Kind, coalesce(r.Name, r.ID), r.ID, r.Tags, r.CreatedAt, r.MetricName, r.MetricValue, r.MetricUnit)
			f.PolicyClass = r.PolicyClass
			out = append(out, f)
		}
	}
	return out, nil
}

// finding builds a finops.Finding for a single AWS resource.
func (s *Scanner) finding(region string, kind finops.ResourceKind, name, id string, tags map[string]string, created time.Time, metric string, value float64, unit string) finops.Finding {
	return finops.Finding{
		ID:           fmt.Sprintf("aws:%s:%s:%s", kind, region, id),
		Cloud:        finops.CloudAWS,
		Kind:         kind,
		Name:         name,
		ResourceID:   id,
		Region:       region,
		AccountOrSub: s.account,
		Tags:         tags,
		CreatedAt:    created,
		MetricName:   metric,
		MetricValue:  value,
		MetricUnit:   unit,
	}
}

// coalesce returns a when non-empty, otherwise b.
func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Remediator applies shutdown or downsize actions to AWS findings.
type Remediator struct{ mutator Mutator }

// NewRemediator constructs an AWS remediator backed by the given mutator.
func NewRemediator(m Mutator) *Remediator { return &Remediator{mutator: m} }

// Name returns the provider identifier "aws".
func (r *Remediator) Name() string { return "aws" }

// Supports reports whether the finding belongs to AWS.
func (r *Remediator) Supports(f finops.Finding) bool {
	return f.Cloud == finops.CloudAWS
}

// Apply executes or dry-runs the requested remediation action on an AWS finding.
func (r *Remediator) Apply(ctx context.Context, f finops.Finding, action finops.Action, dryRun bool) (finops.RemediationResult, error) {
	res := finops.RemediationResult{
		FindingID: f.ID, Action: action, DryRun: dryRun,
		AppliedAt: time.Now().UTC(), SavedUSDEst: f.MonthlyWasteUSD,
	}
	if action != finops.ActionShutdown && action != finops.ActionDownsize {
		res.Success = true
		res.Message = "no mutating action required"
		return res, nil
	}
	if dryRun {
		res.Success = true
		res.Message = fmt.Sprintf("dry-run: would %s %s %s in %s", action, f.Kind, f.ResourceID, f.Region)
		return res, nil
	}
	if err := r.mutator.DeleteResource(ctx, f.Region, f.Kind, f.ResourceID); err != nil {
		res.Success = false
		res.Message = err.Error()
		return res, err
	}
	res.Success = true
	res.Message = fmt.Sprintf("applied %s to %s", action, f.ResourceID)
	return res, nil
}
