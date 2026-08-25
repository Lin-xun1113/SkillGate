package manifest

import (
	"os"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/validation"
)

// ReferenceResolver is the only path-bearing dependency used by the compiler.
// Implementations must enforce project-root and symlink boundaries.
type ReferenceResolver interface {
	Resolve(base, projectRoot, ref string) (string, string)
	Read(path string) ([]byte, error)
	Exists(path string) bool
}

// ContentHasher is the canonical identity dependency used by the compiler.
type ContentHasher interface {
	Hash(value any) (string, error)
}

type PackageIdentity interface {
	SkillHash(root string) (string, error)
	LegacySkillHash(root string) (string, error)
	EnvironmentIdentity(uri, descriptorHash string) (string, error)
}

type fileReferenceResolver struct{}

func (fileReferenceResolver) Resolve(base, projectRoot, ref string) (string, string) {
	return validation.ResolveReference(base, projectRoot, ref)
}
func (fileReferenceResolver) Read(path string) ([]byte, error) { return os.ReadFile(path) }
func (fileReferenceResolver) Exists(path string) bool          { _, err := os.Stat(path); return err == nil }

type canonicalContentHasher struct{}

func (canonicalContentHasher) Hash(value any) (string, error) { return identity.HashCanonical(value) }

type filePackageIdentity struct{}

func (filePackageIdentity) SkillHash(root string) (string, error) {
	return identity.SkillPackageHash(root)
}
func (filePackageIdentity) LegacySkillHash(root string) (string, error) {
	return identity.SkillPackageLegacyHash(root)
}
func (filePackageIdentity) EnvironmentIdentity(uri, descriptorHash string) (string, error) {
	return identity.EnvironmentIdentity(uri, descriptorHash)
}

// These interfaces document the future Registry-backed resolution boundary.
// M1's CLI adapter still resolves local references before a PostgreSQL adapter
// is introduced in M2.
type SkillResolver interface {
	ResolveSkill(name, version string) ([]byte, error)
}

type SuiteResolver interface {
	ResolveSuite(name, version string) ([]byte, error)
}
