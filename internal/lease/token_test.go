package lease

import (
	"bytes"
	"testing"
)

func TestGenerateProducesUnrepeatedHashedToken(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if first.Plaintext == second.Plaintext || bytes.Equal(first.Hash, second.Hash) {
		t.Fatal("lease token 必须不可复用")
	}
	if !Matches(first.Hash, first.Plaintext) || Matches(first.Hash, second.Plaintext) {
		t.Fatal("lease token hash 校验结果不正确")
	}
	if len(first.Plaintext) < 40 {
		t.Fatalf("lease token 熵不足: %d", len(first.Plaintext))
	}
}

func TestEmptyTokenIsInvalid(t *testing.T) {
	if Validate("") == nil || Matches(make([]byte, 32), "") {
		t.Fatal("空 token 必须被拒绝")
	}
}
