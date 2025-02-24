package ebpfcheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"k8s.io/client-go/kubernetes"

	"github.com/kubescape/sizing-checker/pkg/common"
)

func RunEbpfCheck(ctx context.Context, clientset *kubernetes.Clientset, clusterData *common.ClusterData, inCluster bool) *common.EbpfResult {
	ebpfRes := &common.EbpfResult{ResultMessage: "Passed"} // default

	// 1) Always check if any node has a kernel <4.4
	//    We'll gather all kernel versions from clusterData, parse them, track if any is <4.4
	var olderKernelNodes []string
	for _, node := range clusterData.Nodes {
		kernelVer := node.Status.NodeInfo.KernelVersion
		major, minor, _, err := parseKernelVersion(kernelVer)
		if err != nil {
			// If we cannot parse, we just continue or log a note. Let's continue safely.
			continue
		}
		if major < 4 || (major == 4 && minor < 4) {
			olderKernelNodes = append(olderKernelNodes, fmt.Sprintf("%s (v=%s)", node.Name, kernelVer))
		}
	}

	// If there are nodes older than 4.4, set a WARNING
	if len(olderKernelNodes) > 0 {
		ebpfRes.ResultMessage = fmt.Sprintf("Warning: Some nodes have kernel <4.4 => %v", olderKernelNodes)
		// We continue with further checks, but we keep track that at least some nodes might be missing full eBPF
	}

	// 2) If we're NOT inCluster => We skip local file checks and just return
	if !inCluster {
		// We do not read local /boot/config or /sys/kernel/btf, because we're external
		return ebpfRes
	}

	// 3) If inCluster => attempt local checks. We need to find the kernel version
	//    for "this" node. If clusterData has exactly 1 node or if we can identify
	//    the node name in the environment, you can do a more precise match.
	localKernelVersion, err := findLocalKernelVersion(clusterData)
	if err != nil {
		// Fallback: read from /proc/sys/kernel/osrelease
		localKernelVersion, err = getLocalKernelVersion()
		if err != nil {
			// If we still cannot determine local kernel, we cannot do local checks
			ebpfRes.ResultMessage = combineMessages(ebpfRes.ResultMessage,
				"Skipping local eBPF config checks: cannot determine local kernel version")
			return ebpfRes
		}
	}

	configPath := "/boot/config-" + localKernelVersion
	configData, err := os.ReadFile(configPath)
	if err != nil {
		ebpfRes.ResultMessage = combineMessages(ebpfRes.ResultMessage,
			fmt.Sprintf("Could not read kernel config at '%s'", configPath))
	} else {
		if err := checkEBPFConfigFlags(string(configData)); err != nil {
			ebpfRes.ResultMessage = combineMessages(ebpfRes.ResultMessage, err.Error())
		}
	}

	if err := checkBTFSupport(localKernelVersion, configData); err != nil {
		ebpfRes.ResultMessage = combineMessages(ebpfRes.ResultMessage, err.Error())
	}

	return ebpfRes
}

func findLocalKernelVersion(clusterData *common.ClusterData) (string, error) {
	if len(clusterData.Nodes) == 1 {
		return clusterData.Nodes[0].Status.NodeInfo.KernelVersion, nil
	}
	return "", errors.New("could not uniquely identify local node kernel version from clusterData")
}

// getLocalKernelVersion reads the kernel version from /proc/sys/kernel/osrelease.
func getLocalKernelVersion() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return "", fmt.Errorf("cannot read /proc/sys/kernel/osrelease: %w", err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", errors.New("local kernel version is empty")
	}
	return version, nil
}

// parseKernelVersion extracts major, minor, patch from a string like "5.4.0-104-generic".
func parseKernelVersion(versionStr string) (uint, uint, uint, error) {
	re := regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(versionStr)
	if len(matches) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid kernel version format: %s", versionStr)
	}
	var major, minor, patch uint
	if _, err := fmt.Sscanf(matches[0], "%d.%d.%d", &major, &minor, &patch); err != nil {
		// If there's no patch part, attempt parsing just %d.%d
		if _, err2 := fmt.Sscanf(matches[0], "%d.%d", &major, &minor); err2 != nil {
			return 0, 0, 0, fmt.Errorf("unable to parse kernel version %q: %v", matches[0], err)
		}
	}
	return major, minor, patch, nil
}

// checkEBPFConfigFlags ensures the essential flags are present: CONFIG_BPF=y, CONFIG_BPF_SYSCALL=y,
// and optionally checks CONFIG_DEBUG_INFO_BTF=y (though BTF is also checked separately).
func checkEBPFConfigFlags(configContent string) error {
	requiredFlags := []string{"CONFIG_BPF=y", "CONFIG_BPF_SYSCALL=y"}
	var missing []string
	lines := strings.Split(configContent, "\n")
	for _, f := range requiredFlags {
		if !stringSliceContains(lines, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing kernel config flags: %v", missing)
	}
	return nil
}

// checkBTFSupport checks for /sys/kernel/btf/vmlinux or well-known fallback paths
// or "CONFIG_DEBUG_INFO_BTF=y" in the kernel config.
func checkBTFSupport(kernelRelease string, configData []byte) error {
	// 1) If /sys/kernel/btf/vmlinux exists => OK
	if fileExists("/sys/kernel/btf/vmlinux") {
		return nil
	}
	// 2) Check fallback paths
	fallbacks := []string{
		"/boot/vmlinux-" + kernelRelease,
		"/lib/modules/" + kernelRelease + "/vmlinux",
	}
	for _, p := range fallbacks {
		if fileExists(p) {
			return nil
		}
	}
	// 3) If config was readable, see if it has CONFIG_DEBUG_INFO_BTF=y
	if len(configData) > 0 {
		lines := strings.Split(string(configData), "\n")
		if stringSliceContains(lines, "CONFIG_DEBUG_INFO_BTF=y") {
			return nil
		}
	}
	return errors.New("BTF support not detected on local node")
}

// -------------------------------------------------------------------------
// Utility helpers
// -------------------------------------------------------------------------

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func stringSliceContains(lines []string, candidate string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) == candidate {
			return true
		}
	}
	return false
}

// combineMessages merges an existing message with new info, typically used to collect warnings.
func combineMessages(existing, newPart string) string {
	if existing == "" {
		return newPart
	}
	if existing == "Passed" {
		// Overwrite the default "Passed" if there's new info
		return newPart
	}
	return existing + " | " + newPart
}
