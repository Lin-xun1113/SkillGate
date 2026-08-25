package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Lin-xun1113/SkillGate/internal/experiment"
	"github.com/Lin-xun1113/SkillGate/internal/validation"
	"gopkg.in/yaml.v3"
)

type CompiledExperiment struct {
	ManifestHash string                  `json:"manifest_hash"`
	SuiteHash    string                  `json:"suite_hash"`
	Pairing      map[string]string       `json:"pairing"`
	PairCount    int                     `json:"pair_count"`
	TrialCount   int                     `json:"trial_count"`
	Pairs        []experiment.PairPlan   `json:"pairs"`
	Diagnostics  []validation.Diagnostic `json:"diagnostics"`
}

func Compile(manifestPath, projectRoot string) (*CompiledExperiment, []validation.Diagnostic) {
	return CompileWithDependencies(manifestPath, projectRoot, fileReferenceResolver{}, canonicalContentHasher{}, filePackageIdentity{})
}

func CompileWithDependencies(manifestPath, projectRoot string, resolver ReferenceResolver, hasher ContentHasher, packageIdentity PackageIdentity) (*CompiledExperiment, []validation.Diagnostic) {
	if resolver == nil {
		resolver = fileReferenceResolver{}
	}
	if hasher == nil {
		hasher = canonicalContentHasher{}
	}
	if packageIdentity == nil {
		packageIdentity = filePackageIdentity{}
	}
	manifest, diagnostics := loadYAML(manifestPath)
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	if manifest["apiVersion"] != "skillgate.dev/v1alpha1" || manifest["kind"] != "Experiment" {
		return nil, []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: "", Message: "Manifest apiVersion 或 kind 不正确。"}}
	}
	spec, ok := manifest["spec"].(map[string]any)
	if !ok {
		return nil, []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec", Message: "spec 必须是对象。"}}
	}
	diagnostics = append(diagnostics, validateManifestReferences(spec, manifestPath, projectRoot)...)
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	resolve := func(ref string) (string, string) {
		candidate, code := resolver.Resolve(filepath.Dir(manifestPath), projectRoot, ref)
		if code != "" {
			return "", code
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, ""
		}
		// Frozen M0 manifests use project-root-relative ./evals and ./skills
		// references even though the manifest lives under experiments/.
		fallback, fallbackCode := resolver.Resolve(projectRoot, projectRoot, ref)
		if fallbackCode == "" {
			if _, err := os.Stat(fallback); err == nil {
				return fallback, ""
			}
		}
		return candidate, ""
	}
	readHash := func(ref string) (string, bool) {
		resolved, code := resolve(ref)
		if code != "" {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: code, Severity: "error", Path: ref, Message: "引用路径非法。"})
			return "", false
		}
		value, err := os.ReadFile(resolved)
		if err != nil {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "FILE_NOT_FOUND", Severity: "error", Path: ref, Message: "引用文件不存在。"})
			return "", false
		}
		var parsed any
		if err := yaml.Unmarshal(value, &parsed); err != nil {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: ref, Message: "YAML/JSON 无法解析。"})
			return "", false
		}
		if secret := validation.FindSecretField(parsed, ""); secret != "" {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: ref + "." + secret, Message: "引用内容包含 Secret 字段或明显凭据值。"})
			return "", false
		}
		h, err := hasher.Hash(parsed)
		if err != nil {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "IO_ERROR", Severity: "error", Path: ref, Message: "无法计算内容 Hash。"})
			return "", false
		}
		return h, true
	}
	suiteRef, _ := spec["suite"].(string)
	suiteHash, suiteOK := readHash(suiteRef)
	if !suiteOK {
		return nil, diagnostics
	}
	if declared, _ := spec["suiteHash"].(string); declared != "" && declared != suiteHash {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: "spec.suiteHash", Message: "Suite Hash 与内容不一致。"})
		return nil, diagnostics
	}
	suitePath, _ := resolve(suiteRef)
	suiteResult, suiteDiagnostics := validation.ValidateSuite(suitePath, projectRoot)
	diagnostics = append(diagnostics, suiteDiagnostics...)
	if len(suiteDiagnostics) > 0 {
		return nil, diagnostics
	}
	suite := suiteResult.Document
	if secretPath := validation.FindSecretField(manifest, ""); secretPath != "" {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: secretPath, Message: "Manifest 包含 Secret 字段。"})
		return nil, diagnostics
	}
	pairing, _ := spec["pairing"].(map[string]any)
	treatment, _ := pairing["treatment"].(string)
	if diagnostic := experiment.ValidateTreatment(treatment); diagnostic.Code != "" {
		diagnostics = append(diagnostics, diagnostic)
		return nil, diagnostics
	}
	strategyList, ok := spec["strategies"].([]any)
	if !ok {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "MISSING_REQUIRED_ARM", Severity: "error", Path: "spec.strategies", Message: "strategies 缺失或不是数组。"})
		return nil, diagnostics
	}
	strategies := map[string]map[string]any{}
	for _, raw := range strategyList {
		if item, ok := raw.(map[string]any); ok {
			if name, ok := item["name"].(string); ok {
				if _, exists := strategies[name]; exists {
					diagnostics = append(diagnostics, validation.Diagnostic{Code: "DUPLICATE_CASE_ID", Severity: "error", Path: "spec.strategies", Message: "Strategy 名称重复。"})
				}
				strategies[name] = item
			}
		}
	}
	baseline, hasBaseline := strategies["baseline"]
	candidate, hasCandidate := strategies["candidate"]
	if !hasBaseline || !hasCandidate {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "MISSING_REQUIRED_ARM", Severity: "error", Path: "spec.strategies", Message: "必须包含 baseline 和 candidate Strategy。"})
		return nil, diagnostics
	}
	if !sameNonTreatmentWithHasher(baseline, candidate, hasher) {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.strategies", Message: "Baseline 与 Candidate 的非 Treatment Identity 不一致。"})
		return nil, diagnostics
	}
	if skills, ok := baseline["skills"].([]any); !ok || len(skills) != 0 {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.strategies.baseline.skills", Message: "Baseline 必须显式使用空 Skill 列表。"})
	}
	if skills, ok := candidate["skills"].([]any); !ok || len(skills) != 1 {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.strategies.candidate.skills", Message: "Candidate 必须只加载一个冻结 Skill。"})
	}
	arms, _ := spec["arms"].([]any)
	armNames := map[string]bool{}
	for _, raw := range arms {
		if item, ok := raw.(map[string]any); ok {
			if name, ok := item["name"].(string); ok {
				if armNames[name] {
					diagnostics = append(diagnostics, validation.Diagnostic{Code: "DUPLICATE_ARM", Severity: "error", Path: "spec.arms", Message: "Arm 名称重复。"})
				}
				armNames[name] = true
			}
		}
	}
	if len(armNames) != 2 || !armNames["without_skill"] || !armNames["with_skill"] {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "MISSING_REQUIRED_ARM", Severity: "error", Path: "spec.arms", Message: "M1 只允许 without_skill 和 with_skill 两个 Arm。"})
		return nil, diagnostics
	}
	for _, raw := range arms {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := item["name"].(string)
		strategy, _ := item["strategy"].(string)
		expected := map[string]string{"without_skill": "baseline", "with_skill": "candidate"}
		if strategy != expected[name] {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.arms." + name + ".strategy", Message: "Arm 与 Strategy 映射不一致。"})
		}
	}
	if baselineArm, _ := pairing["baselineArm"].(string); baselineArm != "" && baselineArm != "without_skill" {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.pairing.baselineArm", Message: "Baseline Arm 必须为 without_skill。"})
	}
	if candidateArm, _ := pairing["candidateArm"].(string); candidateArm != "" && candidateArm != "with_skill" {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.pairing.candidateArm", Message: "Candidate Arm 必须为 with_skill。"})
	}
	repetitions := intValue(spec["repetitions"])
	if repetitions < 1 {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.repetitions", Message: "repetitions 必须至少为 1。"})
		return nil, diagnostics
	}
	execution, _ := spec["execution"].(map[string]any)
	harness, _ := execution["harness"].(string)
	harnessVersion, _ := execution["harnessVersion"].(string)
	timeoutSeconds := intValue(execution["timeoutSeconds"])
	maxConcurrent := intValue(execution["maxConcurrent"])
	if timeoutSeconds < 1 || maxConcurrent < 1 {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.execution", Message: "timeoutSeconds 和 maxConcurrent 必须为正整数。"})
		return nil, diagnostics
	}
	environmentRef, _ := execution["environmentDescriptor"].(string)
	environmentURI, _ := execution["environment"].(string)
	environmentHash, _ := execution["environmentHash"].(string)
	descriptorHash, envOK := readHash(environmentRef)
	if !envOK {
		return nil, diagnostics
	}
	if environmentHash != "" && environmentHash != descriptorHash {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: "spec.execution.environmentHash", Message: "Environment Hash 与内容不一致。"})
		return nil, diagnostics
	}
	environmentIdentity, _ := packageIdentity.EnvironmentIdentity(environmentURI, descriptorHash)
	modelHash, _ := hasher.Hash(baseline["model"])
	toolPolicyHash, _ := hasher.Hash(map[string]any{"tools": baseline["tools"], "budget": baseline["budget"], "retry": baseline["retry"], "sandbox": baseline["sandbox"]})
	harnessHash, _ := hasher.Hash(map[string]any{"harness": harness, "version": harnessVersion})
	graderRef := ""
	if grading, ok := spec["grading"].(map[string]any); ok {
		graderRef, _ = grading["ref"].(string)
	}
	graderPath, graderPathCode := resolve(graderRef)
	if graderPathCode != "" {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: graderPathCode, Severity: "error", Path: "spec.grading.ref", Message: "Grader 路径非法。"})
		return nil, diagnostics
	}
	graderHash, graderOK := readHash(graderRef)
	if !graderOK || len(diagnostics) > 0 {
		return nil, diagnostics
	}
	diagnostics = append(diagnostics, validateGraderReferences(graderPath, projectRoot)...)
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	if graderPath, code := resolve(graderRef); code == "" {
		if raw, err := os.ReadFile(graderPath); err == nil {
			var graderDoc any
			if yaml.Unmarshal(raw, &graderDoc) == nil {
				if secret := validation.FindSecretField(graderDoc, ""); secret != "" {
					diagnostics = append(diagnostics, validation.Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: "spec.grading." + secret, Message: "Grader 包含 Secret 字段。"})
				}
			}
		}
	}
	if graderPath, code := resolve(graderRef); code == "" {
		var graderDoc map[string]any
		if raw, err := os.ReadFile(graderPath); err == nil && yaml.Unmarshal(raw, &graderDoc) == nil {
			if graderDoc["kind"] != "Grader" && graderDoc["kind"] != "EvalGrader" && graderDoc["kind"] != "DeterministicGraderSet" {
				diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.grading.ref", Message: "Grader kind 不正确。"})
			}
		}
	}
	if grading, ok := spec["grading"].(map[string]any); ok {
		if llm, exists := grading["llm"]; !exists {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.grading.llm", Message: "M1 必须显式配置 llm.enabled=false。"})
		} else if llmMap, ok := llm.(map[string]any); !ok {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.grading.llm", Message: "llm 配置格式非法。"})
		} else if enabled, ok := llmMap["enabled"].(bool); !ok || enabled {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.grading.llm.enabled", Message: "M1 必须显式关闭 LLM Judge。"})
		}
		if declared, _ := grading["hash"].(string); declared != "" && declared != graderHash {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: "spec.grading.hash", Message: "Grader Hash 与内容不一致。"})
		}
		if graderPath, code := resolve(graderRef); code == "" {
			var graderDoc map[string]any
			if raw, err := os.ReadFile(graderPath); err == nil && yaml.Unmarshal(raw, &graderDoc) == nil {
				graderSpec, _ := graderDoc["spec"].(map[string]any)
				if mode, _ := graderSpec["mode"].(string); mode != "deterministic_only" {
					diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.grading.ref", Message: "Grader 必须为 deterministic_only。"})
				}
				if llmJudge, exists := graderSpec["llmJudge"]; exists {
					if value, ok := llmJudge.(bool); !ok || value {
						diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.grading.ref", Message: "Grader llmJudge 必须为 false。"})
					}
				}
			}
		}
	}
	policyRef, _ := spec["policy"].(string)
	policyHash, policyOK := readHash(policyRef)
	if !policyOK {
		return nil, diagnostics
	}
	if policyPath, code := resolve(policyRef); code == "" {
		raw, err := os.ReadFile(policyPath)
		if err == nil {
			var policyDoc any
			if yaml.Unmarshal(raw, &policyDoc) == nil {
				if secret := validation.FindSecretField(policyDoc, ""); secret != "" {
					diagnostics = append(diagnostics, validation.Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: "spec.policy." + secret, Message: "Policy 包含 Secret 字段。"})
				}
			}
		}
	}
	if declared, _ := spec["policyHash"].(string); declared != "" && declared != policyHash {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: "spec.policyHash", Message: "Policy Hash 与内容不一致。"})
	}
	skillVersions := make([]map[string]any, 0)
	skillList, skillListOK := spec["skills"].([]any)
	if !skillListOK || len(skillList) != 1 {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.skills", Message: "M1 必须声明且只允许一个顶层 Skill Source。"})
	}
	if skillListOK {
		for i, raw := range skillList {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ref, _ := item["path"].(string)
			skillPath, code := resolve(ref)
			if code != "" {
				diagnostics = append(diagnostics, validation.Diagnostic{Code: code, Severity: "error", Path: fmt.Sprintf("spec.skills[%d].path", i), Message: "Skill 路径非法。"})
				continue
			}
			hash, err := packageIdentity.SkillHash(skillPath)
			if err != nil {
				diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_SKILL_MANIFEST", Severity: "error", Path: fmt.Sprintf("spec.skills[%d]", i), Message: err.Error()})
				continue
			}
			if declared, _ := item["version"].(string); declared != "" && declared != hash {
				legacy, legacyErr := packageIdentity.LegacySkillHash(skillPath)
				if legacyErr != nil || declared != legacy {
					diagnostics = append(diagnostics, validation.Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: fmt.Sprintf("spec.skills[%d].version", i), Message: "Skill Hash 与内容不一致。"})
				} else {
					hash = declared
				}
			}
			name, _ := item["name"].(string)
			skillVersions = append(skillVersions, map[string]any{"name": name, "skill_version": hash})
		}
	}
	if skills, ok := candidate["skills"].([]any); ok && len(skills) == 1 && len(skillVersions) == 1 {
		item, _ := skills[0].(map[string]any)
		expectedName, _ := skillVersions[0]["name"].(string)
		expectedVersion, _ := skillVersions[0]["skill_version"].(string)
		actualName, _ := item["name"].(string)
		actualVersion, _ := item["version"].(string)
		if actualName != expectedName || actualVersion != expectedVersion {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.strategies.candidate.skills[0]", Message: "Candidate Skill 必须与顶层冻结 Skill 完全一致。"})
		}
	}
	if len(skillVersions) != 1 {
		diagnostics = append(diagnostics, validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.skills", Message: "顶层 Skill 必须解析为一个冻结版本。"})
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	manifestProjection := map[string]any{
		"apiVersion": manifest["apiVersion"], "kind": manifest["kind"], "metadata": map[string]any{"name": metadataName(manifest["metadata"])},
		"suite_hash": suiteHash, "skills": skillVersions, "strategies": map[string]any{"baseline": strategyProjection(baseline, nil), "candidate": strategyProjection(candidate, skillVersions)}, "arms": normalizedArms(arms), "pairing": map[string]any{"treatment": treatment, "baseline_arm": "without_skill", "candidate_arm": "with_skill"},
		"repetitions": repetitions, "execution": map[string]any{"harness": harness, "harnessVersion": harnessVersion, "environment_hash": environmentIdentity, "timeoutSeconds": timeoutSeconds, "maxConcurrent": maxConcurrent},
		"grader_hash": graderHash, "policy_hash": policyHash,
	}
	manifestHash, _ := hasher.Hash(manifestProjection)
	if declared, _ := spec["manifestHash"].(string); declared != "" && declared != manifestHash {
		legacyInput := cloneMap(manifest)
		if legacySpec, ok := legacyInput["spec"].(map[string]any); ok {
			delete(legacySpec, "manifestHash")
		}
		legacyManifestHash, _ := hasher.Hash(legacyInput)
		if declared != legacyManifestHash {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: "spec.manifestHash", Message: "Manifest Hash 与规范化内容不一致。"})
			return nil, diagnostics
		}
	}
	caseList, _ := suite["spec"].(map[string]any)
	cases, _ := caseList["cases"].([]any)
	sort.Slice(cases, func(i, j int) bool { return caseID(cases[i]) < caseID(cases[j]) })
	plan := &CompiledExperiment{ManifestHash: manifestHash, SuiteHash: suiteHash, Pairing: map[string]string{"treatment": "skill_version", "baseline_arm": "without_skill", "candidate_arm": "with_skill"}, Diagnostics: []validation.Diagnostic{}, Pairs: make([]experiment.PairPlan, 0)}
	for _, raw := range cases {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["id"].(string)
		mode, _ := item["evaluationMode"].(string)
		population, _ := item["population"].(string)
		if !validModePopulation(mode, population) {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_EVALUATION_MODE", Severity: "error", Path: "spec.suite.cases." + id, Message: "evaluationMode 与 population 组合非法。"})
			continue
		}
		fixtureHash, _ := hasher.Hash(item["fixtures"])
		for repetition := 1; repetition <= repetitions; repetition++ {
			pair, err := experiment.CompilePairPlan(experiment.CompileInput{ManifestHash: manifestHash, SuiteHash: suiteHash, CaseID: id, EvaluationMode: mode, Repetition: repetition, ModelHash: modelHash, HarnessHash: harnessHash, EnvironmentHash: environmentIdentity, FixtureHash: fixtureHash, GraderHash: graderHash, ToolPolicyHash: toolPolicyHash})
			if err != nil {
				diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_EVALUATION_MODE", Severity: "error", Path: "spec.suite.cases." + id, Message: err.Error()})
				continue
			}
			plan.Pairs = append(plan.Pairs, pair)
		}
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	plan.PairCount = len(plan.Pairs)
	plan.TrialCount = plan.PairCount * 2
	return plan, nil
}

func strategyProjection(strategy map[string]any, skills []map[string]any) map[string]any {
	return map[string]any{"model": strategy["model"], "skills": skillsOrEmpty(skills), "tools": strategy["tools"], "budget": strategy["budget"], "retry": strategy["retry"], "sandbox": strategy["sandbox"]}
}
func skillsOrEmpty(skills []map[string]any) []map[string]any {
	if skills == nil {
		return []map[string]any{}
	}
	return skills
}
func normalizedArms(arms []any) []map[string]string {
	out := make([]map[string]string, 0, len(arms))
	for _, raw := range arms {
		if item, ok := raw.(map[string]any); ok {
			name, _ := item["name"].(string)
			strategy, _ := item["strategy"].(string)
			out = append(out, map[string]string{"name": name, "strategy": strategy})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	return out
}

func metadataName(value any) string {
	if metadata, ok := value.(map[string]any); ok {
		name, _ := metadata["name"].(string)
		return name
	}
	return ""
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func loadYAML(path string) (map[string]any, []validation.Diagnostic) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, []validation.Diagnostic{{Code: "FILE_NOT_FOUND", Severity: "error", Path: path, Message: "文件不存在。"}}
	}
	var value map[string]any
	if err := yaml.Unmarshal(b, &value); err != nil {
		return nil, []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: path, Message: "YAML 无法解析。"}}
	}
	return value, nil
}
func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
func caseID(value any) string {
	if m, ok := value.(map[string]any); ok {
		s, _ := m["id"].(string)
		return s
	}
	return ""
}
func validModePopulation(mode, pop string) bool {
	return (mode == "forced_injection" && pop == "answer") || (mode == "autonomous_trigger" && pop == "trigger") || (mode == "security_probe" && pop == "answer")
}
func sameNonTreatment(a, b map[string]any) bool {
	return sameNonTreatmentWithHasher(a, b, canonicalContentHasher{})
}

func sameNonTreatmentWithHasher(a, b map[string]any, hasher ContentHasher) bool {
	keys := []string{"model", "tools", "budget", "retry", "sandbox"}
	for _, k := range keys {
		ah, _ := hasher.Hash(a[k])
		bh, _ := hasher.Hash(b[k])
		if ah != bh {
			return false
		}
	}
	return true
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
