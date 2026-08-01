// Package report formats ScanReport output as JSON, table, or Markdown.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// WriteJSON emits the full scan report as indented JSON to w.
func WriteJSON(w io.Writer, r *finops.ScanReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteTable emits a human-readable summary table, remediations, and top rationales.
func WriteTable(w io.Writer, r *finops.ScanReport) error {
	fmt.Fprintf(w, "Net FinOps Agent report  generated=%s  lookback=%dh  dry_run=%v\n",
		r.GeneratedAt.Format("2006-01-02T15:04:05Z"), r.LookbackHours, r.DryRun)
	fmt.Fprintf(w, "Actionable=%d  Protected=%d  Est. monthly waste=$%.2f\n\n",
		r.ActionableCount, r.ProtectedCount, r.TotalWasteUSD)

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tACTION\tCLOUD\tKIND\tNAME\tREGION\tMETRIC\t$/MO\tSTATUS")
	for _, f := range r.Findings {
		if f.Recommended == finops.ActionNone {
			continue
		}
		status := "actionable"
		if f.Protected {
			status = "protected:" + f.ProtectedReason
		}
		metric := fmt.Sprintf("%.4g %s", f.MetricValue, f.MetricUnit)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%.2f\t%s\n",
			f.Severity, f.Recommended, f.Cloud, f.Kind, truncate(f.Name, 28), f.Region, metric, f.MonthlyWasteUSD, status)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(r.Remediations) > 0 {
		fmt.Fprintln(w, "\nRemediations:")
		for _, rem := range r.Remediations {
			state := "ok"
			if !rem.Success {
				state = "FAIL"
			}
			mode := "live"
			if rem.DryRun {
				mode = "dry-run"
			}
			fmt.Fprintf(w, "  [%s/%s] %s %s — %s (save ~$%.2f/mo)\n",
				state, mode, rem.Action, rem.FindingID, rem.Message, rem.SavedUSDEst)
		}
	}

	fmt.Fprintln(w, "\nTop rationales:")
	n := 0
	for _, f := range r.Findings {
		if f.Recommended == finops.ActionNone || f.Protected {
			continue
		}
		fmt.Fprintf(w, "  • %s: %s\n", f.ID, f.Rationale)
		n++
		if n >= 8 {
			break
		}
	}
	return nil
}

// truncate shortens s to at most n runes, appending an ellipsis when truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// WriteMarkdown emits a short markdown savings brief suitable for tickets/docs.
func WriteMarkdown(w io.Writer, r *finops.ScanReport) error {
	fmt.Fprintf(w, "# FinOps Network Scan\n\n")
	fmt.Fprintf(w, "- Generated: `%s`\n", r.GeneratedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(w, "- Lookback: **%d hours**\n", r.LookbackHours)
	fmt.Fprintf(w, "- Estimated monthly waste: **$%.2f**\n", r.TotalWasteUSD)
	fmt.Fprintf(w, "- Actionable findings: **%d** (protected: %d)\n\n", r.ActionableCount, r.ProtectedCount)
	fmt.Fprintf(w, "| Severity | Action | Cloud | Resource | Region | $/mo |\n")
	fmt.Fprintf(w, "|----------|--------|-------|----------|--------|------|\n")
	for _, f := range r.Findings {
		if f.Protected || f.Recommended == finops.ActionNone {
			continue
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %.2f |\n",
			f.Severity, f.Recommended, f.Cloud, escapeMD(f.Name), f.Region, f.MonthlyWasteUSD)
	}
	return nil
}

// escapeMD escapes pipe characters so names are safe inside markdown tables.
func escapeMD(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
