package conversion

import "fmt"

type Code string

const (
	CodeInvalidSpec       Code = "invalid_spec"
	CodeUnsupportedCodec  Code = "unsupported_codec"
	CodeNegotiationFailed Code = "negotiation_failed"
	CodeBuildFailed       Code = "build_failed"
	CodePipelineFailed    Code = "pipeline_failed"
	CodeCanceled          Code = "canceled"
	CodeNotFound          Code = "not_found"
	CodeNotReady          Code = "not_ready"
	CodePayloadTooLarge   Code = "payload_too_large"
	CodeInternal          Code = "internal"
)

type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.cause }

// NewError builds an Error carrying code, for use by callers (such as
// example/web's HTTP layer) that need the same {code, message} shape for
// conditions conversion itself never produces (e.g. an unknown job ID).
func NewError(code Code, message string) error {
	return newError(code, message)
}

func newError(code Code, message string) error {
	return &Error{Code: code, Message: message}
}

func invalidSpec(message string) error {
	return newError(CodeInvalidSpec, message)
}

func wrapError(code Code, context string, err error) error {
	return &Error{Code: code, Message: fmt.Sprintf("%s: %v", context, err), cause: err}
}
