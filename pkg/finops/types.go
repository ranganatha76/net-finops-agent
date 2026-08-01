// Package finops defines the shared domain model for FinOps findings,
// remediation results, and resource kind classification.
package finops

import "time"

// Cloud is any cloud / platform identifier (aws, azure, gcp, oci, kubernetes, …).
// It is intentionally a free-form string so custom CSPs work without code changes.
type Cloud string

// Well-known cloud identifiers used by built-in providers.
const (
	CloudAWS          Cloud = "aws"
	CloudAzure        Cloud = "azure"
	CloudGCP          Cloud = "gcp"
	CloudOCI          Cloud = "oci"
	CloudAlibaba      Cloud = "alibaba"
	CloudDigitalOcean Cloud = "digitalocean"
	CloudIBM          Cloud = "ibm"
	CloudK8s          Cloud = "kubernetes"
)

// Action is the remediation the policy engine recommends or a remediator applies.
type Action string

const (
	ActionNone     Action = "none"     // utilization within policy; no change
	ActionShutdown Action = "shutdown" // delete / deallocate / scale-to-zero
	ActionDownsize Action = "downsize" // cheaper SKU, fewer replicas, consolidate
	ActionNotify   Action = "notify"   // report only (typical for protected resources)
)

// Severity ranks urgency / estimated savings impact of a finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Finding is one cloud or Kubernetes resource discovered during a scan,
// enriched by policy with recommended action, cost estimate, and protection status.
type Finding struct {
	ID           string            `json:"id"`             // stable unique id (cloud:kind:…)
	Cloud        Cloud             `json:"cloud"`          // CSP / platform id
	Kind         ResourceKind      `json:"kind"`           // resource type (nat_gateway, deployment, …)
	Name         string            `json:"name"`           // human-readable name
	ResourceID   string            `json:"resource_id"`    // vendor id / namespace/name
	Region       string            `json:"region"`         // region, location, or cluster
	AccountOrSub string            `json:"account_or_subscription,omitempty"` // account / project / tenancy
	Namespace    string            `json:"namespace,omitempty"`               // Kubernetes namespace when applicable
	Tags         map[string]string `json:"tags,omitempty"`  // labels/tags used for protection rules
	CreatedAt    time.Time         `json:"created_at,omitempty"`

	LookbackHours int     `json:"lookback_hours"` // utilization window copied from config
	MetricName    string  `json:"metric_name"`    // e.g. CPUUtilization, BytesOut
	MetricValue   float64 `json:"metric_value"`   // observed utilization
	MetricUnit    string  `json:"metric_unit"`    // e.g. cpu_percent, bytes/hour, rps

	// PolicyClass optionally forces evaluation class (from inventory). Empty → auto-classify.
	PolicyClass KindClass `json:"policy_class,omitempty"`

	Threshold       float64  `json:"threshold"`                   // threshold used for the decision
	Recommended     Action   `json:"recommended_action"`          // shutdown | downsize | notify | none
	Severity        Severity `json:"severity"`
	MonthlyWasteUSD float64  `json:"monthly_waste_usd"`           // estimated monthly savings if acted on
	Rationale       string   `json:"rationale"`                   // human-readable explanation
	Protected       bool     `json:"protected"`                   // true if guardrails blocked mutation
	ProtectedReason string   `json:"protected_reason,omitempty"`
}

// RemediationResult records the outcome of applying (or dry-running) an action on a finding.
type RemediationResult struct {
	FindingID   string    `json:"finding_id"`            // Finding.ID that was targeted
	Action      Action    `json:"action"`                // action attempted
	DryRun      bool      `json:"dry_run"`               // true if no live mutation occurred
	Success     bool      `json:"success"`               // whether the remediator completed OK
	Message     string    `json:"message"`               // detail / error text
	AppliedAt   time.Time `json:"applied_at"`
	SavedUSDEst float64   `json:"saved_usd_monthly_est"` // copied from finding waste estimate
}

// ScanReport aggregates all findings (and optional remediations) from one engine run.
type ScanReport struct {
	GeneratedAt     time.Time           `json:"generated_at"`
	LookbackHours   int                 `json:"lookback_hours"`
	DryRun          bool                `json:"dry_run"`
	Findings        []Finding           `json:"findings"`
	Remediations    []RemediationResult `json:"remediations,omitempty"`
	TotalWasteUSD   float64             `json:"total_monthly_waste_usd"` // sum of actionable monthly waste
	ActionableCount int                 `json:"actionable_count"`        // shutdown/downsize and not protected
	ProtectedCount  int                 `json:"protected_count"`
}
