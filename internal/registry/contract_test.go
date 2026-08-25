package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreListsSkillAndSuiteVersionsAndWritesIndex(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	pkg := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkg, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registered, err := store.RegisterSkill("demo", pkg)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListVersions("skill", "demo")
	if err != nil || len(versions) != 1 || versions[0].Hash != registered.Hash {
		t.Fatalf("versions = %#v err=%v", versions, err)
	}
	if _, err := os.Stat(filepath.Join(root, "index.json")); err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(t.TempDir(), "suite.yaml")
	if err := os.WriteFile(suite, []byte("apiVersion: skillgate.dev/v1alpha1\nkind: EvalSuite\nmetadata:\n  name: demo\nspec:\n  cases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registeredSuite, err := store.RegisterSuite("demo", suite)
	if err != nil {
		t.Fatal(err)
	}
	suites, err := store.ListSuites("demo")
	if err != nil || len(suites) != 1 || suites[0].Hash != registeredSuite.Hash {
		t.Fatalf("suites = %#v err=%v", suites, err)
	}
	if _, err := os.Stat(filepath.Join(root, "suites", "demo", registeredSuite.Hash, "metadata.json")); err != nil {
		t.Fatal(err)
	}
}
