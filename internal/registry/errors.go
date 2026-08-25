package registry

import (
	"errors"
)

type CodeError struct {
	Code string
	Err  error
}

func (e *CodeError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}
func (e *CodeError) Unwrap() error { return e.Err }

func WithCode(code string, err error) error { return &CodeError{Code: code, Err: err} }

func DiagnosticCode(err error) string {
	var coded *CodeError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}
