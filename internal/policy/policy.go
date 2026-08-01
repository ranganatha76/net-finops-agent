// Package policy classifies findings and decides shutdown/downsize/notify actions
// using config thresholds, protection rules, and hourly cost rates.
package policy

import (
	"fmt"
	"strings"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// Evaluator applies utilization policy to any cloud or Kubernetes Finding.
type Evaluator struct {
	cfg *config.Config // thresholds, exclude tags, cost rates
}

// New creates an Evaluator bound to cfg.
func New(cfg *config.Config) *Evaluator {
	return &Evaluator{cfg: cfg}
}

// Evaluate mutates f with Recommended action, Severity, MonthlyWasteUSD, Rationale,
// and Protected status. Call once per finding after a provider scan.
func (e *Evaluator) Evaluate(f *finops.Finding) {
	f.LookbackHours = e.cfg.Scan.LookbackHours
	f.Kind = finops.NormalizeKind(f.Kind)

	if reason, ok := e.protectionReason(f); ok {
		f.Protected = true
		f.ProtectedReason = reason
		f.Recommended = finops.ActionNotify
		f.Severity = finops.SeverityLow
		return
	}

	class := f.PolicyClass
	if class == "" {
		class = finops.Classify(f.Kind, f.MetricUnit)
	}
	// Unit-based overrides for gateway attachment/association edge cases.
	if class == finops.ClassAttachment && isBytesUnit(f.MetricUnit) {
		class = finops.ClassTrafficBytes
	}
	if (class == finops.ClassTrafficBytes || class == finops.ClassUtilization) && isAssocUnit(f.MetricUnit) {
		class = finops.ClassAssociation
	}
	f.PolicyClass = class

	switch class {
	case finops.ClassAssociation:
		e.evalAssociation(f)
	case finops.ClassRequestRate:
		e.evalRequestRate(f)
	case finops.ClassAttachment:
		e.evalAttachment(f)
	case finops.ClassComputeIdle:
		e.evalComputeIdle(f)
	case finops.ClassStorageIdle:
		e.evalStorageIdle(f)
	case finops.ClassReplicaWaste:
		e.evalReplicaWaste(f)
	case finops.ClassTrafficBytes:
		e.evalTrafficBytes(f)
	default:
		e.evalUtilization(f)
	}
}

// isBytesUnit reports whether the metric unit looks like a traffic/volume measure.
func isBytesUnit(u string) bool {
	u = strings.ToLower(u)
	return strings.Contains(u, "byte") || strings.Contains(u, "bit") || strings.Contains(u, "gib") || strings.Contains(u, "mib") || strings.Contains(u, "gb")
}

// isAssocUnit reports whether the metric unit is a boolean attached/associated flag.
func isAssocUnit(u string) bool {
	u = strings.ToLower(u)
	return u == "bool" || u == "associated" || u == "attached" || u == "in_use"
}

// protectionReason returns why a finding must not be mutated, or ("", false) if OK to act.
func (e *Evaluator) protectionReason(f *finops.Finding) (string, bool) {
	if !f.CreatedAt.IsZero() {
		age := time.Since(f.CreatedAt)
		minAge := time.Duration(e.cfg.Policy.MinAgeHours) * time.Hour
		if age < minAge {
			return fmt.Sprintf("resource younger than %dh", e.cfg.Policy.MinAgeHours), true
		}
	}
	for k, want := range e.cfg.Policy.ExcludeTags {
		if got, ok := f.Tags[k]; ok && strings.EqualFold(got, want) {
			return fmt.Sprintf("excluded by tag %s=%s", k, want), true
		}
	}
	if f.Namespace != "" {
		for _, ns := range e.cfg.Policy.ProtectedNamespaces {
			if f.Namespace == ns {
				return fmt.Sprintf("protected namespace %s", ns), true
			}
		}
	}
	if e.cfg.Remediator.RequireApprovalTag {
		if v, ok := f.Tags["finops-approve"]; !ok || !strings.EqualFold(v, "true") {
			return "missing required tag finops-approve=true", true
		}
	}
	return "", false
}

// monthlyFromRate estimates monthly USD waste as hourly_rate × 730.
func (e *Evaluator) monthlyFromRate(f *finops.Finding) float64 {
	return e.cfg.HourlyRate(string(f.Cloud), string(f.Kind)) * float64(hoursInMonth)
}

// evalTrafficBytes decides shutdown/downsize for gateways measured in bytes/hour.
func (e *Evaluator) evalTrafficBytes(f *finops.Finding) {
	idle := e.cfg.Policy.GatewayIdleBytesPerHour
	low := e.cfg.Policy.GatewayLowBytesPerHour
	f.Threshold = idle
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	switch {
	case f.MetricValue <= idle:
		e.mark(f, finops.ActionShutdown, finops.SeverityCritical,
			fmt.Sprintf("%s on %s averages %.0f %s over %dh (idle ≤ %.0f); shut down",
				f.Kind, f.Cloud, f.MetricValue, f.MetricUnit, f.LookbackHours, idle))
	case f.MetricValue <= low:
		f.Threshold = low
		f.MonthlyWasteUSD *= 0.5
		e.mark(f, finops.ActionDownsize, finops.SeverityHigh,
			fmt.Sprintf("%s on %s averages %.0f %s (low ≤ %.0f); downsize or consolidate",
				f.Kind, f.Cloud, f.MetricValue, f.MetricUnit, low))
	default:
		e.clear(f)
	}
}

// evalAssociation shuts down resources that are unattached / unassociated (metric ≤ 0).
func (e *Evaluator) evalAssociation(f *finops.Finding) {
	f.Threshold = 0
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	if f.MetricValue <= 0 {
		e.mark(f, finops.ActionShutdown, finops.SeverityHigh,
			fmt.Sprintf("unassociated / detached %s on %s accruing charges", f.Kind, f.Cloud))
		return
	}
	e.clear(f)
}

// evalRequestRate shuts down LB/ingress/API pathways below idle RPS.
func (e *Evaluator) evalRequestRate(f *finops.Finding) {
	thr := e.cfg.Policy.GatewayIdleRPS
	f.Threshold = thr
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	if f.MetricValue <= thr {
		e.mark(f, finops.ActionShutdown, finops.SeverityHigh,
			fmt.Sprintf("%s on %s averages %.4f %s (idle ≤ %.4f); shut down unused pathway",
				f.Kind, f.Cloud, f.MetricValue, f.MetricUnit, thr))
		return
	}
	e.clear(f)
}

// evalAttachment handles DRG/TGW/VPN style gateways using attachment counts.
func (e *Evaluator) evalAttachment(f *finops.Finding) {
	f.Threshold = 0
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	if f.MetricValue <= 0 {
		e.mark(f, finops.ActionShutdown, finops.SeverityCritical,
			fmt.Sprintf("%s on %s has %.0f %s; shut down orphaned gateway",
				f.Kind, f.Cloud, f.MetricValue, nonempty(f.MetricUnit, "attachments")))
		return
	}
	if f.MetricValue < float64(e.cfg.Policy.MinGatewayAttachments) {
		f.Threshold = float64(e.cfg.Policy.MinGatewayAttachments)
		f.MonthlyWasteUSD *= 0.4
		e.mark(f, finops.ActionDownsize, finops.SeverityHigh,
			fmt.Sprintf("%s on %s has only %.0f attachments (min %d); consolidate",
				f.Kind, f.Cloud, f.MetricValue, e.cfg.Policy.MinGatewayAttachments))
		return
	}
	e.clear(f)
}

// evalComputeIdle handles VMs, functions, DBs, pods using CPU% or invocation rates.
func (e *Evaluator) evalComputeIdle(f *finops.Finding) {
	thr := e.cfg.Policy.ComputeIdlePercent
	// Absolute invocation/request style metrics use request threshold.
	if strings.Contains(strings.ToLower(f.MetricUnit), "invocation") ||
		strings.Contains(strings.ToLower(f.MetricUnit), "rps") {
		thr = e.cfg.Policy.GatewayIdleRPS
	}
	f.Threshold = thr
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	if f.MetricValue <= thr {
		e.mark(f, finops.ActionShutdown, finops.SeverityCritical,
			fmt.Sprintf("%s on %s utilization %.4f %s ≤ %.4f; shut down idle compute",
				f.Kind, f.Cloud, f.MetricValue, f.MetricUnit, thr))
		return
	}
	if f.MetricValue <= e.cfg.Policy.ComputeLowPercent {
		f.Threshold = e.cfg.Policy.ComputeLowPercent
		f.MonthlyWasteUSD *= 0.5
		e.mark(f, finops.ActionDownsize, finops.SeverityHigh,
			fmt.Sprintf("%s on %s utilization %.4f %s is low; downsize instance/sku",
				f.Kind, f.Cloud, f.MetricValue, f.MetricUnit))
		return
	}
	e.clear(f)
}

// evalStorageIdle handles unattached disks, empty buckets, unused PVCs/snapshots.
func (e *Evaluator) evalStorageIdle(f *finops.Finding) {
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	// Unattached / unused → metric 0 with association-style or explicit idle.
	if isAssocUnit(f.MetricUnit) || f.MetricValue <= 0 {
		f.Threshold = 0
		e.mark(f, finops.ActionShutdown, finops.SeverityHigh,
			fmt.Sprintf("%s on %s is unattached or unused; delete to stop storage charges", f.Kind, f.Cloud))
		return
	}
	// Percent-used style: below threshold means mostly empty / stale.
	if strings.Contains(strings.ToLower(f.MetricUnit), "percent") || f.MetricUnit == "%" {
		f.Threshold = e.cfg.Policy.StorageIdlePercent
		if f.MetricValue <= e.cfg.Policy.StorageIdlePercent {
			e.mark(f, finops.ActionShutdown, finops.SeverityMedium,
				fmt.Sprintf("%s on %s used %.1f%% ≤ %.1f%%; reclaim storage",
					f.Kind, f.Cloud, f.MetricValue, e.cfg.Policy.StorageIdlePercent))
			return
		}
	}
	e.clear(f)
}

// evalReplicaWaste handles Deployments/StatefulSets/etc. by replica count or RPS.
func (e *Evaluator) evalReplicaWaste(f *finops.Finding) {
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	// Metric is either replica count or RPS against a workload.
	unit := strings.ToLower(f.MetricUnit)
	if strings.Contains(unit, "rps") || strings.Contains(unit, "request") {
		f.Threshold = e.cfg.Policy.GatewayIdleRPS
		if f.MetricValue <= e.cfg.Policy.GatewayIdleRPS {
			e.mark(f, finops.ActionShutdown, finops.SeverityHigh,
				fmt.Sprintf("%s on %s serves %.4f %s; scale to zero / delete idle workload",
					f.Kind, f.Cloud, f.MetricValue, f.MetricUnit))
			return
		}
		e.clear(f)
		return
	}
	// Replica count: over-provisioned if above max idle replicas while tagged non-prod handled by tags;
	// treat 0 ready replicas or metric as "active pods" == 0 as shutdown.
	f.Threshold = float64(e.cfg.Policy.MinActiveReplicas)
	if f.MetricValue < float64(e.cfg.Policy.MinActiveReplicas) {
		e.mark(f, finops.ActionShutdown, finops.SeverityHigh,
			fmt.Sprintf("%s on %s has %.0f active replicas (min %d); shut down idle workload",
				f.Kind, f.Cloud, f.MetricValue, e.cfg.Policy.MinActiveReplicas))
		return
	}
	if f.MetricValue > float64(e.cfg.Policy.MaxIdleReplicas) {
		// High replica count with no separate traffic metric — recommend downsize.
		f.MonthlyWasteUSD *= 0.3
		e.mark(f, finops.ActionDownsize, finops.SeverityMedium,
			fmt.Sprintf("%s on %s has %.0f replicas (max idle target %d); downsize",
				f.Kind, f.Cloud, f.MetricValue, e.cfg.Policy.MaxIdleReplicas))
		return
	}
	e.clear(f)
}

// evalUtilization is the catch-all policy for unknown kinds (generic idle threshold).
func (e *Evaluator) evalUtilization(f *finops.Finding) {
	thr := e.cfg.Policy.GenericIdleThreshold
	f.Threshold = thr
	f.MonthlyWasteUSD = e.monthlyFromRate(f)
	if f.MetricValue <= thr {
		e.mark(f, finops.ActionShutdown, finops.SeverityHigh,
			fmt.Sprintf("%s on %s metric %.4f %s ≤ idle threshold %.4f; shut down",
				f.Kind, f.Cloud, f.MetricValue, f.MetricUnit, thr))
		return
	}
	e.clear(f)
}

// mark sets the recommended action, severity, and rationale on a finding.
func (e *Evaluator) mark(f *finops.Finding, action finops.Action, sev finops.Severity, why string) {
	f.Recommended = action
	f.Severity = sev
	f.Rationale = why
}

// clear marks a finding as healthy (ActionNone, zero waste).
func (e *Evaluator) clear(f *finops.Finding) {
	f.Recommended = finops.ActionNone
	f.Severity = finops.SeverityLow
	f.MonthlyWasteUSD = 0
	f.Rationale = "utilization within policy"
}

// nonempty returns s, or fallback when s is empty.
func nonempty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

const hoursInMonth = 730
