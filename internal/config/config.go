// Package config loads and defaults the agent YAML configuration:
// scan window, policy thresholds, cost rates, providers, and remediator safety knobs.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// Config is the top-level agent configuration loaded from YAML.
// Providers is a free-form map keyed by CSP id (aws, azure, gcp, oci, …).
type Config struct {
	Scan       ScanConfig                `yaml:"scan"`       // lookback window
	Policy     PolicyConfig              `yaml:"policy"`     // idle thresholds & protection
	Cost       CostConfig                `yaml:"cost"`       // hourly USD rate tables
	Providers  map[string]ProviderConfig `yaml:"providers"`  // which CSPs to enable
	Remediator RemediatorConfig          `yaml:"remediator"` // dry-run & blast-radius caps
	Service    ServiceConfig             `yaml:"service"`    // HTTP microservice + scheduler
}

// ServiceConfig configures the long-running HTTP microservice (Prometheus/Grafana).
type ServiceConfig struct {
	// ListenAddr is the HTTP bind address (default ":8080").
	ListenAddr string `yaml:"listen_addr"`
	// ScheduleInterval is how often to scan (Go duration string, e.g. "15m").
	ScheduleInterval string `yaml:"schedule_interval"`
	// AutoRemediate runs remediate after each scheduled scan when true.
	AutoRemediate bool `yaml:"auto_remediate"`
	// DryRun applies to scheduled/API remediate cycles (API can override via query).
	DryRun bool `yaml:"dry_run"`
}

// ScheduleDuration parses ScheduleInterval, defaulting to 15 minutes.
func (c ServiceConfig) ScheduleDuration() time.Duration {
	if c.ScheduleInterval == "" {
		return 15 * time.Minute
	}
	d, err := time.ParseDuration(c.ScheduleInterval)
	if err != nil || d <= 0 {
		return 15 * time.Minute
	}
	return d
}

// ScanConfig controls how far back utilization metrics are evaluated.
type ScanConfig struct {
	LookbackHours int `yaml:"lookback_hours"` // e.g. 168 = 7 days
}

// PolicyConfig holds idle/low thresholds and protection rules applied to every finding.
type PolicyConfig struct {
	// Network / gateway traffic (bytes/hour and attachment counts).
	GatewayIdleBytesPerHour float64 `yaml:"gateway_idle_bytes_per_hour"` // ≤ → shutdown
	GatewayLowBytesPerHour  float64 `yaml:"gateway_low_bytes_per_hour"`  // ≤ → downsize
	GatewayIdleRPS          float64 `yaml:"gateway_idle_rps"`            // LB/ingress idle RPS
	MinGatewayAttachments   int     `yaml:"min_gateway_attachments"`     // DRG/TGW sparse threshold

	// Compute (VM, function, pod, database CPU, …).
	ComputeIdlePercent float64 `yaml:"compute_idle_percent"` // ≤ → shutdown
	ComputeLowPercent  float64 `yaml:"compute_low_percent"`  // ≤ → downsize

	// Storage (disk, snapshot, bucket, PVC, …).
	StorageIdlePercent float64 `yaml:"storage_idle_percent"` // % used below this → reclaim

	// Kubernetes / replica workloads.
	MinActiveReplicas int `yaml:"min_active_replicas"` // below → shutdown idle workload
	MaxIdleReplicas   int `yaml:"max_idle_replicas"`   // above → downsize over-provisioning

	// Catch-all for unknown resource kinds (ClassUtilization).
	GenericIdleThreshold float64 `yaml:"generic_idle_threshold"`

	MinAgeHours         int               `yaml:"min_age_hours"`         // skip resources younger than this
	ExcludeTags         map[string]string `yaml:"exclude_tags"`          // tag key=value → protect
	ProtectedNamespaces []string          `yaml:"protected_namespaces"`  // K8s namespaces never mutated

	// Deprecated aliases still accepted in YAML for backward compatibility.
	NATIdleBytesPerHour float64 `yaml:"nat_idle_bytes_per_hour"`
	NATLowBytesPerHour  float64 `yaml:"nat_low_bytes_per_hour"`
	IngressIdleRPS      float64 `yaml:"ingress_idle_rps"`
}

// CostConfig holds default hourly USD rates plus optional per-cloud overrides.
type CostConfig struct {
	Defaults map[string]float64            `yaml:"defaults"`  // kind → USD/hour
	ByCloud  map[string]map[string]float64 `yaml:"by_cloud"`  // cloud → kind → USD/hour
}

// ProviderConfig describes one registered cloud / platform to scan.
type ProviderConfig struct {
	Enabled bool              `yaml:"enabled"` // include in engine runs when true
	Regions []string          `yaml:"regions"` // regions, locations, or cluster names
	Account string            `yaml:"account"` // account / subscription / project / tenancy
	Options map[string]string `yaml:"options"` // free-form (e.g. inventory_file)
}

// RemediatorConfig controls mutation safety (dry-run, caps, approval tags).
type RemediatorConfig struct {
	DryRun             bool `yaml:"dry_run"`               // prefer dry-run in config (CLI --apply can override)
	MaxActionsPerRun   int  `yaml:"max_actions_per_run"`   // blast-radius cap per remediate call
	RequireApprovalTag bool `yaml:"require_approval_tag"`  // if true, need finops-approve=true
}

// Load reads YAML config from path and merges it onto Defaults().
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

// defaultRates returns the built-in hourly USD catalog for known resource kinds.
func defaultRates() map[string]float64 {
	return map[string]float64{
		// network
		"nat_gateway": 0.045, "internet_gateway": 0, "service_gateway": 0.02,
		"dynamic_routing_gateway": 0.05, "transit_gateway": 0.05, "vpn_gateway": 0.05,
		"express_route_gateway": 0.15, "network_firewall": 0.40, "application_gateway": 0.10,
		"private_endpoint": 0.01, "public_ip": 0.005, "load_balancer": 0.025,
		"igw": 0, "sgw": 0.02, "drg": 0.05, "tgw": 0.05, "vgw": 0.05, "elastic_ip": 0.005, "vpc_endpoint": 0.01,
		// compute / data
		"instance": 0.05, "vm": 0.05, "ec2": 0.05, "spot_instance": 0.02, "dedicated_host": 1.0,
		"function": 0.002, "lambda": 0.002, "container_app": 0.03, "batch_job": 0.02,
		"database": 0.08, "rds": 0.08, "cache": 0.04, "warehouse": 0.20, "search_cluster": 0.10,
		"queue": 0.001, "stream": 0.02,
		// storage
		"disk": 0.01, "ebs": 0.01, "snapshot": 0.005, "image": 0.003, "file_share": 0.03,
		"object_bucket": 0.002, "s3": 0.002, "backup_vault": 0.01,
		// kubernetes
		"ingress": 0.025, "gateway_api": 0.025, "service_loadbalancer": 0.025, "service_nodeport": 0.005,
		"service_clusterip": 0.001, "ingress_controller": 0.03, "api_gateway": 0.03,
		"deployment": 0.02, "statefulset": 0.03, "daemonset": 0.02, "replicaset": 0.01,
		"job": 0.005, "cronjob": 0.005, "pod": 0.01, "persistentvolumeclaim": 0.01,
		"persistentvolume": 0.01, "horizontalpodautoscaler": 0.001, "namespace": 0.0,
	}
}

// Defaults returns a safe multi-cloud starting configuration (dry-run, conservative thresholds).
func Defaults() *Config {
	return &Config{
		Scan: ScanConfig{LookbackHours: 168},
		Policy: PolicyConfig{
			GatewayIdleBytesPerHour: 1 << 20,
			GatewayLowBytesPerHour:  10 << 20,
			GatewayIdleRPS:          0.01,
			MinGatewayAttachments:   2,
			ComputeIdlePercent:      5,
			ComputeLowPercent:       20,
			StorageIdlePercent:      5,
			MinActiveReplicas:       1,
			MaxIdleReplicas:         3,
			GenericIdleThreshold:    0.01,
			MinAgeHours:             24,
			ExcludeTags: map[string]string{
				"finops":      "keep",
				"environment": "production",
			},
			ProtectedNamespaces: []string{"kube-system", "kube-public", "ingress-nginx"},
		},
		Cost: CostConfig{
			Defaults: defaultRates(),
			ByCloud: map[string]map[string]float64{
				"aws": {
					"nat_gateway": 0.045, "transit_gateway": 0.05, "vpn_gateway": 0.05,
					"network_firewall": 0.395, "public_ip": 0.005, "private_endpoint": 0.01,
					"load_balancer": 0.025,
				},
				"azure": {
					"nat_gateway": 0.045, "vpn_gateway": 0.19, "express_route_gateway": 0.25,
					"application_gateway": 0.12, "network_firewall": 0.40, "public_ip": 0.004,
				},
				"gcp": {
					"nat_gateway": 0.044, "vpn_gateway": 0.05, "public_ip": 0.004, "private_endpoint": 0.01,
				},
				"oci": {
					"nat_gateway": 0.025, "dynamic_routing_gateway": 0.05, "service_gateway": 0.0,
					"internet_gateway": 0.0, "public_ip": 0.003,
				},
				"kubernetes": {
					"ingress": 0.025, "gateway_api": 0.025, "service_loadbalancer": 0.025,
					"service_nodeport": 0.005, "ingress_controller": 0.03, "api_gateway": 0.03,
				},
			},
		},
		Providers: map[string]ProviderConfig{
			"aws":        {Enabled: true, Regions: []string{"ap-south-1", "us-east-1"}, Account: "demo-aws"},
			"azure":      {Enabled: true, Regions: []string{"centralindia"}, Account: "demo-azure"},
			"gcp":        {Enabled: true, Regions: []string{"asia-south1"}, Account: "demo-gcp"},
			"oci":        {Enabled: true, Regions: []string{"ap-mumbai-1"}, Account: "demo-oci"},
			"kubernetes": {Enabled: true, Regions: []string{"blr-dev-cluster"}, Account: "blr-dev-cluster"},
		},
		Remediator: RemediatorConfig{DryRun: true, MaxActionsPerRun: 100},
		Service: ServiceConfig{
			ListenAddr:       ":8080",
			ScheduleInterval: "15m",
			AutoRemediate:    false,
			DryRun:           true,
		},
	}
}

// applyDefaults fills zero-value fields after YAML unmarshal so partial configs are safe.
func (c *Config) applyDefaults() {
	d := Defaults()
	if c.Scan.LookbackHours <= 0 {
		c.Scan.LookbackHours = d.Scan.LookbackHours
	}
	// Merge legacy YAML keys into canonical gateway_* fields.
	if c.Policy.GatewayIdleBytesPerHour <= 0 {
		if c.Policy.NATIdleBytesPerHour > 0 {
			c.Policy.GatewayIdleBytesPerHour = c.Policy.NATIdleBytesPerHour
		} else {
			c.Policy.GatewayIdleBytesPerHour = d.Policy.GatewayIdleBytesPerHour
		}
	}
	if c.Policy.GatewayLowBytesPerHour <= 0 {
		if c.Policy.NATLowBytesPerHour > 0 {
			c.Policy.GatewayLowBytesPerHour = c.Policy.NATLowBytesPerHour
		} else {
			c.Policy.GatewayLowBytesPerHour = d.Policy.GatewayLowBytesPerHour
		}
	}
	if c.Policy.GatewayIdleRPS <= 0 {
		if c.Policy.IngressIdleRPS > 0 {
			c.Policy.GatewayIdleRPS = c.Policy.IngressIdleRPS
		} else {
			c.Policy.GatewayIdleRPS = d.Policy.GatewayIdleRPS
		}
	}
	if c.Policy.MinGatewayAttachments <= 0 {
		c.Policy.MinGatewayAttachments = d.Policy.MinGatewayAttachments
	}
	if c.Policy.ComputeIdlePercent <= 0 {
		c.Policy.ComputeIdlePercent = d.Policy.ComputeIdlePercent
	}
	if c.Policy.ComputeLowPercent <= 0 {
		c.Policy.ComputeLowPercent = d.Policy.ComputeLowPercent
	}
	if c.Policy.StorageIdlePercent <= 0 {
		c.Policy.StorageIdlePercent = d.Policy.StorageIdlePercent
	}
	if c.Policy.MinActiveReplicas <= 0 {
		c.Policy.MinActiveReplicas = d.Policy.MinActiveReplicas
	}
	if c.Policy.MaxIdleReplicas <= 0 {
		c.Policy.MaxIdleReplicas = d.Policy.MaxIdleReplicas
	}
	if c.Policy.GenericIdleThreshold <= 0 {
		c.Policy.GenericIdleThreshold = d.Policy.GenericIdleThreshold
	}
	if c.Policy.MinAgeHours <= 0 {
		c.Policy.MinAgeHours = d.Policy.MinAgeHours
	}
	if c.Remediator.MaxActionsPerRun <= 0 {
		c.Remediator.MaxActionsPerRun = d.Remediator.MaxActionsPerRun
	}
	if len(c.Policy.ExcludeTags) == 0 {
		c.Policy.ExcludeTags = d.Policy.ExcludeTags
	}
	if c.Providers == nil {
		c.Providers = d.Providers
	}
	if c.Cost.Defaults == nil {
		c.Cost.Defaults = d.Cost.Defaults
	} else {
		for k, v := range d.Cost.Defaults {
			if _, ok := c.Cost.Defaults[k]; !ok {
				c.Cost.Defaults[k] = v
			}
		}
	}
	if c.Cost.ByCloud == nil {
		c.Cost.ByCloud = d.Cost.ByCloud
	}
	if c.Service.ListenAddr == "" {
		c.Service.ListenAddr = d.Service.ListenAddr
	}
	if c.Service.ScheduleInterval == "" {
		c.Service.ScheduleInterval = d.Service.ScheduleInterval
	}
}

// Provider returns config for a cloud id (case-insensitive), or false if missing.
func (c *Config) Provider(cloud string) (ProviderConfig, bool) {
	if c.Providers == nil {
		return ProviderConfig{}, false
	}
	if p, ok := c.Providers[cloud]; ok {
		return p, true
	}
	for k, p := range c.Providers {
		if strings.EqualFold(k, cloud) {
			return p, true
		}
	}
	return ProviderConfig{}, false
}

// HourlyRate looks up USD/hour for a cloud + resource kind (with alias normalization),
// falling back to defaults then a conservative 0.01.
func (c *Config) HourlyRate(cloud, kind string) float64 {
	cloud = strings.ToLower(cloud)
	kind = string(finops.NormalizeKind(finops.ResourceKind(strings.ToLower(kind))))
	if by, ok := c.Cost.ByCloud[cloud]; ok {
		if v, ok := by[kind]; ok {
			return v
		}
	}
	if v, ok := c.Cost.Defaults[kind]; ok {
		return v
	}
	return 0.01
}

// EnabledProviders returns enabled provider ids, preferring a stable well-known order first.
func (c *Config) EnabledProviders() []string {
	prefer := []string{"aws", "azure", "gcp", "oci", "alibaba", "digitalocean", "ibm", "kubernetes"}
	seen := map[string]bool{}
	var out []string
	for _, id := range prefer {
		if p, ok := c.Providers[id]; ok && p.Enabled {
			out = append(out, id)
			seen[id] = true
		}
	}
	for id, p := range c.Providers {
		if p.Enabled && !seen[id] {
			out = append(out, id)
		}
	}
	return out
}
