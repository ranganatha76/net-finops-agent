# Net FinOps Agent — Design Document

This document describes the **system design**, **component responsibilities**, and a **step-by-step explanation** of how a scan/remediation run works — including **which APIs identify usage** (Step 3) and **how idle resources are detected** (Steps 5–7).

For operator usage and testing, see [`README.md`](README.md).

---

## 1. Goals

| Goal | Design implication |
|------|-------------------|
| Cut waste from idle cloud & K8s resources | Continuous discover → score → act loop |
| Work on **any** CSP | Plugin registry; no hardcoded AWS/Azure-only paths |
| Work on **any** resource kind | Free-form `kind` + policy **classes**, not per-vendor switches |
| Safe by default | Dry-run, tags, age, namespaces, action caps |
| Testable without credentials | Demo inventories behind the same interfaces as live adapters |

### Non-goals (current version)

- Full live SDK implementations for every CSP API (interfaces + demo are in place; live wiring is incremental)
- Automatic rightsizing ML / forecasting
- Billing invoice ingestion (cost rates are configurable estimates, not CUR/Cost Export parsing)

---

## 2. Design principles

1. **CSP-agnostic core** — Engine and policy never `switch` on a closed cloud enum for business logic.
2. **Normalize early** — Vendor resources become `finops.Finding` as soon as they leave a provider.
3. **Classify, then evaluate** — Kind + metric unit → policy class → thresholds → action.
4. **Protect before act** — Guardrails short-circuit evaluation to `notify` / skip remediation.
5. **Interfaces at the edge** — `Scanner` / `Remediator` / inventory & metrics clients isolate SDKs.
6. **Fail loud on misconfig** — Enabled provider without a registered factory → error.

---

## 3. High-level architecture

```
                    ┌──────────────────────────────────────┐
                    │           CLI (cobra)                 │
                    │ scan|remediate|estimate|serve|version │
                    └──────────────────┬───────────────────┘
                                       │
              serve ───────────────────┼───────────────────┐
                                       │                   │
                    ┌──────────────────▼───────────────────┐
                    │         live.Build(config, mode)      │
                    │     registry → Scanner[] Remediator[] │
                    └──────────────────┬───────────────────┘
                                       │
                    ┌──────────────────▼───────────────────┐
                    │              Engine                   │
                    │  Scan() / Remediate()                 │
                    └─┬──────────────┬──────────────┬──────┘
                      │              │              │
           ┌──────────▼──┐  ┌────────▼──────┐  ┌───▼────────┐
           │  Providers  │  │    Policy     │  │   Report   │
           │ aws azure   │  │  Evaluator    │  │ table/json │
           │ gcp oci k8s │  │  Classify +   │  │  markdown  │
           │ generic …   │  │  thresholds   │  └────────────┘
           └─────────────┘  └───────────────┘
                      │
           ┌──────────▼──────────────────────────────┐
           │  server (HTTP) + metrics (Prometheus)    │
           │  /metrics → Grafana: shutdowns & $/tenant│
           └─────────────────────────────────────────┘
```

### Layer responsibilities

| Layer | Package(s) | Responsibility |
|-------|------------|----------------|
| CLI | `cmd/net-finops-agent` | Flags, timeouts, I/O, invoke engine or `serve` |
| Microservice | `internal/server` | HTTP API, scheduler, health, wire metrics |
| Metrics | `internal/metrics` | Prometheus: shutdowns + savings by tenant |
| Wiring | `internal/provider/live`, `registry` | Map config → concrete scanners/remediators |
| Orchestration | `internal/engine` | Run providers, call policy, remediate, aggregate |
| Policy | `internal/policy` | Protection + classification + action/cost |
| Providers | `internal/provider/*` | Inventory + metrics + mutate per CSP |
| Domain | `pkg/finops` | Shared types, kind catalog, classify helpers |
| Config | `internal/config` | YAML load, rates, thresholds, provider map, `service:` |
| Output | `internal/report` | Human/machine report formats |
| Deploy | `deploy/` | Docker Compose: agent + Prometheus + Grafana |

---

## 4. Core domain model

### 4.1 `Finding`

One discovered resource with utilization evidence:

| Field | Meaning |
|-------|---------|
| `Cloud` | CSP id (`aws`, `oci`, `kubernetes`, …) — free string |
| `Kind` | Resource type (`nat_gateway`, `deployment`, …) — free string |
| `ResourceID` / `Name` / `Region` / `Namespace` | Identity & location |
| `Tags` | Used for protection (`environment=production`, `finops=keep`) |
| `MetricName` / `MetricValue` / `MetricUnit` | Utilization signal |
| `PolicyClass` | Optional override; else auto-classified |
| `Recommended` | `shutdown` \| `downsize` \| `notify` \| `none` |
| `MonthlyWasteUSD` | Estimated savings if acted on |
| `Protected` / `ProtectedReason` | Guardrail outcome |

### 4.2 `Kind` vs `KindClass`

- **Kind** = *what* the resource is (may be vendor-specific or custom).
- **KindClass** = *how* we measure idleness (shared policy algorithm).

This separation is what allows “all resources”: a brand-new kind still maps to a class via unit heuristics or defaults to `utilization`.

### 4.3 Actions

| Action | Intent |
|--------|--------|
| `shutdown` | Delete / deallocate / scale-to-zero |
| `downsize` | Cheaper SKU, fewer replicas, consolidate gateways |
| `notify` | Report only (typical for protected resources) |
| `none` | Healthy / within policy |

---

## 5. End-to-end pipeline (step by step)

A full `remediate` run executes the following steps. `scan` and `estimate` stop after reporting.

```
┌────────┐   ┌────────┐   ┌──────────┐   ┌───────────┐   ┌──────────┐   ┌──────────┐   ┌─────────┐
│ 1.Load │──▶│ 2.Wire │──▶│ 3.Scan   │──▶│ 4.Norm +  │──▶│ 5.Protect│──▶│ 6.Classify│──▶│ 7.Eval  │
│ config │   │ providers│  │ inventory│  │ aliases   │   │ guardrails│  │ + metric │   │ action  │
└────────┘   └────────┘   └──────────┘   └───────────┘   └──────────┘   └──────────┘   └────┬────┘
                                                                                            │
     ┌────────────┐   ┌───────────┐   ┌──────────┐   ┌──────────────────────────────────────┘
     │ 11.Output  │◀──│ 10.Report │◀──│ 9.Remediate│◀──│ 8.Aggregate & sort by $ waste
     └────────────┘   └───────────┘   └──────────┘
```

---

### Step 1 — Load configuration

**Where:** `internal/config.Load` / `Defaults`

**What happens**

1. Read YAML (`configs/config.yaml` or `--config`).
2. Merge with built-in defaults (rates, thresholds, provider map).
3. Accept legacy keys (`nat_idle_bytes_per_hour` → `gateway_idle_bytes_per_hour`).

**Why**

Operators tune risk (thresholds, exclude tags) and scope (which CSPs/regions) without code changes.

**Key inputs**

- `scan.lookback_hours` — utilization averaging window
- `policy.*` thresholds and protection lists
- `cost.defaults` / `cost.by_cloud` hourly USD rates
- `providers.<id>.{enabled,regions,account,options}`
- `remediator.{dry_run,max_actions_per_run,require_approval_tag}`
- `service.{listen_addr,schedule_interval,auto_remediate,dry_run}` — used by `serve` only

See **[configs/README.md](configs/README.md)** for a full key → code usage map, and inline comments in [`configs/config.yaml`](configs/config.yaml).

---

### Step 2 — Wire providers (registry)

**Where:** `internal/provider/live.Build` → `registry.BuildEnabled`

**What happens**

1. On process start, packages `Register` factories (`aws`, `azure`, `gcp`, `oci`, `kubernetes`, `generic`, …).
2. For each **enabled** provider in config, look up its factory.
3. Factory returns a `Scanner` + `Remediator` for `demo` or `live` mode.
4. Missing factory → hard error (misconfiguration).

**Why**

Open-closed design: new CSPs plug in without editing the engine.

**Demo vs live**

| Mode | Behavior |
|------|----------|
| `demo` | In-memory / canned inventories (no credentials) |
| `live` | Expects SDK adapters; may error until wired |

**Inventory-backed CSPs** (`oci`, `digitalocean`, …) read `options.inventory_file` or a built-in demo snapshot.

---

### Step 3 — Scan inventory & metrics (how usage is identified)

**Where:** `provider.Scanner.Scan` implementations  
**Interfaces:** `InventoryClient` (list resources) + `MetricsClient` (usage over lookback)

**What happens (per provider)**

1. List resources in configured regions / cluster via inventory APIs (or demo fixtures / YAML).
2. For each resource, attach a utilization metric averaged over `scan.lookback_hours`.
3. Emit **raw** `Finding`s: identity + `MetricName` / `MetricValue` / `MetricUnit` (no action yet).

**Important:** Today **`--mode=demo`** fills metrics from in-memory fixtures (same `Finding` shape).  
**`--mode=live`** expects SDK adapters behind the interfaces below; until wired, live mode errors.  
The table is the **intended live usage-API map** the adapters must implement.

#### 3.1 Inventory APIs (discover resources)

| CSP | Agent interface methods | Intended live API / SDK |
|-----|-------------------------|-------------------------|
| **AWS** | `ListNATGateways`, `ListInternetGateways`, `ListTransitGateways`, `ListVPNGateways`, `ListElasticIPs`, `ListVPCEndpoints`, `ListLoadBalancers`, `ListCloudResources` | EC2 / ELB Describe* (`DescribeNatGateways`, `DescribeInternetGateways`, `DescribeTransitGateways`, `DescribeVpnGateways`, `DescribeAddresses`, `DescribeVpcEndpoints`, `DescribeLoadBalancers`, EC2/RDS/Lambda/S3 list for compute/storage) |
| **Azure** | `ListNATGateways`, `ListPublicIPs` | Azure Resource Manager: `NatGateways.List`, `PublicIPAddresses.List` (expandable to VMs, disks, …) |
| **GCP** | `ListCloudNATs`, `ListAddresses` | Compute API: routers/NATs, addresses (expandable to GCE, disks, …) |
| **Kubernetes** | `ListAll()` | client-go list: Ingress, Gateway, Service, Deployment, StatefulSet, DaemonSet, Job/CronJob, Pod, PVC/PV, HPA, Namespace, … |
| **Generic / OCI / others** | YAML `items[]` | CMDB export, `configs/inventory.example.yaml`, or CSP CLI dump → no live SDK required |

#### 3.2 Metrics APIs (identify usage)

| CSP | Agent interface | Intended live metrics source | Typical series / signal |
|-----|-----------------|------------------------------|-------------------------|
| **AWS** | `MetricsClient.BytesPerHour`, `RequestRate` | **CloudWatch** `GetMetricStatistics` / `GetMetricData` | NAT: `AWS/NATGateway` `BytesOutToDestination` (+ in); IGW/TGW bytes or packet counts; ALB/NLB: `RequestCount` → RPS; EC2/RDS: `CPUUtilization`; Lambda: `Invocations`; EBS attach state from inventory |
| **Azure** | `MetricsClient.BytesPerHour` (etc.) | **Azure Monitor** Metrics API | NAT Gateway `ByteCount` / `PacketCount`; Public IP association from ARM; VM `% Processor Time` |
| **GCP** | `MetricsClient` bytes/rate helpers | **Cloud Monitoring** (`monitoring.googleapis.com`) | Cloud NAT `router.googleapis.com/nat/sent_bytes_count`; address `in_use_by`; GCE `compute.googleapis.com/instance/cpu/utilization` |
| **Kubernetes** | `MetricsClient.Utilization` | **metrics-server** and/or **Prometheus** (ingress-nginx / gateway controller) | Ingress/Gateway RPS; Deployment `status.readyReplicas`; Pod CPU from metrics-server; PVC bound + used bytes |
| **Generic inventory** | Fields on each item | Pre-computed in YAML | `metric_name`, `metric_value`, `metric_unit` already set |

**Lookback:** all live metric queries should average (or sum→rate) over `lookback_hours` (default **168h = 7 days**), then normalize to the unit policy expects (`bytes/hour`, `rps`, `cpu_percent`, …).

#### 3.3 Finding metric → idle decision (examples)

| Resource | Metric on Finding | Unit | Idle when (default policy) |
|----------|-------------------|------|----------------------------|
| NAT / IGW / SGW | bytes processed | `bytes/hour` | ≤ `gateway_idle_bytes_per_hour` → **shutdown**; ≤ low → **downsize** |
| DRG / TGW / VPN | attachment count | `attachments` | `0` → **shutdown**; `< min_gateway_attachments` → **downsize** |
| Public IP / EIP | associated? | `bool` / association | `≤ 0` → **shutdown** |
| Disk / PVC | attached / % used | `attached` / `%` | unattached or %used ≤ `storage_idle_percent` → **shutdown** |
| LB / Ingress / API GW | request rate | `rps` | ≤ `gateway_idle_rps` → **shutdown** |
| VM / DB / Pod | CPU | `cpu_percent` | ≤ `compute_idle_percent` → **shutdown**; ≤ low → **downsize** |
| Function / Lambda | invocations | `invocations_per_hour` | ≤ idle RPS-style threshold → **shutdown** |
| Deployment / STS | ready replicas | `replicas` | `< min_active_replicas` → **shutdown**; `> max_idle_replicas` → **downsize** |
| Unknown custom kind | any | any | ≤ `generic_idle_threshold` via `utilization` class |

**Provider patterns**

| Pattern | Used by | Description |
|---------|---------|-------------|
| Typed inventory + metrics clients | AWS, Azure, GCP | List* + CloudWatch/Monitor-style client |
| Unified K8s list | Kubernetes | `ListAll()` + optional metrics client |
| Normalized YAML | Generic / OCI demo | File or built-in demo snapshot → findings |

**Why**

Providers own vendor API quirks; the engine only sees findings. Idle detection itself is **not** done in the metrics API — metrics only supply the number; **policy** (Steps 5–7) decides idle.

---

### Step 4 — Normalize kind aliases

**Where:** `finops.NormalizeKind` inside `policy.Evaluator.Evaluate`

**What happens**

Map short/vendor aliases to canonical kinds, e.g.:

- `igw` → `internet_gateway`
- `drg` → `dynamic_routing_gateway`
- `ec2` / `vm` → `instance`
- `ebs` → `disk`
- `elastic_ip` → `public_ip`

**Why**

Inventory exports and humans use different names; policy and cost tables use one canonical key.

---

### Step 5 — Apply protection guardrails

**Where:** `policy.Evaluator.protectionReason`

**Checks (in order)**

1. **Min age** — `CreatedAt` younger than `min_age_hours` → protect (avoid racing new labs).
2. **Exclude tags** — e.g. `environment=production`, `finops=keep`.
3. **Protected namespaces** — e.g. `kube-system`, `ingress-nginx`.
4. **Approval tag** (optional) — require `finops-approve=true` when configured.

**Outcome if protected**

- `Protected = true`
- `Recommended = notify`
- Severity low  
- **No** mutating remediation later

**Why**

FinOps automation must not delete production or brand-new resources by accident.

---

### Step 6 — Classify into a policy class

**Where:** `finops.Classify(kind, metricUnit)` (+ optional `Finding.PolicyClass`)

**Algorithm (simplified)**

1. If inventory set `policy_class`, use it.
2. Else inspect **metric unit** heuristics (`cpu`, `rps`, `attachment`, `attached`, `bytes`, …).
3. Else map **known kinds** to classes (gateways → traffic/attachment, VMs → compute, disks → storage, Deployments → replica, …).
4. Else default to `utilization` (still actionable for custom kinds).
5. Apply edge overrides (e.g. attachment-class with bytes unit → `traffic_bytes`).

**Class → evaluation function**

| Class | Evaluator method |
|-------|------------------|
| `traffic_bytes` | `evalTrafficBytes` |
| `association` | `evalAssociation` |
| `request_rate` | `evalRequestRate` |
| `attachment` | `evalAttachment` |
| `compute_idle` | `evalComputeIdle` |
| `storage_idle` | `evalStorageIdle` |
| `replica_waste` | `evalReplicaWaste` |
| `utilization` | `evalUtilization` |

**Why**

One engine path for “all resources”: new kinds inherit a class instead of needing new switch cases everywhere.

---

### Step 7 — Evaluate thresholds → action & cost (how idle is detected)

**Where:** `policy.Evaluator` eval* methods + `config.HourlyRate`

Idle detection is a **deterministic compare** of `Finding.MetricValue` against config thresholds for the finding’s policy class. No ML.

**Exact rules (code path)**

| Class | Condition | Action | Cost factor |
|-------|-----------|--------|-------------|
| `traffic_bytes` | `value ≤ gateway_idle_bytes_per_hour` | shutdown | ×1.0 monthly |
| `traffic_bytes` | idle &lt; `value ≤ gateway_low_bytes_per_hour` | downsize | ×0.5 |
| `association` | `value ≤ 0` | shutdown | ×1.0 |
| `request_rate` | `value ≤ gateway_idle_rps` | shutdown | ×1.0 |
| `attachment` | `value ≤ 0` | shutdown | ×1.0 |
| `attachment` | `0 < value < min_gateway_attachments` | downsize | ×0.4 |
| `compute_idle` | CPU path: `value ≤ compute_idle_percent` | shutdown | ×1.0 |
| `compute_idle` | CPU path: idle &lt; `value ≤ compute_low_percent` | downsize | ×0.5 |
| `compute_idle` | unit contains `invocation`/`rps`: use `gateway_idle_rps` | shutdown if ≤ | ×1.0 |
| `storage_idle` | assoc unit or `value ≤ 0` | shutdown | ×1.0 |
| `storage_idle` | percent unit and `value ≤ storage_idle_percent` | shutdown | ×1.0 |
| `replica_waste` | rps unit and `value ≤ gateway_idle_rps` | shutdown | ×1.0 |
| `replica_waste` | replicas: `value < min_active_replicas` | shutdown | ×1.0 |
| `replica_waste` | replicas: `value > max_idle_replicas` | downsize | ×0.3 |
| `utilization` | `value ≤ generic_idle_threshold` | shutdown | ×1.0 |

Anything else → `ActionNone`, `MonthlyWasteUSD = 0`, rationale `"utilization within policy"`.

**Cost**

```
MonthlyWasteUSD = HourlyRate(cloud, kind) × 730 × (optional downsize factor)
```

Lookup: `cost.by_cloud[cloud][kind]` → `cost.defaults[kind]` → fallback `0.01` USD/hour.

**Also written on the finding:** `Threshold`, `Severity`, human `Rationale` (auditable why).

---

### Step 8 — Aggregate scan report

**Where:** `engine.Engine.Scan` (tail)

**What happens**

1. Collect all evaluated findings from all providers.
2. Sort by `MonthlyWasteUSD` descending (biggest savings first).
3. Count `ActionableCount`, `ProtectedCount`, sum `TotalWasteUSD`.
4. Return `ScanReport`.

**Why**

Operators and `estimate` need a single ranked view of waste.

---

### Step 9 — Remediate (optional)

**Where:** `engine.Engine.Remediate`

**What happens**

1. If no report yet, run Step 3–8 first.
2. Walk findings in waste order.
3. Skip protected / `none` / `notify`.
4. Stop when `max_actions_per_run` reached.
5. Select remediator where `Supports(finding)`.
6. Call `Apply(ctx, finding, action, dryRun)`.
7. Append `RemediationResult` (success, message, savings).

**Dry-run vs apply**

| Mode | Mutator behavior |
|------|------------------|
| `dryRun=true` (default) | Log “would shutdown …” — no API delete |
| `dryRun=false` (`--apply`) | Call CSP/K8s delete or demo recorder |

**Why**

Blast radius is capped; dry-run is the safe default for CI and first adoption.

---

### Step 10 — Build report artifact

**Where:** `internal/report`

| Format | Use |
|--------|-----|
| `table` | Terminal ops review |
| `json` | CI artifacts / automation |
| `markdown` | Tickets / Confluence |

Includes findings (non-`none`), remediations, and top rationales.

---

### Step 11 — Output

**Where:** CLI `writeReport` **or** microservice JSON/Prometheus

| Entry | Output |
|-------|--------|
| `scan` / `remediate` / `estimate` | stdout and/or `--output` (`table` \| `json` \| `markdown`) |
| `serve` | HTTP: `/api/v1/report`, `/api/v1/tenants/savings`; Prometheus: `/metrics` → Grafana |

---

## 6. Component design details

### 6.1 Engine

- **Stateful only for dependencies** (config, scanners, remediators, logger).
- **Stateless per run** — each `Scan`/`Remediate` builds a fresh report.
- **Error model** — provider scan failures abort the run (fail closed); remediation per-item errors are recorded and continue.

### 6.2 Policy evaluator

Pure function over `(Finding, Config)` → mutates finding fields. Easy to unit test without I/O.

### 6.3 Provider interfaces

```go
type Scanner interface {
    Name() string
    Cloud() finops.Cloud
    Scan(ctx context.Context) ([]finops.Finding, error)
}

type Remediator interface {
    Name() string
    Supports(f finops.Finding) bool
    Apply(ctx context.Context, f finops.Finding, action finops.Action, dryRun bool) (finops.RemediationResult, error)
}
```

Internal to a provider, further interfaces (`InventoryClient`, `MetricsClient`, `Mutator`) keep demo and live implementations swappable.

### 6.4 Registry

Thread-safe map `cloudID → Factory`. Registration at `init()` keeps the CLI thin.

### 6.5 Generic inventory provider

Enables “any cloud” without SDK work:

```
YAML/JSON inventory → generic.Scanner → Findings → same policy/engine path
```

Ideal for CMDB dumps, Excel→YAML exports, or CSPs not yet natively integrated.

### 6.6 Microservice (`serve`) + Grafana

**Where:** `internal/server`, `internal/metrics`, `deploy/`

Long-running mode (not one-shot CLI):

1. HTTP listen on `service.listen_addr` (default `:8080`).
2. Initial + scheduled cycle every `service.schedule_interval` (default `15m`).
3. Cycle = `Scan` or `Remediate` depending on `service.auto_remediate` / `service.dry_run`.
4. After each cycle, update Prometheus gauges/counters.
5. Prometheus scrapes `/metrics`; Grafana dashboard shows shutdowns and **$/tenant**.

| Endpoint | Role |
|----------|------|
| `GET /healthz`, `/readyz` | Probes |
| `GET /metrics` | Prometheus scrape |
| `GET /api/v1/report` | Latest `ScanReport` JSON |
| `GET /api/v1/tenants/savings` | Per-tenant monthly waste + shutdown/downsize counts |
| `POST /api/v1/scan` | On-demand scan |
| `POST /api/v1/remediate?dry_run=true\|false` | On-demand remediate |

**Tenant label resolution** (`metrics.TenantID`): `tags["tenant"]` → `AccountOrSub` → cloud id → `"unknown"`.

| Prometheus metric | Meaning |
|-------------------|---------|
| `finops_resources_shutdown_total{tenant,cloud,kind,action,dry_run}` | Shutdown/downsize events |
| `finops_cost_savings_usd_monthly{tenant}` | Latest estimated monthly waste (from scan gauges) |
| `finops_cost_savings_usd_total{tenant,…}` | Cumulative USD from **live** applies only (`dry_run=false`) |
| `finops_findings_actionable{tenant,cloud,kind}` | Latest actionable counts |
| `finops_scan_duration_seconds`, `finops_scan_errors_total`, `finops_service_up` | Ops health |

Compose stack: `deploy/docker-compose.yml` → agent + Prometheus + Grafana (dashboard under FinOps folder).

---

## 7. Sequence diagram (scan + dry-run remediate)

```
CLI          live.Build       Engine        Scanner(s)      Policy       Remediator
 │               │              │               │             │              │
 │-- config ----▶│              │               │             │              │
 │◀─ scanners ───│              │               │             │              │
 │-- Remediate(dryRun=true) ---▶│               │             │              │
 │               │              │-- Scan() ----▶│             │              │
 │               │              │◀─ findings ───│             │              │
 │               │              │-- Evaluate ────────────────▶│              │
 │               │              │◀─ enriched ─────────────────│              │
 │               │              │-- Apply(dryRun) ──────────────────────────▶│
 │               │              │◀─ RemediationResult ───────────────────────│
 │◀─ ScanReport ─│              │               │             │              │
 │-- WriteTable/JSON/MD                                                             
```

---

## 8. Extensibility playbooks

### Add a new resource kind (same CSP)

1. Emit it from the provider scanner with `Kind`, metric name/value/unit.
2. Optionally set `PolicyClass`.
3. Add hourly rate under `cost.defaults` or `cost.by_cloud` (optional; fallback exists).
4. No engine changes required if class heuristics fit.

### Add a new CSP (native)

1. Create `internal/provider/mycloud` with Scanner/Remediator.
2. `registry.Register("mycloud", factory)` from `live` init.
3. Enable in `configs/config.yaml`.

### Add a new CSP (inventory only)

1. Produce inventory YAML.
2. Enable `providers.mycloud` with `options.inventory_file` (use a registered generic factory id, or register `factoryGeneric("mycloud")`).

### Add a new policy class

1. Add `KindClass` constant in `pkg/finops`.
2. Extend `Classify` mapping.
3. Add `evalX` in `internal/policy` and config thresholds.
4. Add unit tests in `policy_test.go`.

---

## 9. Safety design

Defense in depth:

1. Config `remediator.dry_run` + CLI default without `--apply`
2. Protection evaluation before action selection
3. `max_actions_per_run` circuit breaker
4. Optional approval tag gate
5. Remediator dry-run branch that never calls delete APIs
6. Demo mutators that only append to an in-memory list

Production rollout assumes dry-run reviews and non-prod first.

---

## 10. Testing design

| Level | What | Location |
|-------|------|----------|
| Unit | Thresholds, aliases, protection, rates | `internal/policy`, `internal/config` |
| Unit | Tenant metrics observe | `internal/metrics` |
| Component | Demo AWS inventory shape | `internal/provider/aws` |
| Integration-ish | Multi-provider demo scan + dry-run remediate | `internal/engine` |
| HTTP | `/api/v1/scan`, `/tenants/savings`, `/metrics` | `internal/server` |
| Manual / smoke | CLI + `serve` + compose Grafana | `README.md` |

Demo inventories are **fixtures**: deterministic metrics so CI does not need cloud accounts.

---

## 11. Future design extensions (optional)

| Extension | Notes |
|-----------|-------|
| Live AWS/Azure/GCP/K8s SDKs | Implement existing `InventoryClient` / `MetricsClient` / `Mutator` with CloudWatch, Azure Monitor, Cloud Monitoring, client-go |
| CUR / Cost Export ingestion | Replace static hourly rates with actual spend |
| Approval workflow | Ticket / Slack confirm before `--apply` |
| Persistence | Store findings history for multi-week trend dashboards |
| Soft delete / quarantine | Move to quarantine RG/project before hard delete |
| Broader Azure/GCP inventory | Parity with AWS `ListCloudResources` surface |

Already delivered (no longer “future”): HTTP microservice scheduler, Prometheus metrics, Grafana compose stack.

---

## 12. Summary

The Net FinOps Agent is a **pipeline**:

**configure → wire providers → scan (inventory + usage metrics) → normalize → protect → classify → evaluate idle thresholds → aggregate → (remediate) → report / Prometheus**

**Usage identification** = CSP inventory APIs + metrics APIs (CloudWatch / Azure Monitor / Cloud Monitoring / K8s metrics) → `Finding.Metric*`.  
**Idle detection** = policy class thresholds on that metric (Steps 5–7), not a separate ML model.

The critical design choice is **policy classes over vendor-specific engines**, which lets the same code path handle NAT gateways, DRGs, idle VMs, unattached disks, and abandoned Kubernetes Deployments—on any cloud that can produce a `Finding`.
