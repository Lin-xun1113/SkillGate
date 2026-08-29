// Package secrets defines the small operator-owned secret boundary shared by
// control-plane configuration and tests.  Callers receive a value in memory;
// the value is never included in an error, diagnostic, identity, or log.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Code is a stable reason for a secret lookup failure.
type Code string

const (
	CodeMissing     Code = "SECRET_MISSING"
	CodeEmpty       Code = "SECRET_EMPTY"
	CodeUnreadable  Code = "SECRET_UNREADABLE"
	CodeInvalidName Code = "SECRET_INVALID_NAME"
)

// Error deliberately omits the secret value and file contents.  Name is an
// operator-provided environment variable name and is validated before use.
type Error struct {
	Code  Code
	Name  string
	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return "secret lookup failed"
	}
	if e.Name == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: secret %s is unavailable", e.Code, e.Name)
}

func (e *Error) Unwrap() error { return e.Cause }

// IsCode reports whether err (possibly wrapped) has the requested reason.
func IsCode(err error, code Code) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Lookup resolves an operator secret.  If NAME_FILE is configured, the file
// is authoritative; NAME is used only when NAME_FILE is absent.  This is the
// convention used by Docker Official Images and keeps values out of Compose
// command/argument fields.
func Lookup(name string) (string, error) {
	return LookupWith(name, os.LookupEnv, os.ReadFile)
}

// LookupWith is injectable for deterministic unit tests and alternate hosts.
func LookupWith(name string, lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error)) (string, error) {
	if !envNamePattern.MatchString(name) {
		return "", &Error{Code: CodeInvalidName, Name: name}
	}
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if readFile == nil {
		readFile = os.ReadFile
	}

	fileName := name + "_FILE"
	if path, configured := lookupEnv(fileName); configured {
		path = strings.TrimSpace(path)
		if path == "" {
			return "", &Error{Code: CodeEmpty, Name: fileName}
		}
		contents, err := readFile(path)
		if err != nil {
			return "", &Error{Code: CodeUnreadable, Name: name, Cause: err}
		}
		value := strings.TrimSpace(string(contents))
		if value == "" {
			return "", &Error{Code: CodeEmpty, Name: name}
		}
		return value, nil
	}

	value, configured := lookupEnv(name)
	if !configured {
		return "", &Error{Code: CodeMissing, Name: name}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &Error{Code: CodeEmpty, Name: name}
	}
	return value, nil
}

// Configured reports whether either the direct or file-backed source exists.
// It lets callers try a compatibility alias without masking a configured but
// broken primary source.
func Configured(name string) bool {
	if !envNamePattern.MatchString(name) {
		return false
	}
	_, file := os.LookupEnv(name + "_FILE")
	_, direct := os.LookupEnv(name)
	return file || direct
}
