package pvcheck

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kubescape/sizing-checker/pkg/common"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	annDefaultStorageClass     = "storageclass.kubernetes.io/is-default-class"
	annBetaDefaultStorageClass = "storageclass.beta.kubernetes.io/is-default-class"
	noProvisioner              = "kubernetes.io/no-provisioner"
)

// RunPVProvisioningCheck decides if we run a full test (PVC/Pod existence check) or just a basic check.
func RunPVProvisioningCheck(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	clusterData *common.ClusterData,
	inCluster bool,
) *common.PVCheckResult {

	if inCluster {
		// Full test: expect PVC + Pod to already exist
		return runFullProvisioningTest(ctx, clientset, clusterData)
	}

	// Only run the basic pre-check:
	passed, failReason := BasicPreCheck(ctx, clientset, clusterData)
	if passed {
		// Basic check passed => "Passed"
		return &common.PVCheckResult{
			PassedCount:   len(clusterData.Nodes),
			FailedCount:   0,
			TotalNodes:    len(clusterData.Nodes),
			ResultMessage: "Passed",
		}
	}

	// Basic check failed => "Failed"
	log.Printf("Basic PV check failed: %s", failReason)
	return &common.PVCheckResult{
		PassedCount:   0,
		FailedCount:   len(clusterData.Nodes),
		TotalNodes:    len(clusterData.Nodes),
		ResultMessage: "Failed",
	}
}

// runFullProvisioningTest verifies that:
// 1) Basic pre-check passes (we have at least one default dynamic SC, etc.)
// 2) Waits up to 10 seconds for the PVC to become Bound, and also checks for the Pod’s existence.
// 3) If both checks pass, we confirm the PVC’s backing PV is present and Bound.
func runFullProvisioningTest(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	clusterData *common.ClusterData,
) *common.PVCheckResult {

	// 1) Pre-check for dynamic provisioning
	passed, failReason := BasicPreCheck(ctx, clientset, clusterData)
	if !passed {
		return failResult(len(clusterData.Nodes), failReason)
	}

	namespace := "kubescape-prerequisite"
	pvcName := "kubescape-pv-check-pvc"
	podName := "kubescape-pv-check-pod"
	timeout := 10 * time.Second

	// 2) Wait for PVC to become Bound
	pvc, err := waitForPVCBound(ctx, clientset, namespace, pvcName, timeout)
	if err != nil {
		// Distinguish between an actual error vs. a timeout
		if ctx.Err() != nil {
			return failWarningResult(len(clusterData.Nodes),
				"Could not complete the check. The PVC required for the check did not become Bound within the timeout.")
		}
		return failResult(len(clusterData.Nodes), fmt.Sprintf("Error waiting for PVC to be Bound: %v", err))
	}

	// 2) Also wait up to 10 seconds for the Pod to appear
	err = waitForPodExists(ctx, clientset, namespace, podName, timeout)
	if err != nil {
		if ctx.Err() != nil {
			return failWarningResult(len(clusterData.Nodes),
				"Could not complete the check. The Pod required for the check was not found within 10 seconds.")
		}
		return failResult(len(clusterData.Nodes), fmt.Sprintf("Error checking Pod existence: %v", err))
	}

	// 3) Check the backing PV is present and Bound
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return failResult(len(clusterData.Nodes), "PVC is Bound but has empty volume name; cannot find PV.")
	}

	pv, err := clientset.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		return failResult(len(clusterData.Nodes), fmt.Sprintf("Failed retrieving PV %q: %v", pvName, err))
	}
	if pv.Status.Phase != corev1.VolumeBound {
		return failResult(len(clusterData.Nodes),
			fmt.Sprintf("PV %q is present but not Bound (current phase: %s)", pvName, pv.Status.Phase))
	}

	// If everything was successful => "Passed"
	return &common.PVCheckResult{
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

// waitForPVCBound waits up to 'timeout' for the PVC to exist and transition to Bound.
func waitForPVCBound(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	ns, pvcName string,
	timeout time.Duration,
) (*corev1.PersistentVolumeClaim, error) {

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var pvc *corev1.PersistentVolumeClaim
	err := wait.PollUntilContextTimeout(
		checkCtx,
		time.Second, // poll interval
		timeout,
		true,
		func(ctx context.Context) (bool, error) {
			tempPVC, getErr := clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName, metav1.GetOptions{})
			if apierrors.IsNotFound(getErr) {
				// Not found yet, keep polling
				return false, nil
			}
			if getErr != nil {
				return false, getErr
			}

			pvc = tempPVC
			if pvc.Status.Phase == corev1.ClaimBound {
				return true, nil
			}
			// Otherwise, keep polling
			return false, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return pvc, nil
}

// waitForPodExists uses PollUntilContextTimeout to check for existence of the Pod.
func waitForPodExists(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	ns, podName string,
	timeout time.Duration,
) error {

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wait.PollUntilContextTimeout(
		checkCtx,
		1*time.Second,
		timeout,
		true,
		func(ctx context.Context) (bool, error) {
			_, err := clientset.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				// Not found yet, keep polling
				return false, nil
			}
			return (err == nil), err
		},
	)
}

// failResult is a helper to generate a PVCheckResult with "Failed".
func failResult(totalNodes int, reason string) *common.PVCheckResult {
	log.Printf("Dynamic PV check failed: %s", reason)
	return &common.PVCheckResult{
		PassedCount:   0,
		FailedCount:   totalNodes,
		TotalNodes:    totalNodes,
		ResultMessage: "Failed",
	}
}

// failWarningResult is a helper to generate a PVCheckResult with a "warning" message.
func failWarningResult(totalNodes int, reason string) *common.PVCheckResult {
	log.Printf("Dynamic PV check warning: %s", reason)
	return &common.PVCheckResult{
		PassedCount:   0,
		FailedCount:   totalNodes,
		TotalNodes:    totalNodes,
		ResultMessage: fmt.Sprintf("Warning: %s", reason),
	}
}
