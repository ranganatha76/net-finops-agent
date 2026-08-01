# Configuration guide

How YAML under `configs/` is loaded and used by the Net FinOps Agent.

## Files

| File | Purpose |
|------|---------|
| [`config.yaml`](config.yaml) | Main runtime config: scan window, policy, costs, providers, remediator, **service** |
| [`inventory.example.yaml`](inventory.example.yaml) | Sample normalized inventory for inventory-backed CSPs |

## Load path

```
CLI --config configs/config.yaml
        │
        ▼
internal/config.Load()
        │  merge onto Defaults()
        ▼
live.Build(cfg, mode)     → scanners + remediators
        │
        ├── engine (scan / remediate CLI)
        └── serve → server.Service (scheduler + /metrics → Grafana)
```

If `--config` is missing on disk, the CLI uses `config.Defaults()` and logs a warning.

## Section → code usage map

### `scan`

| Key | Used by | Effect |
|-----|---------|--------|
| `lookback_hours` | AWS/Azure/GCP/K8s scanners; `policy.Evaluate` | Metric window; stamped on each Finding |

### `policy`

| Key | Policy class / path | Effect |
|-----|---------------------|--------|
| `gateway_idle_bytes_per_hour` | `traffic_bytes` | ≤ → **shutdown** |
| `gateway_low_bytes_per_hour` | `traffic_bytes` | ≤ → **downsize** |
| `gateway_idle_rps` | `request_rate` (+ some compute) | ≤ → **shutdown** |
| `min_gateway_attachments` | `attachment` | 0 → shutdown; sparse → downsize |
| `compute_idle_percent` | `compute_idle` | ≤ → **shutdown** |
| `compute_low_percent` | `compute_idle` | ≤ → **downsize** |
| `storage_idle_percent` | `storage_idle` (% used) | ≤ → **shutdown** |
| `min_active_replicas` | `replica_waste` | below → **shutdown** |
| `max_idle_replicas` | `replica_waste` | above → **downsize** |
| `generic_idle_threshold` | `utilization` | ≤ → **shutdown** |
| `min_age_hours` | protection | younger resources → **notify only** |
| `exclude_tags` | protection | matching tags → **notify only** |
| `protected_namespaces` | protection | K8s ns → **notify only** |

### `cost`

| Key | Used by | Effect |
|-----|---------|--------|
| `defaults` | `Config.HourlyRate` | kind → USD/hour |
| `by_cloud` | `Config.HourlyRate` | cloud+kind override |

`MonthlyWasteUSD = HourlyRate(cloud, kind) × 730` (partial factor for downsize).

### `providers`

| Key | Used by | Effect |
|-----|---------|--------|
| `<id>.enabled` | `EnabledProviders` / registry | include CSP in run |
| `<id>.regions` | provider scanners | iterate regions/locations |
| `<id>.account` | scanners | Finding.AccountOrSub / cluster name |
| `<id>.options.inventory_file` | generic / OCI-style factories | path to inventory YAML |

### `remediator`

| Key | Used by | Effect |
|-----|---------|--------|
| `dry_run` | report hint / ops default | CLI `remediate` still needs `--apply` to mutate |
| `max_actions_per_run` | `engine.Remediate` | stop after N mutations |
| `require_approval_tag` | `protectionReason` | need `finops-approve=true` |

### `service` (microservice / Grafana)

| Key | Used by | Effect |
|-----|---------|--------|
| `listen_addr` | `server.Service.Run` | HTTP bind (default `:8080`) |
| `schedule_interval` | scheduler ticker | rescan period (`15m`, `1h`, …) |
| `auto_remediate` | scheduled cycle | scan-only vs remediate each tick |
| `dry_run` | scheduled/API remediate | live savings counters only when false / `?dry_run=false` |

Prometheus series (scraped at `/metrics`) are labeled by **tenant** (`tags.tenant` → account → cloud). Grafana dashboard in `deploy/grafana/dashboards/`.

## Inventory file usage

1. Enable an inventory-backed provider:

```yaml
providers:
  digitalocean:
    enabled: true
    options:
      inventory_file: configs/inventory.example.yaml
```

2. Each `items[]` entry becomes one `Finding`.
3. Policy uses `kind`, `metric_*`, `tags`, `namespace`, `created_at`, optional `policy_class`.

See field mapping comments inside [`inventory.example.yaml`](inventory.example.yaml).

## Quick experiments

```bash
# Stricter idle CPU (more shutdowns on compute)
#   policy.compute_idle_percent: 10

# Never touch anything without approval tag
#   remediator.require_approval_tag: true

# Only scan AWS + Kubernetes
#   set azure/gcp/oci enabled: false

# Cap blast radius
#   remediator.max_actions_per_run: 5

# Microservice with Grafana stack
#   make compose-up
```

Then:

```bash
./bin/net-finops-agent scan --config configs/config.yaml --mode demo
./bin/net-finops-agent remediate --config configs/config.yaml --mode demo
./bin/net-finops-agent serve --config configs/config.yaml --mode demo
```
