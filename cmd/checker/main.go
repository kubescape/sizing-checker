package main

import (
	"context"
	"flag"
	"log"

	"github.com/kubescape/sizing-checker/pkg/checks/connectivity"
	"github.com/kubescape/sizing-checker/pkg/checks/pvcheck"
	"github.com/kubescape/sizing-checker/pkg/checks/sizing"
	"github.com/kubescape/sizing-checker/pkg/common"
)

func main() {
	// Define and parse our flag for active checks
	activeChecks := flag.Bool("active-checks", false, "If set, run checks that require resource deployment on the cluster.")
	flag.Parse()

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
	pvResult := pvcheck.RunPVProvisioningCheck(ctx, clientset, clusterData, activeChecks)
	connectivityResult := connectivity.RunConnectivityChecks(ctx, clientset, clusterData, inCluster)

	// 3) Build and export the final ReportData
	finalReport := common.BuildReportData(clusterData, sizingResult, pvResult, connectivityResult)

	// If NOT using --active-checks, add a note to the HTML to clarify
	if !*activeChecks {
		finalReport.ActiveCheckNote = true
	}

	common.GenerateOutput(finalReport, inCluster)
}
