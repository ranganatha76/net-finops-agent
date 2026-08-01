// Package generic implements a CSP-neutral FinOps scanner backed by inventory files.
package generic

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// Inventory is a CSP-neutral snapshot any cloud (or CMDB export) can produce.
// Point a provider's options.inventory_file at a YAML/JSON file of this shape
// to scan OCI, Alibaba, DigitalOcean, private clouds, etc. without a native SDK.
type Inventory struct {
	Cloud   string          `yaml:"cloud" json:"cloud"`
	Account string          `yaml:"account" json:"account"`
	Items   []InventoryItem `yaml:"items" json:"items"`
}

// InventoryItem is a single resource entry in a normalized inventory file.
type InventoryItem struct {
	Kind        string            `yaml:"kind" json:"kind"`
	Name        string            `yaml:"name" json:"name"`
	ResourceID  string            `yaml:"resource_id" json:"resource_id"`
	Region      string            `yaml:"region" json:"region"`
	Namespace   string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Tags        map[string]string `yaml:"tags,omitempty" json:"tags,omitempty"`
	CreatedAt   time.Time         `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	MetricName  string            `yaml:"metric_name" json:"metric_name"`
	MetricValue float64           `yaml:"metric_value" json:"metric_value"`
	MetricUnit  string            `yaml:"metric_unit" json:"metric_unit"`
	// PolicyClass optionally forces evaluation (compute_idle|storage_idle|replica_waste|…).
	PolicyClass string `yaml:"policy_class,omitempty" json:"policy_class,omitempty"`
}

// Scanner reads a normalized inventory file for an arbitrary CSP.
type Scanner struct {
	cfg      *config.Config
	cloud    string
	account  string
	path     string
	fallback *Inventory // used when path empty (demo)
}

// NewScanner constructs a generic scanner for the given cloud and inventory source.
func NewScanner(cfg *config.Config, cloud, account, inventoryPath string, fallback *Inventory) *Scanner {
	return &Scanner{cfg: cfg, cloud: cloud, account: account, path: inventoryPath, fallback: fallback}
}

// Name returns the cloud identifier configured for this scanner.
func (s *Scanner) Name() string { return s.cloud }

// Cloud returns the finops.Cloud value for this scanner.
func (s *Scanner) Cloud() finops.Cloud { return finops.Cloud(s.cloud) }

// Scan loads inventory and converts each item into a finops.Finding.
func (s *Scanner) Scan(ctx context.Context) ([]finops.Finding, error) {
	_ = ctx
	inv, err := s.load()
	if err != nil {
		return nil, err
	}
	cloud := inv.Cloud
	if cloud == "" {
		cloud = s.cloud
	}
	account := inv.Account
	if account == "" {
		account = s.account
	}
	var out []finops.Finding
	for _, it := range inv.Items {
		unit := it.MetricUnit
		if unit == "" {
			unit = "bytes/hour"
		}
		name := it.Name
		if name == "" {
			name = it.ResourceID
		}
		out = append(out, finops.Finding{
			ID:           fmt.Sprintf("%s:%s:%s", cloud, it.Kind, it.ResourceID),
			Cloud:        finops.Cloud(cloud),
			Kind:         finops.ResourceKind(it.Kind),
			Name:         name,
			ResourceID:   it.ResourceID,
			Region:       it.Region,
			AccountOrSub: account,
			Namespace:    it.Namespace,
			Tags:         it.Tags,
			CreatedAt:    it.CreatedAt,
			MetricName:   it.MetricName,
			MetricValue:  it.MetricValue,
			MetricUnit:   unit,
			PolicyClass:  finops.KindClass(it.PolicyClass),
		})
	}
	return out, nil
}

// load reads inventory from path or returns the demo fallback.
func (s *Scanner) load() (*Inventory, error) {
	if s.path == "" {
		if s.fallback != nil {
			return s.fallback, nil
		}
		return nil, fmt.Errorf("provider %s: set options.inventory_file to a normalized inventory YAML", s.cloud)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read inventory %s: %w", s.path, err)
	}
	var inv Inventory
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse inventory %s: %w", s.path, err)
	}
	return &inv, nil
}

// Remediator dry-runs (or logs) actions for generic inventory-backed clouds.
type Remediator struct {
	cloud   string
	deleted *[]string // resource IDs recorded during non-dry-run apply
}

// NewRemediator constructs a generic remediator for the given cloud identifier.
func NewRemediator(cloud string) *Remediator {
	d := []string{}
	return &Remediator{cloud: cloud, deleted: &d}
}

// Name returns the cloud identifier configured for this remediator.
func (r *Remediator) Name() string { return r.cloud }

// Supports reports whether the finding belongs to this generic cloud.
func (r *Remediator) Supports(f finops.Finding) bool {
	return string(f.Cloud) == r.cloud
}

// Apply executes or dry-runs the requested remediation action on a generic finding.
func (r *Remediator) Apply(_ context.Context, f finops.Finding, action finops.Action, dryRun bool) (finops.RemediationResult, error) {
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
		res.Message = fmt.Sprintf("dry-run: would %s %s %s on %s (wire CSP SDK mutator for live apply)", action, f.Kind, f.ResourceID, r.cloud)
		return res, nil
	}
	*r.deleted = append(*r.deleted, f.ResourceID)
	res.Success = true
	res.Message = fmt.Sprintf("recorded %s for %s (generic provider — replace with native CSP API call)", action, f.ResourceID)
	return res, nil
}
