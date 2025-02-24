package connectivitytargets

import (
	_ "embed"
	"strings"
)

//go:embed connectivity_targets.txt
var defaultTargetsRaw string

// GetDefaultTargets returns a list of default addresses to test connectivity on port 443.
// This is either read from an embedded file or can be adjusted to a static slice if preferred.
func GetDefaultTargets() []string {
	lines := strings.Split(strings.ReplaceAll(defaultTargetsRaw, "\r\n", "\n"), "\n")
	var targets []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		targets = append(targets, line)
	}
	return targets
}
