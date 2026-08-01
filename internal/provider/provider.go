// Package provider defines the Scanner and Remediator interfaces every CSP adapter implements.
package provider

import (
	"context"

	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// Scanner discovers resources and utilization metrics in one cloud / platform plane.
type Scanner interface {
	// Name returns a short provider id (e.g. "aws").
	Name() string
	// Cloud returns the CSP id stamped on findings.
	Cloud() finops.Cloud
	// Scan lists resources and returns raw findings (policy not yet applied).
	Scan(ctx context.Context) ([]finops.Finding, error)
}

// Remediator applies (or dry-runs) shutdown/downsize actions against findings it Supports.
type Remediator interface {
	// Name returns a short remediator id (usually same as the scanner).
	Name() string
	// Supports reports whether this remediator can act on the finding's cloud/kind.
	Supports(f finops.Finding) bool
	// Apply performs or simulates the action and returns a RemediationResult.
	Apply(ctx context.Context, f finops.Finding, action finops.Action, dryRun bool) (finops.RemediationResult, error)
}
