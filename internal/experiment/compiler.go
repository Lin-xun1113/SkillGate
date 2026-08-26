package experiment

import (
	"fmt"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/validation"
)

type CompileInput struct {
	ManifestHash    string
	SuiteHash       string
	CaseID          string
	EvaluationMode  string
	Population      string
	Polarity        string
	Repetition      int
	ModelHash       string
	HarnessHash     string
	EnvironmentHash string
	FixtureHash     string
	GraderHash      string
	ToolPolicyHash  string
}

type Trial struct {
	TrialID string `json:"trial_id"`
	Arm     string `json:"arm"`
	Attempt int    `json:"attempt"`
}

type PairPlan struct {
	PairID         string             `json:"pair_id"`
	CaseID         string             `json:"case_id"`
	EvaluationMode string             `json:"evaluation_mode"`
	Population     string             `json:"population"`
	Polarity       string             `json:"polarity"`
	Repetition     int                `json:"repetition"`
	Identity       identity.PairInput `json:"identity"`
	Trials         []Trial            `json:"trials"`
}

func ValidateTreatment(treatment string) validation.Diagnostic {
	if treatment != "skill_version" {
		return validation.Diagnostic{Code: "UNSUPPORTED_TREATMENT", Severity: "error", Path: "spec.pairing.treatment", Message: "M1 仅支持 skill_version Treatment。"}
	}
	return validation.Diagnostic{}
}

func CompilePairPlan(input CompileInput) (PairPlan, error) {
	if input.EvaluationMode != "forced_injection" && input.EvaluationMode != "autonomous_trigger" && input.EvaluationMode != "security_probe" {
		return PairPlan{}, fmt.Errorf("invalid evaluation mode")
	}
	pairInput := identity.PairInput{
		ExperimentHash: input.ManifestHash, SuiteHash: input.SuiteHash, CaseID: input.CaseID,
		EvaluationMode: input.EvaluationMode, Repetition: input.Repetition, Treatment: "skill_version",
		BaselineArm: "without_skill", CandidateArm: "with_skill", ModelHash: input.ModelHash,
		HarnessHash: input.HarnessHash, EnvironmentHash: input.EnvironmentHash, FixtureHash: input.FixtureHash,
		GraderHash: input.GraderHash, ToolPolicyHash: input.ToolPolicyHash,
	}
	pairID, err := identity.PairID(pairInput)
	if err != nil {
		return PairPlan{}, err
	}
	without, err := identity.TrialID(pairID, "without_skill", 1)
	if err != nil {
		return PairPlan{}, err
	}
	with, err := identity.TrialID(pairID, "with_skill", 1)
	if err != nil {
		return PairPlan{}, err
	}
	return PairPlan{PairID: pairID, CaseID: input.CaseID, EvaluationMode: input.EvaluationMode, Population: input.Population, Polarity: input.Polarity, Repetition: input.Repetition, Identity: pairInput, Trials: []Trial{{TrialID: without, Arm: "without_skill", Attempt: 1}, {TrialID: with, Arm: "with_skill", Attempt: 1}}}, nil
}
