package common

import (
	_ "embed"
)

//go:embed templates/prerequisites-report.html
var PrerequisitesReportHTML string

//go:embed templates/review-values.html
var ReviewValuesHTML string
