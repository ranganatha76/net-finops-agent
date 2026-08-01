package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/engine"
	"github.com/ranganatha76/net-finops-agent/internal/provider/live"
	"github.com/ranganatha76/net-finops-agent/internal/server"
)

func TestAPIEndpoints(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.ListenAddr = ":0"
	scanners, remediators, err := live.Build(cfg, live.ModeDemo)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(cfg, scanners, remediators, nil)
	svc := server.New(cfg, eng, nil, nil)
	h := svc.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scan", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("scan status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/tenants/savings", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("savings status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	tenants, ok := body["tenants"].([]any)
	if !ok || len(tenants) == 0 {
		t.Fatalf("expected tenants, got %#v", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("metrics status=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "finops_cost_savings_usd_monthly") {
		t.Fatal("missing finops_cost_savings_usd_monthly metric")
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatal("healthz")
	}
}
