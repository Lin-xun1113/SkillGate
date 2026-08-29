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
	MethodFileExists      GraderMethod = "file_exists"
	MethodFileContent     GraderMethod = "file_content"
	MethodFileHash        GraderMethod = "file_hash"
	MethodJSONSchema      GraderMethod = "json_schema"
	MethodJSONField       GraderMethod = "json_field"
	MethodRegex           GraderMethod = "regex"
	MethodCommandExitCode GraderMethod = "command_exit_code"
	MethodTraceAssertion  GraderMethod = "trace_assertion"
	MethodSuiteAssertions GraderMethod = "suite_assertions"
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
	Name    string `yaml:"name" json:"name"`
	Version int    `yaml:"version,omitempty" json:"version,omitempty"`
}

// GraderSpec contains grader specification
type GraderSpec struct {
	Type               GraderType             `yaml:"type" json:"type"`
	Method             GraderMethod           `yaml:"method" json:"method"`
	Config             map[string]interface{} `yaml:"config" json:"config"`
	Scoring            ScoringConfig          `yaml:"scoring" json:"scoring"`
	Mode               string                 `yaml:"mode,omitempty" json:"mode,omitempty"`
	LLMJudge           bool                   `yaml:"llmJudge,omitempty" json:"llmJudge,omitempty"`
	SuiteRef           string                 `yaml:"suiteRef,omitempty" json:"suiteRef,omitempty"`
	AssertionSemantics string                 `yaml:"assertionSemantics,omitempty" json:"assertionSemantics,omitempty"`
	SchemaRefs         []string               `yaml:"schemaRefs,omitempty" json:"schemaRefs,omitempty"`
	ExpectedRefs       []string               `yaml:"expectedRefs,omitempty" json:"expectedRefs,omitempty"`
	Boundary           map[string]any         `yaml:"boundary,omitempty" json:"boundary,omitempty"`
}

// ScoringConfig defines how scores are assigned
type ScoringConfig struct {
	PassedScore float64 `yaml:"passed_score" json:"passed_score"`
	FailedScore float64 `yaml:"failed_score" json:"failed_score"`
}

// GradeResult represents the result of grading a single trial
type GradeResult struct {
	GraderID      string                 `json:"grader_id"`
	GraderHash    string                 `json:"grader_hash,omitempty"`
	GraderVersion int                    `json:"grader_version,omitempty"`
	GraderType    string                 `json:"grader_type"`
	Status        string                 `json:"status"`
	Passed        bool                   `json:"passed"`
	Score         float64                `json:"score"`
	Message       string                 `json:"message"`
	Evidence      map[string]interface{} `json:"evidence"`
	InputHash     string                 `json:"input_hash,omitempty"`
	EvidenceHash  string                 `json:"evidence_hash,omitempty"`
	ExecutedAt    time.Time              `json:"executed_at"`
}

// GradesManifest represents the complete grading result for a trial
type GradesManifest struct {
	Graders         []GradeResult `json:"graders"`
	AggregatedScore float64       `json:"aggregated_score"`
	Status          string        `json:"status,omitempty"`
	GraderHash      string        `json:"grader_hash,omitempty"`
	GraderVersion   int           `json:"grader_version,omitempty"`
	InputHash       string        `json:"input_hash,omitempty"`
	EvidenceHash    string        `json:"evidence_hash,omitempty"`
}
