package domain

import (
	"errors"
	"fmt"
)

// Canonical LiveOps domain sentinel errors (strictly transport-independent).
var (
	ErrUnauthorized         = errors.New("unauthorized")
	ErrForbidden            = errors.New("forbidden")
	ErrNotFound             = errors.New("not found")
	ErrValidation           = errors.New("validation error")
	ErrAlreadyExists        = errors.New("already exists")
	ErrIdempotencyConflict  = errors.New("idempotency conflict")
	ErrVersionMismatch      = errors.New("version mismatch")
	ErrPreconditionRequired = errors.New("precondition required")
	ErrStateConflict        = errors.New("state conflict")
	ErrReferenceInUse       = errors.New("reference in use")
	ErrRateLimited          = errors.New("rate limited")
	ErrCursorReset          = errors.New("cursor reset required")
	ErrSnapshotRequired     = errors.New("snapshot required")
	ErrReceiptConflict      = errors.New("receipt conflict")
	ErrStaleLease           = errors.New("stale lease epoch")
)

// CodedError carries a machine-readable LiveOps error code and human message while wrapping a sentinel error.
type CodedError struct {
	Sentinel error
	Code     string
	Message  string
	Details  any
}

func (e *CodedError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Code
}

func (e *CodedError) Unwrap() error { return e.Sentinel }

func NewError(sentinel error, code, msg string, details any) error {
	return &CodedError{Sentinel: sentinel, Code: code, Message: msg, Details: details}
}

func ValidationError(msg string) error {
	return NewError(ErrValidation, "VALIDATION_ERROR", msg, nil)
}

func NotFoundError(msg string) error {
	return NewError(ErrNotFound, "NOT_FOUND", msg, nil)
}

func ConflictError(code, msg string) error {
	return NewError(ErrStateConflict, code, msg, nil)
}

// Scope identifies the mandatory multi-tenant isolation tuple (tenant_id, app_id).
type Scope struct {
	TenantID  string `json:"tenant_id"`
	AppID     string `json:"app_id"`
	ProjectID string `json:"project_id,omitempty"`
}

func (s Scope) Validate() error {
	if s.TenantID == "" {
		return ValidationError("tenant_id is required")
	}
	if s.AppID == "" {
		return ValidationError("app_id is required")
	}
	return nil
}
