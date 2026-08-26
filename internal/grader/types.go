package grader

import (
	"time"
)

// GraderType represents the type of grader
type GraderType string

const (
	GraderTypeDeterministic GraderType = "deterministic"
	GraderTypeLLM           GraderType = "llm"
)

// GraderMethod represents the specific grading method
type GraderMethod string

const (
	MethodFileContent     GraderMethod = "file_content"
	MethodJSONSchema      GraderMethod = "json_schema"
	MethodCommandExitCode GraderMethod = "command_exit_code"
	MethodTraceAssertion  GraderMethod = "trace_assertion"
	MethodLLMRubric       GraderMethod = "llm_rubric"
)

// Grader represents a grading configuration
type Grader struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Metadata   GraderMetadata `yaml:"metadata" json:"metadata"`
	Spec       GraderSpec     `yaml:"spec" json:"spec"`
}

// GraderMetadata contains grader metadata
type GraderMetadata struct {
	Name string `yaml:"name" json:"name"`
}

// GraderSpec contains grader specification
type GraderSpec struct {
	Type    GraderType              `yaml:"type" json:"type"`
	Method  GraderMethod            `yaml:"method" json:"method"`
	Config  map[string]interface{}  `yaml:"config" json:"config"`
	Scoring ScoringConfig           `yaml:"scoring" json:"scoring"`
}

// ScoringConfig defines how scores are assigned
type ScoringConfig struct {
	PassedScore float64 `yaml:"passed_score" json:"passed_score"`
	FailedScore float64 `yaml:"failed_score" json:"failed_score"`
}

// GradeResult represents the result of grading a single trial
type GradeResult struct {
	GraderID   string                 `json:"grader_id"`
	GraderType string                 `json:"grader_type"`
	Passed     bool                   `json:"passed"`
	Score      float64                `json:"score"`
	Message    string                 `json:"message"`
	Evidence   map[string]interface{} `json:"evidence"`
	ExecutedAt time.Time              `json:"executed_at"`
}

// GradesManifest represents the complete grading result for a trial
type GradesManifest struct {
	Graders         []GradeResult `json:"graders"`
	AggregatedScore float64       `json:"aggregated_score"`
}
