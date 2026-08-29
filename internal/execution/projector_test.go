package execution

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
)

func TestProjectM0ManifestBothArms(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestPath := filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml")
	compiled, diagnostics := manifest.Compile(manifestPath, root)
	if len(diagnostics) != 0 {
		t.Fatalf("compile diagnostics = %#v", diagnostics)
	}
	if compiled == nil || len(compiled.Pairs) == 0 {
		t.Fatal("manifest compiled without pairs")
	}

	ctx := context.Background()
	serverProjector := NewProjector(manifest.NewCompiler(root, t.TempDir()))
	legacyProjector := runner.NewProjector(root)
	pairID := compiled.Pairs[0].PairID

	tests := []struct {
		arm             string
		expectSkillHash string
	}{
		{arm: "without_skill", expectSkillHash: ""},
		{arm: "with_skill", expectSkillHash: "sha256:5a2153eaf0a11af8d6141b87755526503e02a3af99080464cae27f49e5164090"},
	}
	for _, tc := range tests {
		t.Run(tc.arm, func(t *testing.T) {
			serverSpec, serverHash, err := serverProjector.Project(ctx, compiled.ManifestHash, pairID, tc.arm)
			if err != nil {
				t.Fatalf("execution projector: %v", err)
			}
			legacySpec, legacyHash, err := legacyProjector.Project(ctx, compiled.ManifestHash, pairID, tc.arm)
			if err != nil {
				t.Fatalf("runner projector: %v", err)
			}

			if serverSpec.CaseID != "csv-contextual-001" {
				t.Fatalf("case_id = %q, want csv-contextual-001", serverSpec.CaseID)
			}
			if serverSpec.SkillHash != tc.expectSkillHash {
				t.Fatalf("skill_hash = %q, want %q", serverSpec.SkillHash, tc.expectSkillHash)
			}
			if prompt, ok := serverSpec.CaseInput["prompt"].(string); !ok || prompt == "" {
				t.Fatalf("case_input.prompt missing: %#v", serverSpec.CaseInput)
			}
			fixtures, ok := serverSpec.CaseInput["fixtures"].([]any)
			if !ok || len(fixtures) != 1 {
				t.Fatalf("case_input.fixtures = %#v, want one fixture", serverSpec.CaseInput["fixtures"])
			}
			if provider, _ := serverSpec.Model["provider"].(string); provider != "fixture" {
				t.Fatalf("model.provider = %q, want fixture", provider)
			}
			if network, _ := serverSpec.Environment["network"].(string); network != "disabled" {
				t.Fatalf("environment.network = %q, want disabled", network)
			}
			if !strings.HasPrefix(serverHash, "sha256:") || serverHash == "sha256:" {
				t.Fatalf("invalid execution hash %q", serverHash)
			}
			if serverHash != legacyHash {
				t.Fatalf("projector hash mismatch: execution=%s runner=%s", serverHash, legacyHash)
			}
			if _, err := runner.ComputeExecutionHashForSpec(serverSpec); err != nil {
				t.Fatalf("recompute execution hash: %v", err)
			} else if recomputed, _ := runner.ComputeExecutionHashForSpec(serverSpec); recomputed != serverHash {
				t.Fatalf("execution hash not reproducible: got %s, recomputed %s", serverHash, recomputed)
			}
			if !reflect.DeepEqual(serverSpec, legacySpec) {
				t.Fatalf("projector execution specs differ:\nserver=%#v\nrunner=%#v", serverSpec, legacySpec)
			}
		})
	}
}

func TestProjectRejectsUnknownArm(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestPath := filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml")
	compiled, diagnostics := manifest.Compile(manifestPath, root)
	if len(diagnostics) != 0 {
		t.Fatalf("compile diagnostics = %#v", diagnostics)
	}
	projector := NewProjector(manifest.NewCompiler(root, t.TempDir()))
	if _, _, err := projector.Project(context.Background(), compiled.ManifestHash, compiled.Pairs[0].PairID, "unknown"); err == nil {
		t.Fatal("expected unknown arm to be rejected")
	}
}

func TestProjectM0ManifestProjectsEveryCaseBothArms(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestPath := filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml")
	compiled, diagnostics := manifest.Compile(manifestPath, root)
	if len(diagnostics) != 0 {
		t.Fatalf("compile diagnostics = %#v", diagnostics)
	}
	serverProjector := NewProjector(manifest.NewCompiler(root, t.TempDir()))
	legacyProjector := runner.NewProjector(root)
	seenCases := map[string]bool{}
	for _, pair := range compiled.Pairs {
		if seenCases[pair.CaseID] {
			continue
		}
		seenCases[pair.CaseID] = true
		for _, arm := range []string{"without_skill", "with_skill"} {
			t.Run(pair.CaseID+"/"+arm, func(t *testing.T) {
				serverSpec, serverHash, err := serverProjector.Project(context.Background(), compiled.ManifestHash, pair.PairID, arm)
				if err != nil {
					t.Fatalf("execution projector: %v", err)
				}
				legacySpec, legacyHash, err := legacyProjector.Project(context.Background(), compiled.ManifestHash, pair.PairID, arm)
				if err != nil {
					t.Fatalf("runner projector: %v", err)
				}
				if serverSpec.CaseID != pair.CaseID {
					t.Fatalf("case_id = %q, want %q", serverSpec.CaseID, pair.CaseID)
				}
				if id, _ := serverSpec.CaseInput["id"].(string); id != pair.CaseID {
					t.Fatalf("case_input.id = %q, want %q", id, pair.CaseID)
				}
				if serverHash != legacyHash {
					t.Fatalf("projector hash mismatch: execution=%s runner=%s", serverHash, legacyHash)
				}
				if !reflect.DeepEqual(serverSpec, legacySpec) {
					t.Fatalf("projector execution specs differ for %s/%s", pair.CaseID, arm)
				}
			})
		}
	}
	if len(seenCases) != 8 {
		t.Fatalf("projected %d cases, want 8", len(seenCases))
	}
}
