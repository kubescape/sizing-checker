package main

import (
	"context"
	"flag"
	"log"

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

	// 2) Run configuration-based checks
	sizingResult := sizing.RunSizingChecker(clusterData)

	var pvResult *pvcheck.PVCheckResult
	if *activeChecks {
		// Full test with PVC + Pod creation
		pvResult = pvcheck.RunPVProvisioningCheck(ctx, clientset, clusterData)
	} else {
		// Only run the basic pre-check. If it passes => "Passed", if it fails => "Failed".
		passed, failReason := pvcheck.BasicPreCheck(ctx, clientset, clusterData)
		if passed {
			pvResult = &pvcheck.PVCheckResult{
				PassedCount:   len(clusterData.Nodes),
				FailedCount:   0,
				TotalNodes:    len(clusterData.Nodes),
				ResultMessage: "Passed",
			}
		} else {
			log.Printf("Basic PV check failed: %s", failReason)
			pvResult = &pvcheck.PVCheckResult{
				PassedCount:   0,
				FailedCount:   len(clusterData.Nodes),
				TotalNodes:    len(clusterData.Nodes),
				ResultMessage: "Failed",
			}
		}
	}

	// 4) Build and export the final ReportData
	finalReport := common.BuildReportData(clusterData, sizingResult)
	finalReport.PVProvisioningMessage = pvResult.ResultMessage

	// If NOT using --active-checks, add a note to the HTML to clarify
	if !*activeChecks {
		finalReport.ActiveCheckNote = true
	}

	common.GenerateOutput(finalReport, inCluster)
}
