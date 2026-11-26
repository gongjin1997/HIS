package v1alpha1

import (
	"errors"
	"fmt"
	"net/http"
)

// Canonical kinstall error codes (05 §5.2).
// Every semantic condition maps to exactly one KI-<DOMAIN>-<4DIGIT> code.
const (
	CodeAPIInvalidRequest       = "KI-API-0001"
	CodeAPIIdempotencyConflict  = "KI-API-0002"
	CodeAPIETagConflict         = "KI-API-0003"
	CodeAPIIdempotencyProgress  = "KI-API-0004"
	CodeAuthUnauthenticated     = "KI-AUTH-0001"
	CodeAuthForbidden           = "KI-AUTH-0002"
	CodeAuthAccountLocked       = "KI-AUTH-0003"
	CodeHostInvalidName         = "KI-HOST-0001"
	CodeHostClaimConflict       = "KI-HOST-0002"
	CodeHostNotFound            = "KI-HOST-0003"
	CodeExecSSHFailed           = "KI-EXEC-0001"
	CodeExecHostKeyMismatch     = "KI-EXEC-0002"
	CodeExecSudoFailed          = "KI-EXEC-0003"
	CodeExecSecurityBlocked     = "KI-EXEC-0004"
	CodeOSUnsupported           = "KI-OS-0001"
	CodeOSPkgMissing            = "KI-OS-0002"
	CodeOSGlibcTooOld           = "KI-OS-0003"
	CodeInitVerifyFailed        = "KI-INIT-0001"
	CodePlanExpiredOrDrifted    = "KI-PLAN-0001"
	CodePlanArchPageMismatch    = "KI-PLAN-0012"
	CodePlanTopologyMinHosts    = "KI-PLAN-0020"
	CodePlanTopologyAntiAffin   = "KI-PLAN-0021"
	CodeLockConflict            = "KI-LOCK-0001"
	CodeEngLeaseLost            = "KI-ENG-0001"
	CodeEngInvalidTransition    = "KI-ENG-0002"
	CodeEngUnknownTask          = "KI-ENG-0003"
	CodeEngCancelled            = "KI-ENG-0004"
	CodeEngAsyncOrphaned        = "KI-ENG-0005"
	CodePluginIncompatible      = "KI-PLG-0001"
	CodePluginNotFound          = "KI-PLG-0002"
	CodeTemplateRenderFailed    = "KI-TPL-0001"
	CodeArtifactVerifyFailed    = "KI-ART-0001"
	CodeArtifactTrustRootFailed = "KI-ART-0002"
	CodeArtifactSM2Failed       = "KI-ART-0003"
	CodeSecretKEKUnavailable    = "KI-SEC-0001"
	CodeSecretDecryptFailed     = "KI-SEC-0002" //nolint:gosec // G101 false positive: error code constant, not a credential
	CodePKIInvalidRequest       = "KI-PKI-0001"
	CodeDBUnavailableOrSchema   = "KI-DB-0001"
	CodeDBNotFound              = "KI-DB-0002"
	CodeDiffExternalDrift       = "KI-DIFF-0001"
	CodeDiffThreeWayConflict    = "KI-DIFF-0002"
	CodeDiffUnmanagedExists     = "KI-DIFF-0003"
	CodeStackCycleDetected      = "KI-STACK-0001"
	CodeStackReferencedInUse    = "KI-STACK-0002"
	CodeStackOutputRefInvalid   = "KI-STACK-0003"
	CodeWaitTimeout             = "KI-WAIT-0001"
	CodeImgMutableTag           = "KI-IMG-0001"
	CodeImgForbiddenRegistry    = "KI-IMG-0002"
	CodeImgRewriteInvalid       = "KI-IMG-0003"
	CodeSrcNotFound             = "KI-SRC-0001"
	CodeSrcMissingSHA256        = "KI-SRC-0002"
	CodeSrcDigestMismatch       = "KI-SRC-0003"
	CodeSrcPresignFailed        = "KI-SRC-0004"
	CodeSrcOfflineStrictDenied  = "KI-SRC-0005"
	CodeBundleMissingBaseRev    = "KI-BND-0001"
)

// Error is the canonical structured error model shared across SDK, internal layers,
// CLI, and RFC 9457 ProblemDetails HTTP responses (D1 §1.3-B, 05 §5.2).
type Error struct {
	Code          string         `json:"code"`
	Domain        string         `json:"domain"`
	HTTPStatus    int            `json:"httpStatus"`
	MsgKey        string         `json:"msgKey"`
	Args          map[string]any `json:"args,omitempty"`
	SuggestionKey string         `json:"suggestionKey,omitempty"`
	Retryable     bool           `json:"retryable"`
	Cause         error          `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.MsgKey, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.MsgKey)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewError builds a structured Error with canonical metadata.
func NewError(code, domain string, httpStatus int, msgKey string, retryable bool, cause error) *Error {
	if httpStatus == 0 {
		httpStatus = http.StatusInternalServerError
	}
	return &Error{
		Code:          code,
		Domain:        domain,
		HTTPStatus:    httpStatus,
		MsgKey:        msgKey,
		SuggestionKey: msgKey + ".suggestion",
		Retryable:     retryable,
		Cause:         cause,
	}
}

// WithArgs attaches safe structured context (must never contain plaintext secrets).
func (e *Error) WithArgs(args map[string]any) *Error {
	e.Args = args
	return e
}

// WithSuggestion overrides the i18n suggestion key.
func (e *Error) WithSuggestion(key string) *Error {
	e.SuggestionKey = key
	return e
}

// IsRetryable returns true only if err wraps a *Error explicitly marked Retryable.
// Unknown errors default to false to prevent side effects or triggering pam_faillock.
func IsRetryable(err error) bool {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Retryable
	}
	return false
}

// IsCode reports whether err wraps a *Error with the given canonical code.
func IsCode(err error, code string) bool {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Code == code
	}
	return false
}

// ErrorCode extracts the KI-* code from err, or returns empty string.
func ErrorCode(err error) string {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}
