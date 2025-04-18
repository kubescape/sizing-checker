package common

import (
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
)

type ConnectivityResult struct {
	AddressesTested []string
	SuccessCount    int
	ResultMessage   string // "Passed", "Failed", "Partial success (X/Y)", or "Skipped"
}

type SizingResult struct {
	TotalResources          int
	MaxNodeCPUCapacity      int
	MaxNodeMemoryMB         int
	LargestContainerImageMB int

	// Final recommended resource allocations for each component
	FinalResourceAllocations map[string]map[string]string
	// Default resource allocations (if you need them, or remove if not)
	DefaultResourceAllocations map[string]map[string]string

	// Whether any resource changed from default
	HasSizingAdjustments bool
}

type PVCheckResult struct {
	PassedCount   int
	FailedCount   int
	TotalNodes    int
	ResultMessage string // "Passed", "Failed", or "Skipped"
}

type NodeInfoSummary struct {
	OperatingSystemCounts         map[string]int
	ArchitectureCounts            map[string]int
	KernelVersionCounts           map[string]int
	OSImageCounts                 map[string]int
	ContainerRuntimeVersionCounts map[string]int
	KubeletVersionCounts          map[string]int
	KubeProxyVersionCounts        map[string]int
}

// ClusterDetails stores metadata about the cluster
type ClusterDetails struct {
	Name            string
	Version         string
	CloudProvider   string
	K8sDistribution string
	TotalNodeCount  int
	TotalVCPUCount  int
}

type ClusterData struct {
	Nodes        []corev1.Node
	Pods         []corev1.Pod
	Services     []corev1.Service
	Deployments  []appsv1.Deployment
	ReplicaSets  []appsv1.ReplicaSet
	StatefulSets []appsv1.StatefulSet
	DaemonSets   []appsv1.DaemonSet
	Jobs         []batchv1.Job
	CronJobs     []batchv1.CronJob

	StorageClasses []storagev1.StorageClass

	ClusterDetails    ClusterDetails
	NodeInfoSummaries NodeInfoSummary
}

type EbpfResult struct {
	ResultMessage string // "Passed", "Warning", "Failed", or any descriptive message
}

type ReportData struct {
	TotalResources          int
	MaxNodeCPUCapacity      int
	MaxNodeMemoryMB         int
	LargestContainerImageMB int

	DefaultResourceAllocations map[string]map[string]string
	FinalResourceAllocations   map[string]map[string]string

	KubernetesVersion string
	CloudProvider     string
	K8sDistribution   string
	TotalNodeCount    int
	TotalVCPUCount    int

	GenerationTime       string
	HasSizingAdjustments bool

	NodeOSSummary               string
	NodeArchSummary             string
	NodeKernelVersionSummary    string
	NodeOSImageSummary          string
	NodeContainerRuntimeSummary string
	NodeKubeletVersionSummary   string
	NodeKubeProxyVersionSummary string

	FullClusterData *ClusterData

	PVProvisioningMessage    string
	ConnectivityCheckMessage string

	EBPFResultMessage string

	InCluster bool

	StorageClasses []string
}
