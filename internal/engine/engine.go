// Package engine orchestrates multi-cloud scan → policy evaluation → optional remediation.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/policy"
	"github.com/ranganatha76/net-finops-agent/internal/provider"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// Engine runs providers, evaluates findings with policy, and optionally remediates.
type Engine struct {
	cfg         *config.Config         // thresholds, rates, remediator caps
	eval        *policy.Evaluator      // protection + classify + action
	scanners    []provider.Scanner     // one per enabled CSP
	remediators []provider.Remediator  // matched to findings by Supports()
	log         *slog.Logger
}

// New constructs an Engine. If log is nil, slog.Default() is used.
func New(cfg *config.Config, scanners []provider.Scanner, remediators []provider.Remediator, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		cfg:         cfg,
		eval:        policy.New(cfg),
		scanners:    scanners,
		remediators: remediators,
		log:         log,
	}
}

// Scan runs all scanners, evaluates each finding, sorts by monthly waste, and
// returns an aggregated ScanReport. It does not mutate cloud resources.
func (e *Engine) Scan(ctx context.Context) (*finops.ScanReport, error) {
	var findings []finops.Finding
	for _, s := range e.scanners {
		e.log.Info("scanning", "provider", s.Name(), "cloud", s.Cloud())
		got, err := s.Scan(ctx)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", s.Name(), err)
		}
		for i := range got {
			e.eval.Evaluate(&got[i]) // enrich with action, cost, protection
			findings = append(findings, got[i])
		}
	}

	// Biggest estimated savings first for operator focus and remediate order.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].MonthlyWasteUSD > findings[j].MonthlyWasteUSD
	})

	report := &finops.ScanReport{
		GeneratedAt:   time.Now().UTC(),
		LookbackHours: e.cfg.Scan.LookbackHours,
		DryRun:        e.cfg.Remediator.DryRun,
		Findings:      findings,
	}
	for _, f := range findings {
		if f.Protected {
			report.ProtectedCount++
			continue
		}
		if f.Recommended == finops.ActionShutdown || f.Recommended == finops.ActionDownsize {
			report.ActionableCount++
			report.TotalWasteUSD += f.MonthlyWasteUSD
		}
	}
	return report, nil
}

// Remediate scans (if report is nil) then applies recommended actions for
// unprotected actionable findings, respecting max_actions_per_run and dryRun.
func (e *Engine) Remediate(ctx context.Context, report *finops.ScanReport, dryRun bool) (*finops.ScanReport, error) {
	if report == nil {
		var err error
		report, err = e.Scan(ctx)
		if err != nil {
			return nil, err
		}
	}
	report.DryRun = dryRun
	max := e.cfg.Remediator.MaxActionsPerRun
	acted := 0

	for _, f := range report.Findings {
		if acted >= max {
			e.log.Warn("max actions reached", "max", max)
			break
		}
		if f.Protected || f.Recommended == finops.ActionNone || f.Recommended == finops.ActionNotify {
			continue
		}
		r := e.remediatorFor(f)
		if r == nil {
			e.log.Warn("no remediator", "finding", f.ID, "cloud", f.Cloud)
			continue
		}
		res, err := r.Apply(ctx, f, f.Recommended, dryRun)
		if err != nil {
			e.log.Error("remediation failed", "finding", f.ID, "err", err)
			report.Remediations = append(report.Remediations, res)
			continue
		}
		report.Remediations = append(report.Remediations, res)
		acted++
		e.log.Info("remediation", "finding", f.ID, "action", f.Recommended, "dry_run", dryRun, "msg", res.Message)
	}
	return report, nil
}

// remediatorFor returns the first remediator that Supports the finding's cloud/kind.
func (e *Engine) remediatorFor(f finops.Finding) provider.Remediator {
	for _, r := range e.remediators {
		if r.Supports(f) {
			return r
		}
	}
	return nil
}
