// Package apperr defines the standard FloatTranslate application error model:
// stable machine-readable codes (docs/04 §2), human-readable messages
// (Chinese is fine) and the standard JSON error envelope:
//
//	{"error":{"code":"...","message":"...","retryable":false,"details":{}}}
package apperr

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Code is a stable machine-readable error code.
type Code string

// Error codes from docs/04 §2.
const (
	CodeUnauthorizedLocalSession Code = "UNAUTHORIZED_LOCAL_SESSION"
	CodeInvalidRequest           Code = "INVALID_REQUEST"
	CodeUnsupportedLanguage      Code = "UNSUPPORTED_LANGUAGE"
	CodeProviderNotConfigured    Code = "PROVIDER_NOT_CONFIGURED"
	CodeProviderConnectionFailed Code = "PROVIDER_CONNECTION_FAILED"
	CodeProviderUnavailable      Code = "PROVIDER_UNAVAILABLE"
	CodeProviderContextLimit     Code = "PROVIDER_CONTEXT_LIMIT"
	CodeStructuredOutputInvalid  Code = "STRUCTURED_OUTPUT_INVALID"
	CodeTranslationFailed        Code = "TRANSLATION_FAILED"
	CodeDatabaseError            Code = "DATABASE_ERROR"
	CodeMigrationFailed          Code = "MIGRATION_FAILED"
	CodeNotFound                 Code = "NOT_FOUND"
	CodeConflict                 Code = "CONFLICT"
	CodeGenerationAlreadyActive  Code = "GENERATION_ALREADY_ACTIVE"
	CodeBackupVersionUnsupported Code = "BACKUP_VERSION_UNSUPPORTED"

	// CodeInternal is used for unrecovered panics and unmapped internal
	// errors. docs/04 §2 has no dedicated code for this case; INTERNAL_ERROR
	// is the conventional extension (flagged to the coordinator).
	CodeInternal Code = "INTERNAL_ERROR"
)

// HTTPStatus maps an error code to its HTTP status.
func (c Code) HTTPStatus() int {
	switch c {
	case CodeUnauthorizedLocalSession:
		return http.StatusUnauthorized
	case CodeInvalidRequest, CodeUnsupportedLanguage, CodeProviderNotConfigured,
		CodeProviderContextLimit, CodeBackupVersionUnsupported:
		return http.StatusBadRequest
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict, CodeGenerationAlreadyActive:
		return http.StatusConflict
	case CodeProviderConnectionFailed, CodeStructuredOutputInvalid:
		return http.StatusBadGateway
	case CodeProviderUnavailable:
		return http.StatusServiceUnavailable
	case CodeTranslationFailed, CodeDatabaseError, CodeMigrationFailed, CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// E is the standard application error implementing the error interface.
type E struct {
	Code      Code           `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
	cause     error
}

// New creates an application error.
func New(code Code, message string, retryable bool) *E {
	return &E{Code: code, Message: message, Retryable: retryable}
}

// Wrap creates an application error that keeps cause for error-chain
// inspection (errors.Is / errors.As) while presenting message to users.
func Wrap(code Code, message string, retryable bool, cause error) *E {
	return &E{Code: code, Message: message, Retryable: retryable, cause: cause}
}

// WithDetails attaches structured details to the error and returns it.
func (e *E) WithDetails(details map[string]any) *E {
	e.Details = details
	return e
}

// Error implements the error interface.
func (e *E) Error() string {
	if e.cause != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.cause.Error()
	}
	return string(e.Code) + ": " + e.Message
}

// Unwrap exposes the underlying cause.
func (e *E) Unwrap() error { return e.cause }

// AsE converts any error into *E. An *E passes through; anything else is
// mapped to a non-retryable INTERNAL_ERROR.
func AsE(err error) *E {
	if err == nil {
		return nil
	}
	var e *E
	if errors.As(err, &e) {
		return e
	}
	return Wrap(CodeInternal, "内部错误", false, err)
}

// Body is the on-the-wire error envelope.
type Body struct {
	Error Inner `json:"error"`
}

// Inner is the error object inside the envelope.
type Inner struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

// Envelope builds the standard JSON envelope for err.
func Envelope(err error) Body {
	e := AsE(err)
	return Body{Error: Inner{
		Code:      string(e.Code),
		Message:   e.Message,
		Retryable: e.Retryable,
		Details:   e.Details,
	}}
}

// WriteHTTP writes err as the standard error envelope with the mapped HTTP
// status and a Content-Type of application/json.
func WriteHTTP(w http.ResponseWriter, err error) {
	e := AsE(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Code.HTTPStatus())
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(Envelope(e))
}
