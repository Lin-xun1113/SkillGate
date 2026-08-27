package runner

import (
	"os"
	"path/filepath"
	"testing"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
)

func TestIsWithinArtifactsRoot(t *testing.T) {
	tmpDir := t.TempDir()
	artifactsRoot := filepath.Join(tmpDir, "artifacts")
	if err := os.MkdirAll(artifactsRoot, 0755); err != nil {
		t.Fatalf("Failed to create artifacts root: %v", err)
	}

	tests := []struct {
		name        string
		targetPath  string
		expectOK    bool
		expectError bool
		setup       func() string
	}{
		{
			name:       "valid relative path",
			targetPath: "exp_01/trial_01/output.txt",
			expectOK:   true,
		},
		{
			name:        "path traversal with ../",
			targetPath:  "artifacts/../../../etc/passwd",
			expectOK:    false,
			expectError: true,
		},
		{
			name:        "absolute path outside root",
			targetPath:  "/etc/passwd",
			expectOK:    false,
			expectError: true,
		},
		{
			name:       "valid absolute path inside root",
			targetPath: filepath.Join(artifactsRoot, "valid.txt"),
			expectOK:   true,
		},
		{
			name:        "empty path",
			targetPath:  "",
			expectOK:    false,
			expectError: true,
		},
		{
			name:       "path with . components",
			targetPath: "./subdir/./file.txt",
			expectOK:   true,
		},
		{
			name:        "symlink pointing outside root",
			expectOK:    false,
			expectError: true,
			setup: func() string {
				outsideFile := filepath.Join(tmpDir, "outside.txt")
				if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
					t.Fatalf("Failed to create outside file: %v", err)
				}
				symlinkPath := filepath.Join(artifactsRoot, "bad_link")
				if err := os.Symlink(outsideFile, symlinkPath); err != nil {
					t.Fatalf("Failed to create symlink: %v", err)
				}
				return "bad_link"
			},
		},
		{
			name:     "symlink pointing inside root",
			expectOK: true,
			setup: func() string {
				insideFile := filepath.Join(artifactsRoot, "inside.txt")
				if err := os.WriteFile(insideFile, []byte("data"), 0644); err != nil {
					t.Fatalf("Failed to create inside file: %v", err)
				}
				symlinkPath := filepath.Join(artifactsRoot, "good_link")
				if err := os.Symlink(insideFile, symlinkPath); err != nil {
					t.Fatalf("Failed to create symlink: %v", err)
				}
				return "good_link"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetPath := tt.targetPath
			if tt.setup != nil {
				targetPath = tt.setup()
			}

			ok, err := isWithinArtifactsRoot(targetPath, artifactsRoot)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if ok != tt.expectOK {
				t.Errorf("Expected ok=%v, got %v (error: %v)", tt.expectOK, ok, err)
			}
		})
	}
}

func TestValidateArtifactManifests_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	artifactsRoot := filepath.Join(tmpDir, "artifacts")
	if err := os.MkdirAll(artifactsRoot, 0755); err != nil {
		t.Fatalf("Failed to create artifacts root: %v", err)
	}

	// Create a valid artifact file
	validPath := filepath.Join(artifactsRoot, "valid.txt")
	validContent := []byte("test content")
	if err := os.WriteFile(validPath, validContent, 0644); err != nil {
		t.Fatalf("Failed to create valid artifact: %v", err)
	}

	// Create a file outside artifacts root to simulate attack target
	outsidePath := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret data"), 0644); err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}

	svc := &RunnerService{
		opts: ServiceOptions{
			ArtifactsRoot: artifactsRoot,
		},
	}

	tests := []struct {
		name      string
		artifacts []*runnerv1.ArtifactManifest
		wantError bool
	}{
		{
			name: "valid artifact within root",
			artifacts: []*runnerv1.ArtifactManifest{
				{
					ArtifactId: "valid",
					LocalPath:  validPath,
					Sha256:     "sha256:6ae8a75555209fd6c44157c0aed8016e763ff435a19cf186f76863140143ff72",
					SizeBytes:  int64(len(validContent)),
				},
			},
			wantError: false,
		},
		{
			name: "path traversal attempt with ../",
			artifacts: []*runnerv1.ArtifactManifest{
				{
					ArtifactId: "traversal",
					LocalPath:  filepath.Join(artifactsRoot, "../secret.txt"),
					Sha256:     "sha256:abc123",
					SizeBytes:  11,
				},
			},
			wantError: true,
		},
		{
			name: "absolute path outside root",
			artifacts: []*runnerv1.ArtifactManifest{
				{
					ArtifactId: "absolute",
					LocalPath:  "/etc/passwd",
					Sha256:     "sha256:abc123",
					SizeBytes:  100,
				},
			},
			wantError: true,
		},
		{
			name: "direct reference to outside file",
			artifacts: []*runnerv1.ArtifactManifest{
				{
					ArtifactId: "outside",
					LocalPath:  outsidePath,
					Sha256:     "sha256:abc123",
					SizeBytes:  11,
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.validateArtifactManifests(tt.artifacts)
			if (err != nil) != tt.wantError {
				t.Errorf("validateArtifactManifests() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateArtifactManifests_NoRootConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("test")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// When ArtifactsRoot is not configured, validation should skip path checks
	svc := &RunnerService{
		opts: ServiceOptions{
			ArtifactsRoot: "", // Not configured
		},
	}

	artifacts := []*runnerv1.ArtifactManifest{
		{
			ArtifactId: "test",
			LocalPath:  testFile,
			Sha256:     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			SizeBytes:  int64(len(testContent)),
		},
	}

	// Should not error even though path validation would be skipped
	err := svc.validateArtifactManifests(artifacts)
	if err != nil {
		t.Errorf("Expected no error when ArtifactsRoot not configured, got: %v", err)
	}
}
