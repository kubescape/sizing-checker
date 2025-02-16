package pvcheck

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kubescape/sizing-checker/pkg/common"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	annDefaultStorageClass     = "storageclass.kubernetes.io/is-default-class"
	annBetaDefaultStorageClass = "storageclass.beta.kubernetes.io/is-default-class"
	noProvisioner              = "kubernetes.io/no-provisioner"
)

// PVCheckResult is a minimal struct containing pass/fail info.
type PVCheckResult struct {
	PassedCount   int
	FailedCount   int
	TotalNodes    int
	ResultMessage string // "Passed", "Failed", or "Skipped"
}

// RunPVProvisioningCheck does the real creation of PVC + Pod in a temporary namespace.
func RunPVProvisioningCheck(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	clusterData *common.ClusterData,
) *PVCheckResult {

	// 1) Pre-checks for dynamic provisioning
	passed, failReason := BasicPreCheck(ctx, clientset, clusterData)
	if !passed {
		return failResult(len(clusterData.Nodes), failReason)
	}

	// 2) If all pre-checks pass, try a real creation of PVC + Pod in an ephemeral namespace.
	namespace := "kubescape-pv-check-ns"
	if err := createNamespace(ctx, clientset, namespace); err != nil {
		return failResult(len(clusterData.Nodes),
			fmt.Sprintf("Failed to create temporary namespace %q: %v", namespace, err))
	}

	// Ensure cleanup:
	defer func() {
		if delErr := deleteNamespace(context.Background(), clientset, namespace); delErr != nil {
			log.Printf("Warning: could not delete namespace %q: %v", namespace, delErr)
		}
	}()

	pvcName := "kubescape-pv-check-pvc"
	podName := "kubescape-pv-check-pod"

	// 2a) Create a 5Gi PVC, letting the cluster pick the default StorageClass
	if err := createTestPVC(ctx, clientset, namespace, pvcName, "5Gi"); err != nil {
		return failResult(len(clusterData.Nodes),
			fmt.Sprintf("Failed to create PVC: %v", err))
	}

	// 2b) Create a Pod that references the PVC
	if err := createTestPod(ctx, clientset, namespace, podName, pvcName); err != nil {
		return failResult(len(clusterData.Nodes),
			fmt.Sprintf("Failed to create Pod: %v", err))
	}

	// 2c) Wait for the PVC to be Bound
	if err := waitForPVCBound(ctx, clientset, namespace, pvcName, 60*time.Second); err != nil {
		return failResult(len(clusterData.Nodes),
			fmt.Sprintf("PVC did not become Bound: %v", err))
	}

	// 2d) Wait for the Pod to become Running or Succeeded
	if err := waitForPodRunningOrSucceeded(ctx, clientset, namespace, podName, 60*time.Second); err != nil {
		return failResult(len(clusterData.Nodes),
			fmt.Sprintf("Pod did not become Running/Succeeded: %v", err))
	}

	// 3) If everything was successful => "Passed"
	return &PVCheckResult{
		PassedCount:   len(clusterData.Nodes),
		FailedCount:   0,
		TotalNodes:    len(clusterData.Nodes),
		ResultMessage: "Passed",
	}
}

// BasicPreCheck ensures there's at least one schedulable node,
// at least one dynamic StorageClass, and at least one default dynamic SC.
func BasicPreCheck(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	clusterData *common.ClusterData,
) (bool, string) {

	totalNodes := len(clusterData.Nodes)
	if totalNodes == 0 {
		return false, "No nodes found in cluster."
	}

	// Check for at least one schedulable node
	var schedulableFound bool
	for _, node := range clusterData.Nodes {
		if !node.Spec.Unschedulable {
			schedulableFound = true
			break
		}
	}
	if !schedulableFound {
		return false, "No schedulable node found (all unschedulable)."
	}

	// Reuse the storage classes from clusterData
	scList := clusterData.StorageClasses
	if len(scList) == 0 {
		return false, "No StorageClasses found; dynamic provisioning not available."
	}

	// Identify dynamic StorageClasses
	var dynamicSCs []storagev1.StorageClass
	for _, sc := range scList {
		if sc.Provisioner != noProvisioner && sc.Provisioner != "" {
			dynamicSCs = append(dynamicSCs, sc)
		}
	}
	if len(dynamicSCs) == 0 {
		return false, "All StorageClasses use 'no-provisioner'; no dynamic provisioning."
	}

	// Require at least one default dynamic SC
	hasDefault := false
	for _, sc := range dynamicSCs {
		if isStorageClassDefault(&sc) {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		return false, "No default dynamic StorageClass found."
	}

	// If we got here, all theoretical checks passed
	return true, ""
}

func isStorageClassDefault(sc *storagev1.StorageClass) bool {
	if sc.Annotations == nil {
		return false
	}
	if sc.Annotations[annDefaultStorageClass] == "true" {
		return true
	}
	if sc.Annotations[annBetaDefaultStorageClass] == "true" {
		return true
	}
	return false
}

// ---------------------------------------------------------------------
// Resource creation/waiting
// ---------------------------------------------------------------------
func createNamespace(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_, err := clientset.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{})
	return err
}

func createTestPVC(ctx context.Context, clientset *kubernetes.Clientset, namespace, pvcName, size string) error {
	qty, err := resource.ParseQuantity(size)
	if err != nil {
		return fmt.Errorf("invalid size quantity %q: %w", size, err)
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: qty,
				},
			},
		},
	}
	_, createErr := clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	return createErr
}

func createTestPod(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, pvcName string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "pv-check-container",
					Image: "registry.k8s.io/pause:3.9",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "pvc-volume",
							MountPath: "/test",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "pvc-volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}
	_, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func waitForPVCBound(ctx context.Context, clientset *kubernetes.Clientset, ns, pvcName string, timeout time.Duration) error {
	return wait.PollImmediate(3*time.Second, timeout, func() (bool, error) {
		pvc, err := clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pvc.Status.Phase == corev1.ClaimBound {
			return true, nil
		}
		return false, nil
	})
}

func waitForPodRunningOrSucceeded(ctx context.Context, clientset *kubernetes.Clientset, ns, podName string, timeout time.Duration) error {
	return wait.PollImmediate(3*time.Second, timeout, func() (bool, error) {
		pod, err := clientset.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		switch pod.Status.Phase {
		case corev1.PodRunning, corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed:
			return false, fmt.Errorf("pod failed")
		default:
			return false, nil
		}
	})
}

func deleteNamespace(ctx context.Context, clientset *kubernetes.Clientset, ns string) error {
	return clientset.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
}

func failResult(totalNodes int, reason string) *PVCheckResult {
	log.Printf("Dynamic PV check failed: %s", reason)
	return &PVCheckResult{
		PassedCount:   0,
		FailedCount:   totalNodes,
		TotalNodes:    totalNodes,
		ResultMessage: "Failed",
	}
}
