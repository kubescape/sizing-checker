package common

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// BuildReportData constructs a ReportData struct from the cluster details and check results.
// It processes resource allocations, node information, and storage class data to generate
// a comprehensive report for the user.
func BuildReportData(
	cd *ClusterData,
	sr *SizingResult,
	pr *PVCheckResult,
	ccr *ConnectivityResult,
	er *EbpfResult,
) *ReportData {

	report := &ReportData{
		// Sizing data
		TotalResources:             sr.TotalResources,
		MaxNodeCPUCapacity:         sr.MaxNodeCPUCapacity,
		MaxNodeMemoryMB:            sr.MaxNodeMemoryMB,
		LargestContainerImageMB:    sr.LargestContainerImageMB,
		DefaultResourceAllocations: sr.DefaultResourceAllocations,
		FinalResourceAllocations:   sr.FinalResourceAllocations,
		HasSizingAdjustments:       sr.HasSizingAdjustments,

		// Basic cluster details
		KubernetesVersion: cd.ClusterDetails.Version,
		CloudProvider:     cd.ClusterDetails.CloudProvider,
		K8sDistribution:   cd.ClusterDetails.K8sDistribution,
		TotalNodeCount:    cd.ClusterDetails.TotalNodeCount,
		TotalVCPUCount:    cd.ClusterDetails.TotalVCPUCount,

		GenerationTime:  time.Now().Format("2006-01-02 15:04:05"),
		FullClusterData: cd,

		PVProvisioningMessage:    pr.ResultMessage,
		ConnectivityCheckMessage: ccr.ResultMessage,
		EBPFResultMessage:        er.ResultMessage,
	}

	// Extract storage class names
	report.StorageClasses = make([]string, 0, len(cd.StorageClasses))
	for _, sc := range cd.StorageClasses {
		report.StorageClasses = append(report.StorageClasses, sc.Name)
	}

	// Node info summaries
	totalNodes := cd.ClusterDetails.TotalNodeCount
	ni := cd.NodeInfoSummaries

	report.NodeOSSummary = summarizeMap(ni.OperatingSystemCounts, totalNodes)
	report.NodeArchSummary = summarizeMap(ni.ArchitectureCounts, totalNodes)
	report.NodeKernelVersionSummary = summarizeMap(ni.KernelVersionCounts, totalNodes)
	report.NodeOSImageSummary = summarizeMap(ni.OSImageCounts, totalNodes)
	report.NodeContainerRuntimeSummary = summarizeMap(ni.ContainerRuntimeVersionCounts, totalNodes)
	report.NodeKubeletVersionSummary = summarizeMap(ni.KubeletVersionCounts, totalNodes)
	report.NodeKubeProxyVersionSummary = summarizeMap(ni.KubeProxyVersionCounts, totalNodes)

	return report
}

func BuildFullDumpYAML(cd *ClusterData) string {
	if cd == nil {
		return "Error building full cluster dump: cluster data is nil"
	}
	y, err := yaml.Marshal(cd)
	if err != nil {
		return fmt.Sprintf("Error building full cluster dump: %v", err)
	}
	return string(y)
}

func BuildHTMLReport(data *ReportData, tpl string) string {
	// Create a FuncMap and include any functions you want to use in your template
	funcMap := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}

	// Parse your template with FuncMap
	tmpl, err := template.New("report").Funcs(funcMap).Parse(tpl)
	if err != nil {
		return fmt.Sprintf("Error building report: %v", err)
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return fmt.Sprintf("Error rendering template: %v", err)
	}
	return sb.String()
}

func BuildReviewValuesHTML(data *ReportData, helmValuesContent string) string {
	// Create a FuncMap and include any functions you want to use in your template
	funcMap := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}

	// Parse the embedded review values template with FuncMap
	tmpl, err := template.New("review-values").Funcs(funcMap).Parse(ReviewValuesHTML)
	if err != nil {
		return fmt.Sprintf("Error building review values page: %v", err)
	}

	// Execute the template with the YAML content and storage classes
	templateData := struct {
		RecommendedValues     string
		StorageClasses        []string
		PVProvisioningMessage string
	}{
		RecommendedValues:     helmValuesContent,
		StorageClasses:        data.StorageClasses,
		PVProvisioningMessage: data.PVProvisioningMessage,
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, templateData); err != nil {
		return fmt.Sprintf("Error rendering review values template: %v", err)
	}
	return sb.String()
}

// BuildValuesYAML generates a YAML string containing the recommended Helm values
// based on the report data. It includes resource allocations and other necessary
// configurations for the cluster.
func BuildValuesYAML(d *ReportData) string {
	overrides := collectOverrides(d)

	if len(overrides) == 0 && d.PVProvisioningMessage != "Failed" {
		return "# no adjustments are required for the default values\n"
	}

	// Add persistence configuration if PV provisioning check failed
	if d.PVProvisioningMessage == "Failed" {
		overrides["configurations.persistence"] = "disable"
	}

	return convertOverridesToYAML(overrides)
}

// collectOverrides gathers all the necessary Helm value overrides based on the report data.
// It processes resource allocations and other configurations to generate a map of
// overrides that should be applied to the default Helm values.
func collectOverrides(d *ReportData) map[string]string {
	overrides := map[string]string{}

	// Compare default vs. final resource allocations for each component
	for comp, defMap := range d.DefaultResourceAllocations {
		finalMap := d.FinalResourceAllocations[comp]
		if finalMap == nil {
			continue
		}

		// CPU request override
		if finalCPUReq, ok := finalMap["cpuReq"]; ok {
			if defaultCPUReq, okDef := defMap["cpuReq"]; okDef && finalCPUReq != defaultCPUReq {
				overrides[fmt.Sprintf("%s.resources.requests.cpu", comp)] = finalCPUReq
			}
		}

		// Memory request override
		if finalMemReq, ok := finalMap["memReq"]; ok {
			if defaultMemReq, okDef := defMap["memReq"]; okDef && finalMemReq != defaultMemReq {
				overrides[fmt.Sprintf("%s.resources.requests.memory", comp)] = finalMemReq
			}
		}

		// CPU limit override
		if finalCPULim, ok := finalMap["cpuLim"]; ok {
			if defaultCPULim, okDef := defMap["cpuLim"]; okDef && finalCPULim != defaultCPULim {
				overrides[fmt.Sprintf("%s.resources.limits.cpu", comp)] = finalCPULim
			}
		}

		// Memory limit override
		if finalMemLim, ok := finalMap["memLim"]; ok {
			if defaultMemLim, okDef := defMap["memLim"]; okDef && finalMemLim != defaultMemLim {
				overrides[fmt.Sprintf("%s.resources.limits.memory", comp)] = finalMemLim
			}
		}
	}

	return overrides
}

func convertOverridesToYAML(overrides map[string]string) string {
	// 1. Group by top-level prefix (everything up to the first ".").
	grouped := make(map[string]map[string]string)
	for fullKey, value := range overrides {
		parts := strings.SplitN(fullKey, ".", 2)
		topLevel := parts[0]
		subKey := ""
		if len(parts) > 1 {
			subKey = parts[1]
		}

		if _, exists := grouped[topLevel]; !exists {
			grouped[topLevel] = make(map[string]string)
		}
		grouped[topLevel][subKey] = value
	}

	// 2. Sort top-level keys for consistent output.
	var topKeys []string
	for k := range grouped {
		topKeys = append(topKeys, k)
	}
	sort.Strings(topKeys)

	// 3. Build YAML for each top-level group.
	var sb strings.Builder
	for _, topKey := range topKeys {
		sb.WriteString(fmt.Sprintf("%s:\n", topKey))

		subMap := grouped[topKey]
		var subKeys []string
		for k := range subMap {
			subKeys = append(subKeys, k)
		}
		sort.Strings(subKeys)

		for _, subKey := range subKeys {
			val := subMap[subKey]
			if subKey == "" {
				// If there's no subKey, just put the value at this level (unlikely in typical usage).
				sb.WriteString(fmt.Sprintf("  %s\n", val))
			} else {
				// Recursively build nested structure.
				buildNestedYAML(&sb, "  ", subKey, val)
			}
		}
	}

	return sb.String()
}

func buildNestedYAML(sb *strings.Builder, indent, subKey, value string) {
	parts := strings.SplitN(subKey, ".", 2)
	if len(parts) == 1 {
		sb.WriteString(fmt.Sprintf("%s%s: %s\n", indent, parts[0], value))
		return
	}

	sb.WriteString(fmt.Sprintf("%s%s:\n", indent, parts[0]))
	buildNestedYAML(sb, indent+"  ", parts[1], value)
}

func summarizeMap(counts map[string]int, totalCount int) string {
	if len(counts) == 1 {
		// If there's exactly one key, we might just return that key or "Linux (7)"
		for key, c := range counts {
			if c == totalCount {
				return key // e.g., all 10 are "Linux"
			}
			return fmt.Sprintf("%s (%d)", key, c)
		}
	}

	// multiple distinct keys: e.g. "Linux (7), Windows (3)"
	var parts []string
	for key, c := range counts {
		parts = append(parts, fmt.Sprintf("%s (%d)", key, c))
	}
	return strings.Join(parts, ", ")
}
