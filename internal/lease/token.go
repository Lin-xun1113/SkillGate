package lease

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
)

const tokenBytes = 32

var ErrInvalidToken = errors.New("lease token 不能为空")

type Token struct {
	Plaintext string
	Hash      []byte
}

func Generate() (Token, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, err
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	return Token{Plaintext: plaintext, Hash: Hash(plaintext)}, nil
}

func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func Matches(storedHash []byte, token string) bool {
	if len(storedHash) != sha256.Size || token == "" {
		return false
	}
	actual := Hash(token)
	return subtle.ConstantTimeCompare(storedHash, actual) == 1
}

func Validate(token string) error {
	if token == "" {
		return ErrInvalidToken
	}
	return nil
}
