package manifest

import (
	"testing"
)

type countingHasher struct{ calls int }

func (h *countingHasher) Hash(value any) (string, error) {
	h.calls++
	return "sha256:dependency-test", nil
}

type passthroughPackageIdentity struct{}

func (passthroughPackageIdentity) SkillHash(string) (string, error) { return "sha256:skill-test", nil }
func (passthroughPackageIdentity) LegacySkillHash(string) (string, error) {
	return "sha256:legacy-test", nil
}
func (passthroughPackageIdentity) EnvironmentIdentity(uri, descriptor string) (string, error) {
	return "sha256:environment-test", nil
}

func TestCompilerDependenciesAreExplicit(t *testing.T) {
	var _ ReferenceResolver = fileReferenceResolver{}
	var _ ContentHasher = (*countingHasher)(nil)
	var _ PackageIdentity = passthroughPackageIdentity{}
}
