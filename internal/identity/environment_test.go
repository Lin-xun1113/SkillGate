package identity

import "testing"

func TestEnvironmentIdentityIncludesURIAndDescriptorHash(t *testing.T) {
	first, err := EnvironmentIdentity("docker://case:v1", "sha256:descriptor")
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnvironmentIdentity("docker://case:v2", "sha256:descriptor")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("environment URI change did not change identity: %s", first)
	}
}
