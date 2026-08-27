package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"gopkg.in/yaml.v3"
)

func main() {
	raw, _ := os.ReadFile("evals/csv-analysis/grader.yaml")
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		fmt.Println("yaml err:", err)
		return
	}
	hash, _ := identity.HashCanonical(parsed)
	rawSum := sha256.Sum256(raw)
	rawHash := "sha256:" + hex.EncodeToString(rawSum[:])
	fmt.Println("Raw bytes SHA-256   :", rawHash)
	fmt.Println("Canonical JSON SHA-256:", hash)
	fmt.Println("Declared in manifest : sha256:b5f94acfd374f9c92123e4e25006aacd4bf703aefab72dc23a9f00b98f779892")
	fmt.Println("Match canonical to declared:", hash == "sha256:b5f94acfd374f9c92123e4e25006aacd4bf703aefab72dc23a9f00b98f779892")
	fmt.Println("Match raw bytes to declared:", rawHash == "sha256:b5f94acfd374f9c92123e4e25006aacd4bf703aefab72dc23a9f00b98f779892")
	fmt.Println()
	fmt.Println("grader/registry.go::calculateHash uses raw bytes (NOT canonical)")
	fmt.Println("manifest.go::readHash(graderRef) uses canonical JSON")
	fmt.Println("=> graderHash in experiments.grader_hash != registry key")
	fmt.Println("=> M5 grading can NEVER find the grader")
}
