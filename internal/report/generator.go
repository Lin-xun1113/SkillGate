package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/metrics"
	"github.com/Lin-xun1113/SkillGate/internal/releasegate"
	"github.com/Lin-xun1113/SkillGate/internal/statistics"
	"github.com/xeipuuv/gojsonschema"
)

//go:embed schema/report.schema.json
var reportSchemaJSON []byte

// Report represents a complete experiment report
type Report struct {
	Schema            string                      `json:"schema"`
	ExperimentID      string                      `json:"experiment_id"`
	Metadata          ReportMetadata              `json:"metadata"`
	Summary           ReportSummary               `json:"summary"`
	CaseResults       []CaseResult                `json:"case_results"`
	PassAtK           map[string]float64          `json:"pass_at_k,omitempty"`
	ResourceUsage     ResourceUsage               `json:"resource_usage,omitempty"`
	InvalidPairs      []InvalidPair               `json:"invalid_pairs"`
	StatisticalMethod StatisticalMethod           `json:"statistical_method"`
	Decision          *releasegate.ReportDecision `json:"decision,omitempty"`
}

// ReportMetadata contains report metadata
type ReportMetadata struct {
	ExperimentName string    `json:"experiment_name"`
	CreatedAt      time.Time `json:"created_at"`
	TotalTrials    int       `json:"total_trials"`
	ValidPairs     int       `json:"valid_pairs"`
	InvalidPairs   int       `json:"invalid_pairs"`
}

// ReportSummary contains aggregate metrics
type ReportSummary struct {
	MeanLift                 float64 `json:"mean_lift"`
	CILower                  float64 `json:"ci_lower"`
	CIUpper                  float64 `json:"ci_upper"`
	NCases                   int     `json:"n_cases"`
	StatisticallySignificant bool    `json:"statistically_significant"`
	Method                   string  `json:"method"`
	NumResamples             int     `json:"num_resamples"`
}

// CaseResult represents a single case's result
type CaseResult struct {
	CaseID               string  `json:"case_id"`
	BaselineScore        float64 `json:"baseline_score"`
	CandidateScore       float64 `json:"candidate_score"`
	Difference           float64 `json:"difference"`
	BaselineRepetitions  int     `json:"baseline_repetitions"`
	CandidateRepetitions int     `json:"candidate_repetitions"`
}

// ResourceUsage contains resource usage metrics
type ResourceUsage struct {
	TokenDelta   statistics.ResourceDelta `json:"token_delta"`
	LatencyDelta statistics.ResourceDelta `json:"latency_delta"`
}

// InvalidPair represents an invalid pairing with reason
type InvalidPair struct {
	CaseID string `json:"case_id"`
	Reason string `json:"reason"`
}

// StatisticalMethod describes the statistical methods used
type StatisticalMethod struct {
	BootstrapMethod   string  `json:"bootstrap_method"`
	CIMethod          string  `json:"ci_method"`
	SignificanceLevel float64 `json:"significance_level"`
}

// Generator generates experiment reports
type Generator struct {
	outputDir string
}

// NewGenerator creates a new report generator
func NewGenerator(outputDir string) *Generator {
	return &Generator{
		outputDir: outputDir,
	}
}

// Generate generates a complete report. passAtK and resourceUsage may be nil
// when there isn't enough data to compute them (e.g. fewer than 2 valid pairs,
// or no recorded token/latency usage) — the corresponding report sections are
// then omitted rather than populated with misleading zero values.
func (g *Generator) Generate(
	experimentID string,
	experimentName string,
	pairs []metrics.Pair,
	bootstrapResult *statistics.BootstrapResult,
	bootstrapConfig statistics.BootstrapConfig,
	totalTrials int,
	passAtK map[string]float64,
	resourceUsage *ResourceUsage,
) (*Report, error) {
	// Count valid and invalid pairs
	validPairs := 0
	var invalidPairs []InvalidPair
	for _, pair := range pairs {
		if pair.Valid {
			validPairs++
		} else {
			invalidPairs = append(invalidPairs, InvalidPair{
				CaseID: pair.CaseID,
				Reason: pair.InvalidReason,
			})
		}
	}
	if invalidPairs == nil {
		invalidPairs = []InvalidPair{}
	}

	// Build case results
	var caseResults []CaseResult
	for _, pair := range pairs {
		if pair.Valid {
			caseResults = append(caseResults, CaseResult{
				CaseID:               pair.CaseID,
				BaselineScore:        pair.BaselineScore,
				CandidateScore:       pair.CandidateScore,
				Difference:           pair.Difference,
				BaselineRepetitions:  pair.BaselineRepetitions,
				CandidateRepetitions: pair.CandidateRepetitions,
			})
		}
	}
	if caseResults == nil {
		caseResults = []CaseResult{}
	}

	// Determine statistical significance
	// CI is statistically significant if it doesn't cross zero
	statisticallySignificant := false
	var summary ReportSummary
	if bootstrapResult != nil {
		statisticallySignificant = (bootstrapResult.CILower > 0 && bootstrapResult.CIUpper > 0) ||
			(bootstrapResult.CILower < 0 && bootstrapResult.CIUpper < 0)
		summary = ReportSummary{
			MeanLift:                 bootstrapResult.MeanEstimate,
			CILower:                  bootstrapResult.CILower,
			CIUpper:                  bootstrapResult.CIUpper,
			NCases:                   bootstrapResult.NCases,
			StatisticallySignificant: statisticallySignificant,
			Method:                   bootstrapResult.Method,
			NumResamples:             bootstrapConfig.NumResamples,
		}
	}

	report := &Report{
		Schema:       "skillgate.report.v1",
		ExperimentID: experimentID,
		Metadata: ReportMetadata{
			ExperimentName: experimentName,
			CreatedAt:      time.Now(),
			TotalTrials:    totalTrials,
			ValidPairs:     validPairs,
			InvalidPairs:   len(invalidPairs),
		},
		Summary:      summary,
		CaseResults:  caseResults,
		PassAtK:      passAtK,
		InvalidPairs: invalidPairs,
		StatisticalMethod: StatisticalMethod{
			BootstrapMethod:   "cluster_bootstrap",
			CIMethod:          "percentile",
			SignificanceLevel: 0.05,
		},
	}
	if resourceUsage != nil {
		report.ResourceUsage = *resourceUsage
	}

	return report, nil
}

// ValidateSchema validates a marshalled report against the SkillGate report JSON Schema.
func ValidateSchema(reportJSON []byte) error {
	schemaLoader := gojsonschema.NewBytesLoader(reportSchemaJSON)
	documentLoader := gojsonschema.NewBytesLoader(reportJSON)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("schema validation error: %w", err)
	}
	if !result.Valid() {
		var errs []string
		for _, desc := range result.Errors() {
			errs = append(errs, desc.String())
		}
		return fmt.Errorf("report failed schema validation: %s", strings.Join(errs, "; "))
	}
	return nil
}

// SaveJSON saves report as JSON to <outputDir>/report.json. The caller is
// expected to pass an outputDir already scoped to the experiment
// (artifacts/<experiment_id>), matching D4's storage layout.
func (g *Generator) SaveJSON(report *Report) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create report directory: %w", err)
	}

	reportPath := filepath.Join(g.outputDir, "report.json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := ValidateSchema(data); err != nil {
		return "", fmt.Errorf("report failed schema validation: %w", err)
	}

	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write report: %w", err)
	}

	return reportPath, nil
}

// SaveMarkdown saves report as Markdown to <outputDir>/report.md.
func (g *Generator) SaveMarkdown(report *Report) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create report directory: %w", err)
	}

	var md strings.Builder

	// Header
	md.WriteString(fmt.Sprintf("# Experiment Report: %s\n\n", report.Metadata.ExperimentName))
	md.WriteString(fmt.Sprintf("**Experiment ID:** %s  \n", report.ExperimentID))
	md.WriteString(fmt.Sprintf("**Generated:** %s  \n", report.Metadata.CreatedAt.Format(time.RFC3339)))
	md.WriteString(fmt.Sprintf("**Status:** Completed\n\n"))

	// Executive Summary
	md.WriteString("## Executive Summary\n\n")
	md.WriteString(fmt.Sprintf("- **Skill Lift:** %.3f (95%% CI: [%.3f, %.3f])\n",
		report.Summary.MeanLift, report.Summary.CILower, report.Summary.CIUpper))

	significance := "No"
	if report.Summary.StatisticallySignificant {
		significance = "Yes"
	}
	md.WriteString(fmt.Sprintf("- **Statistically Significant:** %s\n", significance))
	md.WriteString(fmt.Sprintf("- **Valid Pairs:** %d / %d\n\n",
		report.Metadata.ValidPairs, report.Metadata.ValidPairs+report.Metadata.InvalidPairs))

	// Case-level Results
	md.WriteString("## Case-level Results\n\n")
	md.WriteString("| Case ID | Baseline | Candidate | Difference |\n")
	md.WriteString("|---------|----------|-----------|------------|\n")

	for _, cr := range report.CaseResults {
		md.WriteString(fmt.Sprintf("| %s | %.3f | %.3f | %+.3f |\n",
			cr.CaseID, cr.BaselineScore, cr.CandidateScore, cr.Difference))
	}
	md.WriteString("\n")

	// Resource Usage
	md.WriteString("## Resource Usage\n\n")
	if report.ResourceUsage.TokenDelta.BaselineMean > 0 || report.ResourceUsage.TokenDelta.CandidateMean > 0 {
		md.WriteString(fmt.Sprintf("- **Token Delta:** %+.0f tokens (%+.1f%%)\n",
			report.ResourceUsage.TokenDelta.Delta,
			report.ResourceUsage.TokenDelta.DeltaRatio*100))
		md.WriteString(fmt.Sprintf("- **Latency Delta:** %+.0f ms (%+.1f%%)\n\n",
			report.ResourceUsage.LatencyDelta.Delta,
			report.ResourceUsage.LatencyDelta.DeltaRatio*100))
	} else {
		md.WriteString("No resource usage data recorded for this experiment.\n\n")
	}
	if report.Decision != nil {
		md.WriteString("## Release Decision\n\n")
		md.WriteString(fmt.Sprintf("- **Result:** %s\n", report.Decision.Result))
		md.WriteString(fmt.Sprintf("- **Policy Hash:** %s\n", report.Decision.PolicyHash))
		md.WriteString(fmt.Sprintf("- **Snapshot Hash:** %s\n", report.Decision.SnapshotHash))
		if len(report.Decision.MatchedRules) > 0 {
			md.WriteString(fmt.Sprintf("- **Matched Rules:** %s\n", strings.Join(report.Decision.MatchedRules, ", ")))
		}
		md.WriteString(fmt.Sprintf("- **Explanation:** %s\n\n", report.Decision.Explanation))
	}

	// Statistical Method
	md.WriteString("## Statistical Method\n\n")
	md.WriteString(fmt.Sprintf("- **Bootstrap:** %s (%d resamples)\n",
		report.StatisticalMethod.BootstrapMethod, report.Summary.NumResamples))
	md.WriteString(fmt.Sprintf("- **CI Method:** %s\n", report.StatisticalMethod.CIMethod))
	md.WriteString(fmt.Sprintf("- **Significance level:** α=%.2f\n\n", report.StatisticalMethod.SignificanceLevel))

	// Failure Analysis
	md.WriteString("## Failure Analysis\n\n")
	if len(report.InvalidPairs) > 0 {
		md.WriteString(fmt.Sprintf("**Invalid Pairs:** %d\n\n", len(report.InvalidPairs)))

		for _, ip := range report.InvalidPairs {
			md.WriteString(fmt.Sprintf("- **%s**: %s\n", ip.CaseID, ip.Reason))
		}
		md.WriteString("\n")
	} else {
		md.WriteString("No invalid pairs. All cases paired successfully.\n\n")
	}

	reportPath := filepath.Join(g.outputDir, "report.md")
	if err := os.WriteFile(reportPath, []byte(md.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write markdown report: %w", err)
	}

	return reportPath, nil
}

// SaveHTML saves report as HTML (basic implementation) to <outputDir>/report.html.
func (g *Generator) SaveHTML(report *Report) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create report directory: %w", err)
	}

	var html strings.Builder

	html.WriteString("<!DOCTYPE html>\n<html>\n<head>\n")
	html.WriteString(fmt.Sprintf("<title>Report: %s</title>\n", report.Metadata.ExperimentName))
	html.WriteString("<style>\n")
	html.WriteString("body { font-family: sans-serif; margin: 40px; }\n")
	html.WriteString("table { border-collapse: collapse; width: 100%; }\n")
	html.WriteString("th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }\n")
	html.WriteString("th { background-color: #f2f2f2; }\n")
	html.WriteString(".positive { color: green; }\n")
	html.WriteString(".negative { color: red; }\n")
	html.WriteString("</style>\n")
	html.WriteString("</head>\n<body>\n")

	html.WriteString(fmt.Sprintf("<h1>Experiment Report: %s</h1>\n", report.Metadata.ExperimentName))
	html.WriteString(fmt.Sprintf("<p><strong>Experiment ID:</strong> %s</p>\n", report.ExperimentID))
	html.WriteString(fmt.Sprintf("<p><strong>Generated:</strong> %s</p>\n", report.Metadata.CreatedAt.Format(time.RFC3339)))

	html.WriteString("<h2>Executive Summary</h2>\n")
	html.WriteString("<ul>\n")
	html.WriteString(fmt.Sprintf("<li><strong>Skill Lift:</strong> %.3f (95%% CI: [%.3f, %.3f])</li>\n",
		report.Summary.MeanLift, report.Summary.CILower, report.Summary.CIUpper))
	html.WriteString(fmt.Sprintf("<li><strong>Statistically Significant:</strong> %v</li>\n",
		report.Summary.StatisticallySignificant))
	html.WriteString(fmt.Sprintf("<li><strong>Valid Pairs:</strong> %d / %d</li>\n",
		report.Metadata.ValidPairs, report.Metadata.ValidPairs+report.Metadata.InvalidPairs))
	html.WriteString("</ul>\n")

	html.WriteString("<h2>Case-level Results</h2>\n")
	html.WriteString("<table>\n")
	html.WriteString("<tr><th>Case ID</th><th>Baseline</th><th>Candidate</th><th>Difference</th></tr>\n")

	for _, cr := range report.CaseResults {
		diffClass := ""
		if cr.Difference > 0 {
			diffClass = "positive"
		} else if cr.Difference < 0 {
			diffClass = "negative"
		}
		html.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%.3f</td><td>%.3f</td><td class=\"%s\">%+.3f</td></tr>\n",
			cr.CaseID, cr.BaselineScore, cr.CandidateScore, diffClass, cr.Difference))
	}
	html.WriteString("</table>\n")

	if report.Decision != nil {
		html.WriteString("<h2>Release Decision</h2>\n")
		html.WriteString(fmt.Sprintf("<ul><li><strong>Result:</strong> %s</li>\n", report.Decision.Result))
		html.WriteString(fmt.Sprintf("<li><strong>Policy Hash:</strong> %s</li>\n", report.Decision.PolicyHash))
		html.WriteString(fmt.Sprintf("<li><strong>Snapshot Hash:</strong> %s</li>\n", report.Decision.SnapshotHash))
		if len(report.Decision.MatchedRules) > 0 {
			html.WriteString(fmt.Sprintf("<li><strong>Matched Rules:</strong> %s</li>\n", strings.Join(report.Decision.MatchedRules, ", ")))
		}
		html.WriteString(fmt.Sprintf("<li><strong>Explanation:</strong> %s</li></ul>\n", report.Decision.Explanation))
	}

	html.WriteString("</body>\n</html>\n")

	reportPath := filepath.Join(g.outputDir, "report.html")
	if err := os.WriteFile(reportPath, []byte(html.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write HTML report: %w", err)
	}

	return reportPath, nil
}
