package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"gopkg.in/yaml.v3"
)

func main() {
	raw, err := os.ReadFile("evals/csv-analysis/grader.yaml")
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	rawSum := sha256.Sum256(raw)
	rawHash := "sha256:" + hex.EncodeToString(rawSum[:])
	fmt.Println("Raw bytes hash   :", rawHash)

	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		fmt.Println("yaml err:", err)
		return
	}
	canonHash, err := identity.HashCanonical(parsed)
	if err != nil {
		fmt.Println("canonical err:", err)
		return
	}
	fmt.Println("Canonical hash   :", canonHash)
	fmt.Println("Equal?           :", rawHash == canonHash)

	if m, ok := parsed.(map[string]any); ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Println("Top-level keys   :", keys)
	}
}
