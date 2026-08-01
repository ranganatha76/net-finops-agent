// Package gcp implements the GCP FinOps scanner and remediator.
package gcp

import (
	"context"
	"fmt"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// InventoryClient lists Cloud NAT and static addresses.
type InventoryClient interface {
	ListCloudNATs(ctx context.Context, region string) ([]CloudNAT, error)
	ListAddresses(ctx context.Context, region string) ([]Address, error)
}

// MetricsClient fetches Cloud Monitoring metrics for GCP network resources.
type MetricsClient interface {
	NATBytesPerHour(ctx context.Context, region, natID string, lookback time.Duration) (float64, error)
}

// Mutator deletes Cloud NAT gateways and releases static addresses during remediation.
type Mutator interface {
	DeleteCloudNAT(ctx context.Context, region, natID string) error
	ReleaseAddress(ctx context.Context, region, name string) error
}

// CloudNAT represents a GCP Cloud NAT inventory record.
type CloudNAT struct {
	ID        string
	Name      string
	Network   string // VPC network name
	CreatedAt time.Time
	Tags      map[string]string
}

// Address represents a GCP regional static external IP address.
type Address struct {
	Name      string
	Address   string // allocated IP
	InUse     bool   // true when attached to a resource
	Tags      map[string]string
	CreatedAt time.Time
}

// Scanner discovers idle GCP Cloud NAT gateways and unattached addresses.
type Scanner struct {
	cfg       *config.Config
	inventory InventoryClient
	metrics   MetricsClient
	project   string // GCP project ID
}

// NewScanner constructs a GCP scanner with the given clients and project ID.
func NewScanner(cfg *config.Config, inventory InventoryClient, metrics MetricsClient, project string) *Scanner {
	return &Scanner{cfg: cfg, inventory: inventory, metrics: metrics, project: project}
}

// Name returns the provider identifier "gcp".
func (s *Scanner) Name() string { return "gcp" }

// Cloud returns finops.CloudGCP.
func (s *Scanner) Cloud() finops.Cloud { return finops.CloudGCP }

// regions returns configured GCP regions or a default region.
func (s *Scanner) regions() []string {
	if p, ok := s.cfg.Provider("gcp"); ok && len(p.Regions) > 0 {
		return p.Regions
	}
	return []string{"asia-south1"}
}

// Scan enumerates Cloud NAT gateways and static addresses and emits findings.
func (s *Scanner) Scan(ctx context.Context) ([]finops.Finding, error) {
	lookback := time.Duration(s.cfg.Scan.LookbackHours) * time.Hour
	var out []finops.Finding
	for _, region := range s.regions() {
		nats, err := s.inventory.ListCloudNATs(ctx, region)
		if err != nil {
			return nil, fmt.Errorf("list Cloud NAT in %s: %w", region, err)
		}
		for _, n := range nats {
			bps, err := s.metrics.NATBytesPerHour(ctx, region, n.ID, lookback)
			if err != nil {
				return nil, err
			}
			out = append(out, finops.Finding{
				ID:           fmt.Sprintf("gcp:nat:%s:%s", region, n.ID),
				Cloud:        finops.CloudGCP,
				Kind:         finops.KindNATGateway,
				Name:         n.Name,
				ResourceID:   n.ID,
				Region:       region,
				AccountOrSub: s.project,
				Tags:         n.Tags,
				CreatedAt:    n.CreatedAt,
				MetricName:   "router_nat_sent_bytes_avg_per_hour",
				MetricValue:  bps,
				MetricUnit:   "bytes/hour",
			})
		}
		addrs, err := s.inventory.ListAddresses(ctx, region)
		if err != nil {
			return nil, fmt.Errorf("list addresses in %s: %w", region, err)
		}
		for _, a := range addrs {
			assoc := 1.0
			if !a.InUse {
				assoc = 0
			}
			out = append(out, finops.Finding{
				ID:           fmt.Sprintf("gcp:ip:%s:%s", region, a.Name),
				Cloud:        finops.CloudGCP,
				Kind:         finops.KindPublicIP,
				Name:         a.Address,
				ResourceID:   a.Name,
				Region:       region,
				AccountOrSub: s.project,
				Tags:         a.Tags,
				CreatedAt:    a.CreatedAt,
				MetricName:   "associated",
				MetricValue:  assoc,
				MetricUnit:   "bool",
			})
		}
	}
	return out, nil
}

// Remediator applies shutdown actions for GCP findings.
type Remediator struct{ mutator Mutator }

// NewRemediator constructs a GCP remediator backed by the given mutator.
func NewRemediator(m Mutator) *Remediator { return &Remediator{mutator: m} }

// Name returns the provider identifier "gcp".
func (r *Remediator) Name() string { return "gcp" }

// Supports reports whether the finding belongs to GCP.
func (r *Remediator) Supports(f finops.Finding) bool {
	return f.Cloud == finops.CloudGCP
}

// Apply executes or dry-runs the requested remediation action on a GCP finding.
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
	var err error
	switch f.Kind {
	case finops.KindNATGateway:
		err = r.mutator.DeleteCloudNAT(ctx, f.Region, f.ResourceID)
	case finops.KindPublicIP:
		err = r.mutator.ReleaseAddress(ctx, f.Region, f.ResourceID)
	default:
		res.Success = false
		res.Message = fmt.Sprintf("unsupported kind %s", f.Kind)
		return res, nil
	}
	if err != nil {
		res.Success = false
		res.Message = err.Error()
		return res, err
	}
	res.Success = true
	res.Message = fmt.Sprintf("applied %s to %s", action, f.ResourceID)
	return res, nil
}
