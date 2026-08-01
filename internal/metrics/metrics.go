// Package metrics exposes Prometheus metrics for Grafana dashboards:
// resources shut down and estimated cost savings per tenant.
package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

const namespace = "finops"

// Recorder holds Prometheus collectors for the FinOps microservice.
type Recorder struct {
	// ResourcesShutdownTotal counts remediated resources (shutdown/downsize).
	ResourcesShutdownTotal *prometheus.CounterVec
	// CostSavingsUSDTotal accumulates estimated monthly USD saved when actions succeed.
	CostSavingsUSDTotal *prometheus.CounterVec
	// CostSavingsUSDMonthly is the latest estimated monthly waste (actionable) per tenant.
	CostSavingsUSDMonthly *prometheus.GaugeVec
	// FindingsActionable is the latest actionable finding count per tenant/cloud/kind.
	FindingsActionable *prometheus.GaugeVec
	// FindingsProtected is the latest protected finding count per tenant.
	FindingsProtected *prometheus.GaugeVec
	// ScanDurationSeconds observes scan/remediate cycle latency.
	ScanDurationSeconds prometheus.Histogram
	// ScanErrorsTotal counts failed scan cycles.
	ScanErrorsTotal prometheus.Counter
	// LastScanTimestamp is unix seconds of the last successful scan.
	LastScanTimestamp prometheus.Gauge
	// ServiceUp is 1 while the HTTP server is serving.
	ServiceUp prometheus.Gauge
}

// New registers default Prometheus metrics under the finops_ namespace.
func New(reg prometheus.Registerer) *Recorder {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	factory := promauto.With(reg)

	return &Recorder{
		ResourcesShutdownTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "resources_shutdown_total",
			Help:      "Total resources shut down or downsized by the FinOps agent, labeled by tenant.",
		}, []string{"tenant", "cloud", "kind", "action", "dry_run"}),

		CostSavingsUSDTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cost_savings_usd_total",
			Help:      "Cumulative estimated monthly USD savings from successful remediations, by tenant.",
		}, []string{"tenant", "cloud", "kind", "action"}),

		CostSavingsUSDMonthly: factory.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cost_savings_usd_monthly",
			Help:      "Latest estimated monthly USD waste (actionable findings) per tenant after a scan.",
		}, []string{"tenant"}),

		FindingsActionable: factory.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "findings_actionable",
			Help:      "Latest count of actionable findings per tenant, cloud, and kind.",
		}, []string{"tenant", "cloud", "kind"}),

		FindingsProtected: factory.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "findings_protected",
			Help:      "Latest count of protected (non-mutable) findings per tenant.",
		}, []string{"tenant"}),

		ScanDurationSeconds: factory.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "scan_duration_seconds",
			Help:      "Duration of scan or remediate cycles in seconds.",
			Buckets:   prometheus.DefBuckets,
		}),

		ScanErrorsTotal: factory.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scan_errors_total",
			Help:      "Total number of failed scan/remediate cycles.",
		}),

		LastScanTimestamp: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scan_timestamp_seconds",
			Help:      "Unix timestamp of the last successful scan cycle.",
		}),

		ServiceUp: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "service_up",
			Help:      "1 if the FinOps microservice is up and serving.",
		}),
	}
}

// TenantID resolves a stable tenant label from a finding (tag tenant → account → cloud).
func TenantID(f finops.Finding) string {
	if f.Tags != nil {
		if t := f.Tags["tenant"]; t != "" {
			return t
		}
		if t := f.Tags["Tenant"]; t != "" {
			return t
		}
	}
	if f.AccountOrSub != "" {
		return f.AccountOrSub
	}
	if f.Cloud != "" {
		return string(f.Cloud)
	}
	return "unknown"
}

// ObserveScan updates gauges from a completed scan report (no remediations required).
func (r *Recorder) ObserveScan(report *finops.ScanReport) {
	if r == nil || report == nil {
		return
	}
	r.FindingsActionable.Reset()
	r.FindingsProtected.Reset()
	r.CostSavingsUSDMonthly.Reset()

	tenantWaste := map[string]float64{}
	tenantProtected := map[string]float64{}
	actionable := map[string]float64{} // key: tenant|cloud|kind

	for _, f := range report.Findings {
		tenant := TenantID(f)
		if f.Protected {
			tenantProtected[tenant]++
			continue
		}
		if f.Recommended == finops.ActionShutdown || f.Recommended == finops.ActionDownsize {
			key := tenant + "\x00" + string(f.Cloud) + "\x00" + string(f.Kind)
			actionable[key]++
			tenantWaste[tenant] += f.MonthlyWasteUSD
		}
	}
	for key, n := range actionable {
		parts := split3(key)
		r.FindingsActionable.WithLabelValues(parts[0], parts[1], parts[2]).Set(n)
	}
	for tenant, n := range tenantProtected {
		r.FindingsProtected.WithLabelValues(tenant).Set(n)
	}
	for tenant, usd := range tenantWaste {
		r.CostSavingsUSDMonthly.WithLabelValues(tenant).Set(usd)
	}
	r.LastScanTimestamp.Set(float64(report.GeneratedAt.Unix()))
}

// split3 splits key on NUL into exactly 3 parts.
func split3(key string) [3]string {
	var out [3]string
	i, start := 0, 0
	for p := 0; p < len(key) && i < 2; p++ {
		if key[p] == 0 {
			out[i] = key[start:p]
			i++
			start = p + 1
		}
	}
	out[i] = key[start:]
	return out
}

// ObserveRemediation records counters for each successful remediation result.
func (r *Recorder) ObserveRemediation(f finops.Finding, res finops.RemediationResult) {
	if r == nil || !res.Success {
		return
	}
	if res.Action != finops.ActionShutdown && res.Action != finops.ActionDownsize {
		return
	}
	tenant := TenantID(f)
	dry := strconv.FormatBool(res.DryRun)
	r.ResourcesShutdownTotal.WithLabelValues(
		tenant, string(f.Cloud), string(f.Kind), string(res.Action), dry,
	).Inc()
	// Only count USD savings for live (non dry-run) applies, so Grafana "savings" is real.
	// Dry-run still increments resources_shutdown_total with dry_run=true for visibility.
	if !res.DryRun {
		r.CostSavingsUSDTotal.WithLabelValues(
			tenant, string(f.Cloud), string(f.Kind), string(res.Action),
		).Add(res.SavedUSDEst)
	}
}

// ObserveRemediations walks the report remediations and matching findings to emit counters.
func (r *Recorder) ObserveRemediations(report *finops.ScanReport) {
	if r == nil || report == nil {
		return
	}
	byID := map[string]finops.Finding{}
	for _, f := range report.Findings {
		byID[f.ID] = f
	}
	for _, res := range report.Remediations {
		f, ok := byID[res.FindingID]
		if !ok {
			f = finops.Finding{ID: res.FindingID, AccountOrSub: "unknown", Cloud: "unknown", Kind: "unknown"}
		}
		r.ObserveRemediation(f, res)
	}
}
