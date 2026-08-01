# Net FinOps Agent

Go automation engine that **discovers underutilized cloud and Kubernetes resources** and recommends (or applies) **shutdown / downsize** actions to cut waste from idle gateways, forgotten lab environments, orphan disks, unused workloads, and more.

> **Problem it addresses:** Engineering teams spinning up Docker/Kubernetes and cloud lab environments rarely clean up infrastructure. That leaves billable NAT/IGW/SGW/DRG/TGW gateways, VMs, disks, databases, LoadBalancers, Ingresses, and Deployments running — driving large cloud bills (especially egress and fixed hourly gateway charges).

**Docs:** [README](README.md) · **[Design](design.md)** · **[Config usage](configs/README.md)**


---

## Table of contents

1. [Features](#features)
2. [Architecture](#architecture)
3. [Microservice + Grafana](#microservice--grafana)
4. [Resource coverage](#resource-coverage)
5. [Policy classes](#policy-classes)
6. [Requirements](#requirements)
7. [Quick start](#quick-start)
8. [CLI reference](#cli-reference)
9. [Configuration](#configuration)
10. [How to test](#how-to-test)
11. [Adding a cloud provider](#adding-a-cloud-provider)
12. [Safety & guardrails](#safety--guardrails)
13. [Project layout](#project-layout)
14. [Troubleshooting](#troubleshooting)

---

## Features

- **Any CSP** — AWS, Azure, GCP, OCI, Alibaba, DigitalOcean, IBM, or a custom cloud via registry / inventory file
- **Any resource kind** — network gateways, compute, storage, data services, and full Kubernetes object set
- **Policy-driven** — idle thresholds for traffic, CPU, storage, replicas, attachments, request rate
- **Cost estimates** — monthly USD waste from configurable hourly rates
- **Dry-run by default** — no live deletes unless you pass `--apply`
- **Protection rules** — production tags, `finops=keep`, min age, protected namespaces
- **Demo mode** — realistic multi-cloud inventory so you can test without credentials
- **Reports** — table, JSON, or Markdown
- **Microservice mode** — long-running HTTP service with Prometheus metrics and Grafana dashboards (shutdowns + cost savings per tenant)

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  CLI: scan | remediate | estimate | serve | version                │
│  serve → HTTP microservice (scheduler + /metrics + JSON API)       │
└────────────────────────────┬─────────────────────────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────────┐
│                         Engine                                     │
│         Scan providers → Evaluate policy → Remediate               │
└───────┬────────────────────┬────────────────────┬────────────────┘
        │                    │                    │
        ▼                    ▼                    ▼
   Provider              Policy            Metrics → Prometheus
   registry           (kind + metric)      → Grafana (per-tenant)
```

**Flow**

1. **Scan** — each enabled provider returns `Finding`s (resource + utilization metric)
2. **Classify** — kind + metric unit → policy class (`compute_idle`, `traffic_bytes`, …)
3. **Evaluate** — thresholds, tags, age, namespaces → `shutdown` / `downsize` / `notify` / `none`
4. **Remediate** (optional) — dry-run or apply via provider mutators
5. **Report / metrics** — actionable waste, protected items, estimated $/month; service mode also emits Prometheus series for Grafana

---

## Microservice + Grafana

Run continuously instead of one-shot CLI:

```bash
# Local microservice (Prometheus scrape target on :8080/metrics)
make run-serve

# Full stack: agent + Prometheus + Grafana
make compose-up
# Grafana  http://localhost:3000  (admin / admin)
# Prometheus http://localhost:9090
# Agent API  http://localhost:8080
```

| Endpoint | Purpose |
|----------|---------|
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness |
| `GET /metrics` | Prometheus scrape (Grafana datasource) |
| `GET /api/v1/report` | Latest scan JSON |
| `GET /api/v1/tenants/savings` | Per-tenant monthly waste + shutdown candidates |
| `POST /api/v1/scan` | Trigger scan now |
| `POST /api/v1/remediate?dry_run=true\|false` | Trigger remediate |

**Tenant label** (for Grafana series): finding tag `tenant` → else `AccountOrSub` → else cloud id.

**Key metrics**

| Metric | Meaning |
|--------|---------|
| `finops_resources_shutdown_total{tenant,cloud,kind,action,dry_run}` | Count of shutdown/downsize actions |
| `finops_cost_savings_usd_total{tenant,...}` | Cumulative USD from **live** applies only |
| `finops_cost_savings_usd_monthly{tenant}` | Latest estimated monthly waste per tenant |
| `finops_findings_actionable{tenant,cloud,kind}` | Latest actionable finding counts |

Pre-provisioned dashboard: **Net FinOps Agent — Tenant Savings & Shutdowns** (under FinOps folder in Grafana).

Service knobs live under `service:` in `configs/config.yaml` (`listen_addr`, `schedule_interval`, `auto_remediate`, `dry_run`).

---

## Resource coverage

### Cloud (examples)

| Domain | Kinds |
|--------|--------|
| Network / gateways | `nat_gateway`, `internet_gateway` (`igw`), `service_gateway` (`sgw`), `dynamic_routing_gateway` (`drg`), `transit_gateway` (`tgw`), `vpn_gateway` (`vgw`), `express_route_gateway`, `network_firewall`, `application_gateway`, `private_endpoint`, `public_ip`, `load_balancer` |
| Compute | `instance` / `vm` / `ec2`, `function` / `lambda`, `container_app`, `batch_job`, `spot_instance`, `dedicated_host` |
| Storage | `disk` / `ebs`, `snapshot`, `image`, `file_share`, `object_bucket` / `s3`, `backup_vault` |
| Data | `database` / `rds`, `cache`, `queue`, `stream`, `search_cluster`, `warehouse` |
| Custom | any string — still evaluated via metric unit / `policy_class` |

### Kubernetes

| Kind | Description |
|------|-------------|
| `ingress`, `gateway_api` | HTTP routing |
| `service_loadbalancer`, `service_nodeport`, `service_clusterip` | Services |
| `ingress_controller`, `api_gateway` | Controllers / Kong / Envoy / Traefik |
| `deployment`, `statefulset`, `daemonset`, `replicaset` | Workloads |
| `job`, `cronjob`, `pod` | Batch / pods |
| `persistentvolumeclaim`, `persistentvolume` | Storage |
| `horizontalpodautoscaler`, `namespace` | Scaling / tenancy |

---

## Policy classes

| Class | Typical metric | Action when idle |
|-------|----------------|------------------|
| `traffic_bytes` | bytes/hour | shutdown / downsize |
| `association` | attached / bool | shutdown if unattached |
| `request_rate` | rps / qps | shutdown |
| `attachment` | attachment count | shutdown if 0; downsize if sparse |
| `compute_idle` | CPU % / invocations | shutdown / downsize |
| `storage_idle` | attached / % used | shutdown |
| `replica_waste` | replicas / rps | shutdown / downsize |
| `utilization` | generic 0–N | shutdown below threshold |

Inventory items may set `policy_class` explicitly to override auto-classification.

---

## Requirements

- **Go 1.22+** (developed with Go 1.26)
- No cloud credentials required for **demo** mode
- Optional: cloud SDKs / kubeconfig when wiring **live** adapters

---

## Quick start

```bash
cd net-finops-agent

# Build
make build
# or: go build -o bin/net-finops-agent ./cmd/net-finops-agent

# Scan demo inventory (no cloud credentials)
./bin/net-finops-agent scan --mode demo --config configs/config.yaml

# Estimated monthly waste
./bin/net-finops-agent estimate --mode demo --config configs/config.yaml

# Remediate in dry-run (safe — prints what would be deleted/downsized)
./bin/net-finops-agent remediate --mode demo --config configs/config.yaml
```

Expected demo ballpark: **~40+ actionable findings** and **hundreds of USD/month** estimated waste across AWS, Azure, GCP, OCI, and Kubernetes.

---

## CLI reference

```text
net-finops-agent [command]

Commands:
  scan        Scan and report underutilized resources (no changes)
  remediate   Scan + remediate (dry-run unless --apply)
  estimate    Print estimated monthly savings only
  serve       Run HTTP microservice (Prometheus metrics for Grafana)
  version     Print version

Global flags:
  --config string   Config YAML path (default: configs/config.yaml)
  --mode string     demo | live (default: demo)
  --format string   table | json | markdown (default: table)
  --output string   Write report to file
  -v, --verbose     Debug logging
```

### `serve` flags

```text
  --addr string       Listen address (default from service.listen_addr)
  --interval string   Rescan interval e.g. 15m
  --auto-remediate    Remediate after each scheduled scan
  --apply             Live remediations when auto-remediating (else dry-run)
```

### Examples

```bash
# Human-readable table
./bin/net-finops-agent scan --mode demo --format table

# Machine-readable JSON
./bin/net-finops-agent scan --mode demo --format json --output reports/scan.json

# Markdown brief for Confluence / PR description
./bin/net-finops-agent scan --mode demo --format markdown --output reports/scan.md

# Dry-run remediation
./bin/net-finops-agent remediate --mode demo

# Live apply (demo mutators only record deletes; real CSPs need SDK wiring)
./bin/net-finops-agent remediate --mode demo --apply

# Microservice + scrape target for Grafana/Prometheus
./bin/net-finops-agent serve --mode demo --addr :8080 --interval 1m
curl -s localhost:8080/api/v1/tenants/savings | jq .
curl -s localhost:8080/metrics | grep finops_cost_savings
```

---

## Configuration

Primary file: [`configs/config.yaml`](configs/config.yaml)  
**How each key is used:** [`configs/README.md`](configs/README.md) (section → code map)  
Inventory format: [`configs/inventory.example.yaml`](configs/inventory.example.yaml)

### Scan

```yaml
scan:
  lookback_hours: 168   # utilization window (7 days)
```

### Policy thresholds

```yaml
policy:
  gateway_idle_bytes_per_hour: 1048576   # 1 MiB/h → idle gateway
  gateway_low_bytes_per_hour: 10485760   # → downsize candidate
  gateway_idle_rps: 0.01
  min_gateway_attachments: 2
  compute_idle_percent: 5
  compute_low_percent: 20
  storage_idle_percent: 5
  min_active_replicas: 1
  max_idle_replicas: 3
  generic_idle_threshold: 0.01
  min_age_hours: 24                      # skip brand-new resources
  exclude_tags:
    finops: "keep"
    environment: "production"
  protected_namespaces:
    - kube-system
    - kube-public
    - ingress-nginx
```

### Providers

```yaml
providers:
  aws:
    enabled: true
    account: demo-aws
    regions: [ap-south-1, us-east-1]
  kubernetes:
    enabled: true
    account: blr-dev-cluster
  # Inventory-backed CSP (no native SDK required):
  digitalocean:
    enabled: true
    options:
      inventory_file: configs/inventory.example.yaml
```

### Remediator

```yaml
remediator:
  dry_run: true              # config hint; CLI --apply overrides for a run
  max_actions_per_run: 100   # blast-radius cap
  require_approval_tag: false  # if true, need finops-approve=true
```

### Inventory file format

See [`configs/inventory.example.yaml`](configs/inventory.example.yaml) for field→Finding mapping and `policy_class` usage.
---

## How to test

### 1. Unit tests (recommended first step)

```bash
cd net-finops-agent

# All packages
go test ./... -count=1

# Or via Make
make test

# With race detector
go test ./... -race -count=1

# Verbose + coverage
go test ./... -count=1 -coverprofile=coverage.out
go tool cover -func=coverage.out | tail -20
```

**What the tests cover**

| Package | Focus |
|---------|--------|
| `internal/config` | YAML load, rate fallbacks, provider map |
| `internal/policy` | NAT/DRG/compute/storage/K8s/custom kinds, production protection |
| `internal/engine` | End-to-end demo scan + dry-run remediate |
| `internal/provider/aws` | Demo inventory returns gateways + compute/storage |

### 2. Lint / vet

```bash
make lint
# or
go vet ./...
```

### 3. Build the binary

```bash
make build
./bin/net-finops-agent version
# → net-finops-agent v0.1.0
```

### 4. Manual demo smoke tests

```bash
# A) Scan — should list actionable + protected findings
make run-scan
# Expect: Actionable > 0, Protected > 0, Est. monthly waste > $0

# B) Estimate — one-line savings summary
make run-estimate

# C) Remediate dry-run — should show [ok/dry-run] lines, no errors
make run-remediate

# D) JSON output (for scripting / CI artifacts)
mkdir -p reports
./bin/net-finops-agent scan --mode demo --format json --output reports/demo-scan.json
test -s reports/demo-scan.json && echo "JSON OK"
```

### 5. Verify protection rules

In the scan table you should see rows with `STATUS` like:

- `protected:excluded by tag environment=production`
- `protected:excluded by tag finops=keep`
- `protected:resource younger than 24h`
- `protected:protected namespace ingress-nginx`

Those must **not** appear under Remediations as live deletes.

### 6. Verify multi-cloud coverage

```bash
./bin/net-finops-agent scan --mode demo --format json \
  | grep -o '"cloud": "[^"]*"' | sort | uniq -c
```

You should see at least: `aws`, `azure`, `gcp`, `oci`, `kubernetes`.

### 7. Test a custom inventory file

```bash
# Enable digitalocean (or any generic id) pointing at the example inventory
# Edit configs/config.yaml temporarily, or copy:

cat > /tmp/finops-test.yaml <<'EOF'
scan:
  lookback_hours: 168
policy:
  compute_idle_percent: 5
  min_age_hours: 1
  exclude_tags:
    environment: production
  protected_namespaces: [kube-system]
providers:
  digitalocean:
    enabled: true
    options:
      inventory_file: configs/inventory.example.yaml
remediator:
  dry_run: true
  max_actions_per_run: 50
EOF

./bin/net-finops-agent scan --mode demo --config /tmp/finops-test.yaml --format table
```

### 8. Test remediation apply (demo mutators only)

```bash
# Still safe: demo backends record deletes in-memory / log; no real cloud APIs
./bin/net-finops-agent remediate --mode demo --apply -v
```

### 9. One-shot CI-style script

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
go test ./... -count=1
go vet ./...
go build -o bin/net-finops-agent ./cmd/net-finops-agent
./bin/net-finops-agent estimate --mode demo --config configs/config.yaml | grep -q "Estimated monthly"
./bin/net-finops-agent scan --mode demo --config configs/config.yaml --format json \
  | grep -q '"actionable_count"'
echo "All checks passed"
```

---

## Adding a cloud provider

### Option A — Inventory YAML (fastest)

1. Export resources to the normalized inventory schema  
2. Enable in config:

```yaml
providers:
  mycloud:
    enabled: true
    account: "my-account"
    options:
      inventory_file: path/to/inventory.yaml
```

`oci`, `alibaba`, `digitalocean`, `ibm`, and `generic` are already registered as inventory-backed factories.

### Option B — Native Go adapter

1. Implement `provider.Scanner` and `provider.Remediator`
2. Register in `init()`:

```go
registry.Register("mycloud", func(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error) {
    return mycloud.NewScanner(...), mycloud.NewRemediator(...), nil
})
```

3. Enable `providers.mycloud.enabled: true`

See `internal/provider/live/doc.go` and existing packages under `internal/provider/{aws,azure,gcp,k8s,generic}`.

> **Live mode today:** `--mode=live` expects SDK adapters to be wired. Demo mode is fully functional for evaluation and tests.

---

## Safety & guardrails

| Guardrail | Behavior |
|-----------|----------|
| Dry-run default | No mutations unless `--apply` |
| `min_age_hours` | Skip resources younger than N hours |
| Exclude tags | e.g. `environment=production`, `finops=keep` |
| Protected namespaces | e.g. `kube-system`, `ingress-nginx` |
| `max_actions_per_run` | Caps remediations per execution |
| `require_approval_tag` | Optional: only act if `finops-approve=true` |

**Recommended rollout**

1. `scan` in demo → validate policy  
2. Point inventory / live adapters at **non-prod**  
3. `remediate` dry-run → review report  
4. Enable `--apply` with low `max_actions_per_run`  
5. Expand to production only with strict exclude tags

---

## Project layout

```text
net-finops-agent/
├── cmd/net-finops-agent/     # CLI + serve entrypoint (cobra)
├── configs/
│   ├── config.yaml          # Default multi-cloud policy + service:
│   └── inventory.example.yaml
├── deploy/
│   ├── docker-compose.yml   # agent + Prometheus + Grafana
│   ├── Dockerfile
│   ├── prometheus/
│   └── grafana/             # datasource + tenant savings dashboard
├── internal/
│   ├── config/              # YAML config + rate table
│   ├── engine/              # Orchestration
│   ├── metrics/             # Prometheus (tenant shutdowns & savings)
│   ├── policy/              # Classification & thresholds
│   ├── provider/
│   │   ├── aws|azure|gcp|k8s|generic/
│   │   ├── live/            # Factory registration
│   │   └── registry/        # CSP plugin registry
│   ├── report/              # table / json / markdown
│   └── server/              # HTTP microservice + scheduler
├── pkg/finops/              # Public types & kind catalog
├── Makefile
├── go.mod
└── README.md
```

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `config not found; using defaults` | Run from repo root or pass `--config configs/config.yaml` |
| `provider "X" is enabled but no factory is registered` | Register via `registry.Register` or use an inventory-backed id (`generic`, `oci`, …) |
| `live mode: … not wired yet` | Use `--mode=demo`, or implement SDK clients for that provider |
| No actionable findings | Lower thresholds in `policy`, ensure demo providers are `enabled: true`, check exclude tags |
| Too many remediations | Lower `max_actions_per_run`; tighten exclude tags / protected namespaces |
| Tests fail on package name | Ensure `internal/provider/k8s` files all use `package k8s` |

---

## License

Internal / project use — adjust as needed for distribution.
