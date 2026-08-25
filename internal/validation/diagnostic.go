package validation

import "encoding/json"

type Diagnostic struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Path     string         `json:"path,omitempty"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
}

func (d Diagnostic) ExitCode() int {
	switch d.Code {
	case "INVALID_ARGUMENT":
		return 2
	case "FILE_NOT_FOUND":
		return 3
	case "PATH_OUTSIDE_ROOT", "SECRET_FIELD_PRESENT":
		return 4
	case "INVALID_SKILL_MANIFEST", "DUPLICATE_CASE_ID", "DUPLICATE_ARM", "MISSING_REQUIRED_ARM", "INVALID_EVALUATION_MODE", "UNSUPPORTED_TREATMENT":
		return 5
	case "PAIR_IDENTITY_MISMATCH", "LEAKAGE_DETECTED", "CONTENT_HASH_MISMATCH":
		return 6
	case "REGISTRY_CONFLICT":
		return 7
	case "IO_ERROR":
		return 8
	default:
		return 1
	}
}

func (d Diagnostic) JSON() ([]byte, error) { return json.Marshal(d) }
