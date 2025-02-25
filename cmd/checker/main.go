package main

import (
	"context"
	"log"

	"github.com/kubescape/sizing-checker/pkg/checks/connectivitycheck"
	"github.com/kubescape/sizing-checker/pkg/checks/ebpfcheck"
	"github.com/kubescape/sizing-checker/pkg/checks/pvcheck"
	"github.com/kubescape/sizing-checker/pkg/checks/sizing"
	"github.com/kubescape/sizing-checker/pkg/common"
)

func main() {
	clientset, inCluster := common.BuildKubeClient()
	if clientset == nil {
		log.Fatal("Could not create kube client. Exiting.")
	}

	ctx := context.Background()

	// 1) Collect cluster data
	clusterData, err := common.CollectClusterData(ctx, clientset)
	if err != nil {
		log.Printf("Failed to collect cluster data: %v", err)
	}

	// 2) Run checks
	sizingResult := sizing.RunSizingChecker(clusterData)
	pvResult := pvcheck.RunPVProvisioningCheck(ctx, clientset, clusterData, inCluster)
	connectivityResult := connectivitycheck.RunConnectivityChecks(ctx, clientset, clusterData, inCluster)
	ebpfResult := ebpfcheck.RunEbpfCheck(ctx, clientset, clusterData, inCluster)

	// 3) Build and export the final ReportData
	finalReport := common.BuildReportData(clusterData, sizingResult, pvResult, connectivityResult, ebpfResult)

	// If NOT using --active-checks, add a note to the HTML to clarify
	if inCluster {
		finalReport.InCluster = true
	} else {
		finalReport.InCluster = false
	}

	common.GenerateOutput(finalReport, inCluster)
}
