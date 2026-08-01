package finops

// ResourceKind is any CSP-neutral or vendor resource type string.
// Known constants below are helpers; unknown kinds are still tracked and
// evaluated via metric-unit heuristics (so "all cloud / all k8s resources" work).
type ResourceKind string

const (
	// --- Network / gateways ---
	KindNATGateway            ResourceKind = "nat_gateway"
	KindInternetGateway       ResourceKind = "internet_gateway"
	KindServiceGateway        ResourceKind = "service_gateway"
	KindDynamicRoutingGateway ResourceKind = "dynamic_routing_gateway"
	KindTransitGateway        ResourceKind = "transit_gateway"
	KindVPNGateway            ResourceKind = "vpn_gateway"
	KindLocalPeeringGateway   ResourceKind = "local_peering_gateway"
	KindExpressRouteGateway   ResourceKind = "express_route_gateway"
	KindNetworkFirewall       ResourceKind = "network_firewall"
	KindApplicationGateway    ResourceKind = "application_gateway"
	KindPrivateEndpoint       ResourceKind = "private_endpoint"
	KindPublicIP              ResourceKind = "public_ip"
	KindLoadBalancer          ResourceKind = "load_balancer"

	// --- Compute ---
	KindInstance       ResourceKind = "instance"        // EC2 / Azure VM / GCE / OCI compute
	KindInstancePool   ResourceKind = "instance_pool"
	KindSpotInstance   ResourceKind = "spot_instance"
	KindDedicatedHost  ResourceKind = "dedicated_host"
	KindContainerApp   ResourceKind = "container_app"   // ACS / Cloud Run / Container Apps
	KindFunction       ResourceKind = "function"        // Lambda / Functions / Cloud Functions
	KindBatchJob       ResourceKind = "batch_job"

	// --- Storage ---
	KindDisk           ResourceKind = "disk"            // EBS / Managed Disk / Persistent Disk
	KindSnapshot       ResourceKind = "snapshot"
	KindImage          ResourceKind = "image"
	KindFileShare      ResourceKind = "file_share"      // EFS / Azure Files / Filestore
	KindObjectBucket   ResourceKind = "object_bucket"   // S3 / Blob / GCS / Object Storage
	KindBackupVault    ResourceKind = "backup_vault"

	// --- Data / messaging / cache ---
	KindDatabase       ResourceKind = "database"        // RDS / Azure SQL / Cloud SQL / Autonomous
	KindCache          ResourceKind = "cache"           // ElastiCache / Redis / Memorystore
	KindQueue          ResourceKind = "queue"
	KindStream         ResourceKind = "stream"
	KindSearchCluster  ResourceKind = "search_cluster"
	KindWarehouse      ResourceKind = "warehouse"       // Redshift / Synapse / BigQuery reservation

	// --- Kubernetes workloads & objects ---
	KindIngress           ResourceKind = "ingress"
	KindGatewayAPI        ResourceKind = "gateway_api"
	KindServiceLB         ResourceKind = "service_loadbalancer"
	KindServiceNodePort   ResourceKind = "service_nodeport"
	KindServiceClusterIP  ResourceKind = "service_clusterip"
	KindIngressController ResourceKind = "ingress_controller"
	KindAPIGateway        ResourceKind = "api_gateway"
	KindDeployment        ResourceKind = "deployment"
	KindStatefulSet       ResourceKind = "statefulset"
	KindDaemonSet         ResourceKind = "daemonset"
	KindReplicaSet        ResourceKind = "replicaset"
	KindJob               ResourceKind = "job"
	KindCronJob           ResourceKind = "cronjob"
	KindPod               ResourceKind = "pod"
	KindPVC               ResourceKind = "persistentvolumeclaim"
	KindPV                ResourceKind = "persistentvolume"
	KindHPA               ResourceKind = "horizontalpodautoscaler"
	KindNamespace         ResourceKind = "namespace"

	// Aliases
	KindIGW         ResourceKind = "igw"
	KindSGW         ResourceKind = "sgw"
	KindDRG         ResourceKind = "drg"
	KindTGW         ResourceKind = "tgw"
	KindVGW         ResourceKind = "vgw"
	KindElasticIP   ResourceKind = "elastic_ip"
	KindVPCEndpoint ResourceKind = "vpc_endpoint"
	KindVM          ResourceKind = "vm"
	KindEC2         ResourceKind = "ec2"
	KindEBS         ResourceKind = "ebs"
	KindS3          ResourceKind = "s3"
	KindRDS         ResourceKind = "rds"
	KindLambda      ResourceKind = "lambda"
)

// KindClass selects which utilization policy applies.
type KindClass string

const (
	ClassTrafficBytes  KindClass = "traffic_bytes"
	ClassAssociation   KindClass = "association"
	ClassRequestRate   KindClass = "request_rate"
	ClassAttachment    KindClass = "attachment"
	ClassComputeIdle   KindClass = "compute_idle"   // CPU% / invocation rate
	ClassStorageIdle   KindClass = "storage_idle"   // unattached disk, empty/unused bucket
	ClassReplicaWaste  KindClass = "replica_waste"  // desired replicas with near-zero traffic
	ClassUtilization   KindClass = "utilization"    // generic 0–100 scale (or absolute idle threshold)
)

// NormalizeKind maps aliases to canonical kinds.
func NormalizeKind(k ResourceKind) ResourceKind {
	switch k {
	case KindIGW:
		return KindInternetGateway
	case KindSGW:
		return KindServiceGateway
	case KindDRG:
		return KindDynamicRoutingGateway
	case KindTGW:
		return KindTransitGateway
	case KindVGW:
		return KindVPNGateway
	case KindElasticIP:
		return KindPublicIP
	case KindVPCEndpoint:
		return KindPrivateEndpoint
	case KindVM, KindEC2:
		return KindInstance
	case KindEBS:
		return KindDisk
	case KindS3:
		return KindObjectBucket
	case KindRDS:
		return KindDatabase
	case KindLambda:
		return KindFunction
	default:
		return k
	}
}

// Classify picks a policy class from kind + metric unit.
// Explicit unit heuristics let unknown / custom resource kinds still evaluate correctly.
func Classify(k ResourceKind, metricUnit string) KindClass {
	if c := classFromUnit(metricUnit); c != "" {
		// Prefer unit when it clearly indicates the measurement type,
		// except when kind forces a more specific class below.
		switch NormalizeKind(k) {
		case KindPublicIP:
			return ClassAssociation
		case KindDisk, KindSnapshot, KindImage, KindFileShare, KindObjectBucket, KindBackupVault, KindPVC, KindPV:
			if c == ClassAssociation || c == ClassStorageIdle || c == ClassUtilization {
				return ClassStorageIdle
			}
		case KindInstance, KindInstancePool, KindSpotInstance, KindDedicatedHost, KindFunction, KindContainerApp, KindBatchJob,
			KindPod, KindJob:
			if c == ClassComputeIdle || c == ClassUtilization || c == ClassRequestRate {
				return ClassComputeIdle
			}
		case KindDeployment, KindStatefulSet, KindDaemonSet, KindReplicaSet, KindHPA:
			return ClassReplicaWaste
		}
		if c != "" {
			return c
		}
	}

	switch NormalizeKind(k) {
	case KindPublicIP:
		return ClassAssociation
	case KindLoadBalancer, KindApplicationGateway,
		KindIngress, KindGatewayAPI, KindServiceLB, KindServiceNodePort, KindServiceClusterIP,
		KindIngressController, KindAPIGateway, KindQueue, KindStream, KindSearchCluster:
		return ClassRequestRate
	case KindDynamicRoutingGateway, KindTransitGateway, KindVPNGateway,
		KindLocalPeeringGateway, KindExpressRouteGateway:
		return ClassAttachment
	case KindInstance, KindInstancePool, KindSpotInstance, KindDedicatedHost,
		KindFunction, KindContainerApp, KindBatchJob, KindPod, KindJob, KindCronJob:
		return ClassComputeIdle
	case KindDisk, KindSnapshot, KindImage, KindFileShare, KindObjectBucket, KindBackupVault, KindPVC, KindPV:
		return ClassStorageIdle
	case KindDeployment, KindStatefulSet, KindDaemonSet, KindReplicaSet, KindHPA, KindNamespace:
		return ClassReplicaWaste
	case KindDatabase, KindCache, KindWarehouse:
		return ClassComputeIdle
	case KindNATGateway, KindInternetGateway, KindServiceGateway, KindNetworkFirewall, KindPrivateEndpoint:
		return ClassTrafficBytes
	default:
		// Unknown cloud/k8s kind: still trackable — generic utilization policy.
		return ClassUtilization
	}
}

// classFromUnit infers a KindClass from the metric unit string alone
// (e.g. "cpu_percent" → compute_idle, "attachments" → attachment).
// Returns empty string when the unit is unrecognized.
func classFromUnit(u string) KindClass {
	u = toLower(u)
	switch {
	case u == "":
		return ""
	case contains(u, "attachment"):
		return ClassAttachment
	case u == "bool" || u == "associated" || u == "attached" || u == "in_use":
		return ClassAssociation
	case contains(u, "rps") || contains(u, "qps") || contains(u, "rpm") || contains(u, "request"):
		return ClassRequestRate
	case contains(u, "cpu") || contains(u, "invocation") || u == "percent" || u == "%":
		return ClassComputeIdle
	case contains(u, "replica"):
		return ClassReplicaWaste
	case contains(u, "byte") || contains(u, "gib") || contains(u, "mib") || contains(u, "gb"):
		return ClassTrafficBytes
	case contains(u, "idle") || contains(u, "utilization") || contains(u, "usage"):
		return ClassUtilization
	default:
		return ""
	}
}

// toLower returns a lowercase ASCII copy of s (avoids importing strings in this file).
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// contains reports whether s contains sub as a substring.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

// indexOf returns the first index of sub in s, or -1 if not found.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
