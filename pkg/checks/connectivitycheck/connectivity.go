package connectivitycheck

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/kubescape/sizing-checker/pkg/common"
	"github.com/kubescape/sizing-checker/pkg/common/connectivitytargets"
	"k8s.io/client-go/kubernetes"
)

func RunConnectivityChecks(ctx context.Context, clientset *kubernetes.Clientset, clusterData *common.ClusterData, inCluster bool) *common.ConnectivityCheckResult {
	// If not running in-cluster, skip this check entirely.
	if !inCluster {
		return &common.ConnectivityCheckResult{
			AddressesTested: nil,
			SuccessCount:    0,
			ResultMessage:   "Skipped",
		}
	}

	// Determine the list of targets to test:
	//  1) if an environment variable "CONNECTIVITY_TARGETS" is provided, parse it as a comma-separated list
	//  2) otherwise, use the default embedded list in connectivitytargets.
	var targets []string
	targetsEnv := os.Getenv("CONNECTIVITY_TARGETS")
	if targetsEnv != "" {
		for _, t := range strings.Split(targetsEnv, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				targets = append(targets, t)
			}
		}
	} else {
		targets = connectivitytargets.GetDefaultTargets()
	}

	// Perform connectivity checks (TCP dial on port 443)
	successCount := 0
	timeout := 5 * time.Second
	for _, addr := range targets {
		targetHostPort := fmt.Sprintf("%s:443", addr)
		conn, err := net.DialTimeout("tcp", targetHostPort, timeout)
		if err != nil {
			log.Printf("Failed to connect to %s: %v", targetHostPort, err)
			continue
		}
		_ = conn.Close() // close as soon as we succeed
		successCount++
	}

	// Determine final message
	resultMsg := "Failed"
	if successCount == len(targets) {
		resultMsg = "Passed"
	} else if successCount > 0 && successCount < len(targets) {
		resultMsg = fmt.Sprintf("Partial success (%d/%d)", successCount, len(targets))
	}

	return &common.ConnectivityCheckResult{
		AddressesTested: targets,
		SuccessCount:    successCount,
		ResultMessage:   resultMsg,
	}
}
