// Package k8s implements the Kubernetes FinOps scanner and remediator.
package k8s

import (
	"context"
	"fmt"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// InventoryClient lists any Kubernetes resource that can incur cloud cost or waste capacity.
type InventoryClient interface {
	ListAll(ctx context.Context) ([]Resource, error)
}

// MetricsClient fetches utilization metrics for Kubernetes resources from the cluster.
type MetricsClient interface {
	Metric(ctx context.Context, namespace, name string, lookback time.Duration) (float64, error)
}

// Mutator deletes Kubernetes resources during remediation.
type Mutator interface {
	DeleteResource(ctx context.Context, kind finops.ResourceKind, namespace, name string) error
}

// Resource is any K8s object under FinOps evaluation.
type Resource struct {
	Kind        finops.ResourceKind
	Name        string
	Namespace   string
	CreatedAt   time.Time
	Labels      map[string]string
	MetricName  string
	MetricUnit  string
	// If MetricValue is set (>=0) and MetricFromClient is false, use it directly.
	MetricValue      float64
	MetricFromClient bool // when true, Metric is fetched from MetricsClient
	PolicyClass      finops.KindClass
}

// Scanner discovers idle or wasteful Kubernetes workloads and network resources.
type Scanner struct {
	cfg       *config.Config
	inventory InventoryClient
	metrics   MetricsClient
	cluster   string // default cluster name when not configured
}

// NewScanner constructs a Kubernetes scanner with the given clients and cluster name.
func NewScanner(cfg *config.Config, inventory InventoryClient, metrics MetricsClient, cluster string) *Scanner {
	return &Scanner{cfg: cfg, inventory: inventory, metrics: metrics, cluster: cluster}
}

// Name returns the provider identifier "kubernetes".
func (s *Scanner) Name() string { return "kubernetes" }

// Cloud returns finops.CloudK8s.
func (s *Scanner) Cloud() finops.Cloud { return finops.CloudK8s }

// clusterName returns the configured cluster identifier or the constructor default.
func (s *Scanner) clusterName() string {
	if p, ok := s.cfg.Provider("kubernetes"); ok {
		if p.Account != "" {
			return p.Account
		}
		if len(p.Regions) > 0 {
			return p.Regions[0]
		}
	}
	return s.cluster
}

// Scan enumerates cluster resources and emits findings with utilization metrics.
func (s *Scanner) Scan(ctx context.Context) ([]finops.Finding, error) {
	lookback := time.Duration(s.cfg.Scan.LookbackHours) * time.Hour
	cluster := s.clusterName()
	items, err := s.inventory.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	var out []finops.Finding
	for _, it := range items {
		val := it.MetricValue
		if it.MetricFromClient {
			val, err = s.metrics.Metric(ctx, it.Namespace, it.Name, lookback)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", it.Namespace, it.Name, err)
			}
		}
		unit := it.MetricUnit
		if unit == "" {
			unit = "rps"
		}
		out = append(out, finops.Finding{
			ID:           fmt.Sprintf("k8s:%s:%s/%s", it.Kind, it.Namespace, it.Name),
			Cloud:        finops.CloudK8s,
			Kind:         it.Kind,
			Name:         it.Name,
			ResourceID:   it.Namespace + "/" + it.Name,
			Region:       cluster,
			Namespace:    it.Namespace,
			AccountOrSub: cluster,
			Tags:         it.Labels,
			CreatedAt:    it.CreatedAt,
			MetricName:   it.MetricName,
			MetricValue:  val,
			MetricUnit:   unit,
			PolicyClass:  it.PolicyClass,
		})
	}
	return out, nil
}

// Remediator applies shutdown actions for Kubernetes findings.
type Remediator struct{ mutator Mutator }

// NewRemediator constructs a Kubernetes remediator backed by the given mutator.
func NewRemediator(m Mutator) *Remediator { return &Remediator{mutator: m} }

// Name returns the provider identifier "kubernetes".
func (r *Remediator) Name() string { return "kubernetes" }

// Supports reports whether the finding belongs to Kubernetes.
func (r *Remediator) Supports(f finops.Finding) bool {
	return f.Cloud == finops.CloudK8s
}

// Apply executes or dry-runs the requested remediation action on a Kubernetes finding.
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
		res.Message = fmt.Sprintf("dry-run: would %s %s %s", action, f.Kind, f.ResourceID)
		return res, nil
	}
	ns, name, err := splitNN(f.ResourceID)
	if err != nil {
		res.Success = false
		res.Message = err.Error()
		return res, err
	}
	if err := r.mutator.DeleteResource(ctx, f.Kind, ns, name); err != nil {
		res.Success = false
		res.Message = err.Error()
		return res, err
	}
	res.Success = true
	res.Message = fmt.Sprintf("applied %s to %s", action, f.ResourceID)
	return res, nil
}

// splitNN parses a "namespace/name" resource identifier.
func splitNN(id string) (string, string, error) {
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return id[:i], id[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid resource id %q", id)
}
