// Package server runs the FinOps agent as an HTTP microservice with Prometheus
// metrics for Grafana (shutdowns and per-tenant cost savings).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/engine"
	"github.com/ranganatha76/net-finops-agent/internal/metrics"
	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// Service is the long-running FinOps HTTP microservice.
type Service struct {
	cfg      *config.Config
	eng      *engine.Engine
	metrics  *metrics.Recorder
	log      *slog.Logger
	registry *prometheus.Registry

	mu         sync.RWMutex
	lastReport *finops.ScanReport
	lastError  string
	ready      bool
}

// New constructs a Service. registry may be nil (uses a dedicated registry).
func New(cfg *config.Config, eng *engine.Engine, log *slog.Logger, registry *prometheus.Registry) *Service {
	if log == nil {
		log = slog.Default()
	}
	if registry == nil {
		registry = prometheus.NewRegistry()
	}
	rec := metrics.New(registry)
	return &Service{
		cfg:      cfg,
		eng:      eng,
		metrics:  rec,
		log:      log,
		registry: registry,
	}
}

// Handler returns the HTTP mux with health, metrics, and API routes.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /api/v1/report", s.handleReport)
	mux.HandleFunc("GET /api/v1/tenants/savings", s.handleTenantSavings)
	mux.HandleFunc("POST /api/v1/scan", s.handleScan)
	mux.HandleFunc("POST /api/v1/remediate", s.handleRemediate)
	mux.HandleFunc("GET /", s.handleRoot)
	return mux
}

// Run starts the HTTP server and optional periodic scan/remediate scheduler.
// It blocks until ctx is cancelled, then shuts down gracefully.
func (s *Service) Run(ctx context.Context) error {
	svcCfg := s.cfg.Service
	addr := svcCfg.ListenAddr
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.metrics.ServiceUp.Set(1)
	s.ready = true

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("finops microservice listening", "addr", addr, "metrics", "/metrics")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Initial scan so Grafana has data immediately.
	go func() {
		if err := s.runCycle(ctx, svcCfg.AutoRemediate, svcCfg.DryRun); err != nil {
			s.log.Error("initial cycle failed", "err", err)
		}
	}()

	interval := svcCfg.ScheduleDuration()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.log.Info("scheduler started", "interval", interval.String(), "auto_remediate", svcCfg.AutoRemediate, "dry_run", svcCfg.DryRun)

	for {
		select {
		case <-ctx.Done():
			s.metrics.ServiceUp.Set(0)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return srv.Shutdown(shutdownCtx)
		case err := <-errCh:
			s.metrics.ServiceUp.Set(0)
			return err
		case <-ticker.C:
			if err := s.runCycle(ctx, svcCfg.AutoRemediate, svcCfg.DryRun); err != nil {
				s.log.Error("scheduled cycle failed", "err", err)
			}
		}
	}
}

// runCycle executes scan (and optional remediate), then updates Prometheus metrics.
func (s *Service) runCycle(ctx context.Context, remediate, dryRun bool) error {
	start := time.Now()
	var (
		report *finops.ScanReport
		err    error
	)
	if remediate {
		report, err = s.eng.Remediate(ctx, nil, dryRun)
	} else {
		report, err = s.eng.Scan(ctx)
	}
	s.metrics.ScanDurationSeconds.Observe(time.Since(start).Seconds())
	if err != nil {
		s.metrics.ScanErrorsTotal.Inc()
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		return err
	}

	s.metrics.ObserveScan(report)
	if remediate {
		s.metrics.ObserveRemediations(report)
	}

	s.mu.Lock()
	s.lastReport = report
	s.lastError = ""
	s.mu.Unlock()

	s.log.Info("cycle complete",
		"actionable", report.ActionableCount,
		"protected", report.ProtectedCount,
		"waste_usd", report.TotalWasteUSD,
		"remediations", len(report.Remediations),
		"remediate", remediate,
		"dry_run", dryRun,
	)
	return nil
}

func (s *Service) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Service) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	ready := s.ready
	s.mu.RUnlock()
	if !ready {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (s *Service) handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"service": "net-finops-agent",
		"endpoints": []string{
			"GET  /healthz",
			"GET  /readyz",
			"GET  /metrics",
			"GET  /api/v1/report",
			"GET  /api/v1/tenants/savings",
			"POST /api/v1/scan",
			"POST /api/v1/remediate?dry_run=true|false",
		},
	})
}

func (s *Service) handleReport(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastReport == nil {
		http.Error(w, "no report yet", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.lastReport)
}

// TenantSavings is the JSON view of per-tenant estimated monthly savings.
type TenantSavings struct {
	Tenant              string  `json:"tenant"`
	MonthlyWasteUSD     float64 `json:"monthly_waste_usd"`
	ActionableFindings  int     `json:"actionable_findings"`
	ProtectedFindings   int     `json:"protected_findings"`
	ShutdownCandidates  int     `json:"shutdown_candidates"`
	DownsizeCandidates  int     `json:"downsize_candidates"`
}

func (s *Service) handleTenantSavings(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	report := s.lastReport
	s.mu.RUnlock()
	if report == nil {
		http.Error(w, "no report yet", http.StatusNotFound)
		return
	}

	type agg struct {
		waste, actionable, protected, shutdown, downsize float64
	}
	byTenant := map[string]*agg{}
	for _, f := range report.Findings {
		t := metrics.TenantID(f)
		a := byTenant[t]
		if a == nil {
			a = &agg{}
			byTenant[t] = a
		}
		if f.Protected {
			a.protected++
			continue
		}
		switch f.Recommended {
		case finops.ActionShutdown:
			a.shutdown++
			a.actionable++
			a.waste += f.MonthlyWasteUSD
		case finops.ActionDownsize:
			a.downsize++
			a.actionable++
			a.waste += f.MonthlyWasteUSD
		}
	}

	out := make([]TenantSavings, 0, len(byTenant))
	for tenant, a := range byTenant {
		out = append(out, TenantSavings{
			Tenant:             tenant,
			MonthlyWasteUSD:    a.waste,
			ActionableFindings: int(a.actionable),
			ProtectedFindings:  int(a.protected),
			ShutdownCandidates: int(a.shutdown),
			DownsizeCandidates: int(a.downsize),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"generated_at": report.GeneratedAt,
		"tenants":      out,
	})
}

func (s *Service) handleScan(w http.ResponseWriter, r *http.Request) {
	if err := s.runCycle(r.Context(), false, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.handleReport(w, r)
}

func (s *Service) handleRemediate(w http.ResponseWriter, r *http.Request) {
	dryRun := true
	if r.URL.Query().Get("dry_run") == "false" {
		dryRun = false
	}
	if err := s.runCycle(r.Context(), true, dryRun); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.handleReport(w, r)
}

// LastError returns the last cycle error string (for tests/ops).
func (s *Service) LastError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastError
}

// String helper for logs.
func (s *Service) String() string {
	return fmt.Sprintf("finops-service(%s)", s.cfg.Service.ListenAddr)
}
