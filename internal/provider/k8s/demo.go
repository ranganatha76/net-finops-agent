package k8s

import (
	"context"
	"fmt"
	"time"

	"github.com/ranganatha76/net-finops-agent/pkg/finops"
)

// DemoInventory covers network + workload + storage K8s resources.
type DemoInventory struct{}

// ListAll returns a representative set of demo Kubernetes resources.
func (DemoInventory) ListAll(_ context.Context) ([]Resource, error) {
	now := time.Now().UTC()
	return []Resource{
		// network
		{Kind: finops.KindIngress, Name: "api-feature-x", Namespace: "team-payments-dev", CreatedAt: now.Add(-20 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		{Kind: finops.KindIngress, Name: "staging-web", Namespace: "team-web-staging", CreatedAt: now.Add(-90 * 24 * time.Hour),
			Labels: map[string]string{"environment": "staging"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		{Kind: finops.KindGatewayAPI, Name: "lab-gateway", Namespace: "team-mesh-dev", CreatedAt: now.Add(-18 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		{Kind: finops.KindServiceLB, Name: "orphan-lb", Namespace: "scratch", CreatedAt: now.Add(-14 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		{Kind: finops.KindServiceNodePort, Name: "debug-np", Namespace: "scratch", CreatedAt: now.Add(-10 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		{Kind: finops.KindIngressController, Name: "nginx-ingress-controller", Namespace: "ingress-nginx", CreatedAt: now.Add(-365 * 24 * time.Hour),
			Labels: map[string]string{"app.kubernetes.io/name": "ingress-nginx"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		{Kind: finops.KindIngressController, Name: "old-traefik", Namespace: "traefik-lab", CreatedAt: now.Add(-60 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		{Kind: finops.KindAPIGateway, Name: "kong-dev", Namespace: "api-lab", CreatedAt: now.Add(-22 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "rps", MetricUnit: "rps", MetricFromClient: true},
		// workloads
		{Kind: finops.KindDeployment, Name: "feature-x-api", Namespace: "team-payments-dev", CreatedAt: now.Add(-21 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "ready_replicas", MetricValue: 0, MetricUnit: "replicas"},
		{Kind: finops.KindDeployment, Name: "overprovisioned-web", Namespace: "team-web-staging", CreatedAt: now.Add(-50 * 24 * time.Hour),
			Labels: map[string]string{"environment": "staging"}, MetricName: "ready_replicas", MetricValue: 8, MetricUnit: "replicas"},
		{Kind: finops.KindStatefulSet, Name: "redis-lab", Namespace: "scratch", CreatedAt: now.Add(-30 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "http_rps", MetricValue: 0, MetricUnit: "rps"},
		{Kind: finops.KindDaemonSet, Name: "node-debug-agent", Namespace: "scratch", CreatedAt: now.Add(-45 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "ready_replicas", MetricValue: 0, MetricUnit: "replicas"},
		{Kind: finops.KindJob, Name: "oneoff-migrate-2024", Namespace: "scratch", CreatedAt: now.Add(-100 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "active", MetricValue: 0, MetricUnit: "replicas"},
		{Kind: finops.KindCronJob, Name: "stale-report", Namespace: "scratch", CreatedAt: now.Add(-80 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "last_schedule_success", MetricValue: 0, MetricUnit: "utilization"},
		{Kind: finops.KindPod, Name: "orphan-pod-xyz", Namespace: "scratch", CreatedAt: now.Add(-12 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "cpu", MetricValue: 0.1, MetricUnit: "cpu_percent"},
		// storage
		{Kind: finops.KindPVC, Name: "abandoned-pvc", Namespace: "scratch", CreatedAt: now.Add(-70 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "bound_and_used", MetricValue: 0, MetricUnit: "in_use", PolicyClass: finops.ClassStorageIdle},
		{Kind: finops.KindNamespace, Name: "team-old-hackathon", Namespace: "team-old-hackathon", CreatedAt: now.Add(-200 * 24 * time.Hour),
			Labels: map[string]string{"environment": "development"}, MetricName: "active_workloads", MetricValue: 0, MetricUnit: "replicas"},
	}, nil
}

// DemoMetrics provides fixed utilization metrics for demo Kubernetes resources.
type DemoMetrics struct{}

// Metric returns demo metric values keyed by namespace/name.
func (DemoMetrics) Metric(_ context.Context, namespace, name string, _ time.Duration) (float64, error) {
	switch namespace + "/" + name {
	case "team-payments-dev/api-feature-x", "scratch/orphan-lb", "scratch/debug-np",
		"team-mesh-dev/lab-gateway", "traefik-lab/old-traefik", "api-lab/kong-dev":
		return 0, nil
	case "team-web-staging/staging-web":
		return 0.002, nil
	case "ingress-nginx/nginx-ingress-controller":
		return 120, nil
	default:
		return 1, nil
	}
}

// DemoMutator records deleted Kubernetes resources for demo remediation.
type DemoMutator struct {
	Deleted []string // kind:namespace/name strings "deleted" during demo apply
}

// DeleteResource appends the resource to Deleted and succeeds without calling the API server.
func (m *DemoMutator) DeleteResource(_ context.Context, kind finops.ResourceKind, namespace, name string) error {
	m.Deleted = append(m.Deleted, fmt.Sprintf("%s:%s/%s", kind, namespace, name))
	return nil
}
