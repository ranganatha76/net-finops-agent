// Package azure implements the Azure FinOps scanner and remediator.
package azure

import (
	"context"
	"fmt"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// InventoryClient lists Azure NAT gateways and public IPs.
type InventoryClient interface {
	ListNATGateways(ctx context.Context, location string) ([]NATGateway, error)
	ListPublicIPs(ctx context.Context, location string) ([]PublicIP, error)
}

// MetricsClient returns average processed bytes/hour for a NAT gateway.
type MetricsClient interface {
	NATBytesPerHour(ctx context.Context, resourceID string, lookback time.Duration) (float64, error)
}

// Mutator remediates Azure network resources.
type Mutator interface {
	DeleteNATGateway(ctx context.Context, resourceID string) error
	DeletePublicIP(ctx context.Context, resourceID string) error
}

// NATGateway represents an Azure NAT gateway inventory record.
type NATGateway struct {
	ID            string
	Name          string
	ResourceGroup string
	Location      string
	CreatedAt     time.Time
	Tags          map[string]string
}

// PublicIP represents an Azure public IP address inventory record.
type PublicIP struct {
	ID         string
	Name       string
	Location   string
	Associated bool // true when attached to a NIC or load balancer
	Tags       map[string]string
}

// Scanner discovers idle Azure VNet pathways.
type Scanner struct {
	cfg          *config.Config
	inventory    InventoryClient
	metrics      MetricsClient
	subscription string // Azure subscription ID
}

// NewScanner constructs an Azure scanner with the given clients and subscription ID.
func NewScanner(cfg *config.Config, inventory InventoryClient, metrics MetricsClient, subscription string) *Scanner {
	return &Scanner{cfg: cfg, inventory: inventory, metrics: metrics, subscription: subscription}
}

// Name returns the provider identifier "azure".
func (s *Scanner) Name() string { return "azure" }

// Cloud returns finops.CloudAzure.
func (s *Scanner) Cloud() finops.Cloud { return finops.CloudAzure }

// regions returns configured Azure locations or a default location.
func (s *Scanner) regions() []string {
	if p, ok := s.cfg.Provider("azure"); ok && len(p.Regions) > 0 {
		return p.Regions
	}
	return []string{"centralindia"}
}

// Scan enumerates Azure NAT gateways and public IPs and emits findings.
func (s *Scanner) Scan(ctx context.Context) ([]finops.Finding, error) {
	lookback := time.Duration(s.cfg.Scan.LookbackHours) * time.Hour
	var out []finops.Finding

	for _, loc := range s.regions() {
		nats, err := s.inventory.ListNATGateways(ctx, loc)
		if err != nil {
			return nil, fmt.Errorf("list Azure NAT gateways in %s: %w", loc, err)
		}
		for _, n := range nats {
			bps, err := s.metrics.NATBytesPerHour(ctx, n.ID, lookback)
			if err != nil {
				return nil, fmt.Errorf("Azure NAT metrics %s: %w", n.ID, err)
			}
			out = append(out, finops.Finding{
				ID:           fmt.Sprintf("azure:nat:%s", n.ID),
				Cloud:        finops.CloudAzure,
				Kind:         finops.KindNATGateway,
				Name:         n.Name,
				ResourceID:   n.ID,
				Region:       n.Location,
				AccountOrSub: s.subscription,
				Tags:         n.Tags,
				CreatedAt:    n.CreatedAt,
				MetricName:   "ByteCount_avg_per_hour",
				MetricValue:  bps,
				MetricUnit:   "bytes/hour",
			})
		}

		ips, err := s.inventory.ListPublicIPs(ctx, loc)
		if err != nil {
			return nil, fmt.Errorf("list public IPs in %s: %w", loc, err)
		}
		for _, ip := range ips {
			assoc := 1.0
			if !ip.Associated {
				assoc = 0
			}
			out = append(out, finops.Finding{
				ID:           fmt.Sprintf("azure:pip:%s", ip.ID),
				Cloud:        finops.CloudAzure,
				Kind:         finops.KindPublicIP,
				Name:         ip.Name,
				ResourceID:   ip.ID,
				Region:       ip.Location,
				AccountOrSub: s.subscription,
				Tags:         ip.Tags,
				MetricName:   "associated",
				MetricValue:  assoc,
				MetricUnit:   "bool",
			})
		}
	}
	return out, nil
}

// Remediator applies shutdown actions for Azure findings.
type Remediator struct {
	mutator Mutator
}

// NewRemediator constructs an Azure remediator backed by the given mutator.
func NewRemediator(m Mutator) *Remediator {
	return &Remediator{mutator: m}
}

// Name returns the provider identifier "azure".
func (r *Remediator) Name() string { return "azure" }

// Supports reports whether the finding belongs to Azure.
func (r *Remediator) Supports(f finops.Finding) bool {
	return f.Cloud == finops.CloudAzure
}

// Apply executes or dry-runs the requested remediation action on an Azure finding.
func (r *Remediator) Apply(ctx context.Context, f finops.Finding, action finops.Action, dryRun bool) (finops.RemediationResult, error) {
	res := finops.RemediationResult{
		FindingID:   f.ID,
		Action:      action,
		DryRun:      dryRun,
		AppliedAt:   time.Now().UTC(),
		SavedUSDEst: f.MonthlyWasteUSD,
	}
	if action != finops.ActionShutdown && action != finops.ActionDownsize {
		res.Success = true
		res.Message = "no mutating action required"
		return res, nil
	}
	if dryRun {
		res.Success = true
		res.Message = fmt.Sprintf("dry-run: would %s %s %s", action, f.Kind, f.ResourceID)
		return res, nil
	}
	var err error
	switch f.Kind {
	case finops.KindNATGateway:
		err = r.mutator.DeleteNATGateway(ctx, f.ResourceID)
	case finops.KindPublicIP:
		err = r.mutator.DeletePublicIP(ctx, f.ResourceID)
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
