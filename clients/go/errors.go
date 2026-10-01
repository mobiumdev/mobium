package mobium

import (
	"encoding/json"
	"errors"
)

// Code classifies a failure. The strings are Mobium's stable error codes,
// the same in every client and on the wire; see the error codes in the
// mobium repository's docs/guides/cli.md. A client is not required to know every code: one it
// does not recognize is still an *Error, with Code set.
type Code string

// The codes, and a sentinel for each: errors.Is(err, mobium.ErrNoSuchElement)
// is true for any failure with that code, whatever its message.
const (
	CodeError               Code = "error"
	CodeNoDevice            Code = "no_device"
	CodeDeviceNotReady      Code = "device_not_ready"
	CodeToolchainMissing    Code = "toolchain_missing"
	CodeNoSuchElement       Code = "no_such_element"
	CodeAmbiguousLocator    Code = "ambiguous_locator"
	CodeElementNotReachable Code = "element_not_reachable"
	CodeNoSuchContext       Code = "no_such_context"
	CodeNoSuchAlert         Code = "no_such_alert"
	CodeUnsupported         Code = "unsupported"
	CodeNotConfirmed        Code = "not_confirmed"
	CodeTimeout             Code = "timeout"
	CodeInvalidArgument     Code = "invalid_argument"
	CodeDeviceServer        Code = "device_server"
	CodeInternal            Code = "internal"
)

var (
	ErrNoDevice            = &Error{Code: CodeNoDevice}
	ErrDeviceNotReady      = &Error{Code: CodeDeviceNotReady}
	ErrToolchainMissing    = &Error{Code: CodeToolchainMissing}
	ErrNoSuchElement       = &Error{Code: CodeNoSuchElement}
	ErrAmbiguousLocator    = &Error{Code: CodeAmbiguousLocator}
	ErrElementNotReachable = &Error{Code: CodeElementNotReachable}
	ErrNoSuchContext       = &Error{Code: CodeNoSuchContext}
	ErrNoSuchAlert         = &Error{Code: CodeNoSuchAlert}
	ErrUnsupported         = &Error{Code: CodeUnsupported}
	ErrNotConfirmed        = &Error{Code: CodeNotConfirmed}
	// TimedOut, not Timeout, as in every client: Python's TimeoutError and
	// Java's TimeoutException are builtins a Timeout name would shadow.
	ErrTimedOut        = &Error{Code: CodeTimeout}
	ErrInvalidArgument = &Error{Code: CodeInvalidArgument}
	ErrDeviceServer    = &Error{Code: CodeDeviceServer}
	ErrInternal        = &Error{Code: CodeInternal}
)

// Is makes the sentinels above match by code.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return t.Reason == "" && t.Tool == "" && t.Code != "" && t.Code == e.Code
}

// payload is the structured half of a failed tool call.
type payload struct {
	Code      Code           `json:"code"`
	Message   string         `json:"message"`
	Remedy    string         `json:"remedy"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details"`
}

// toolError builds the error for a failed tool call: the text as before, and
// the code, remedy and details when the daemon sent them. An older daemon
// sends only text, and that still arrives, as CodeError.
func toolError(tool, text string, structured json.RawMessage) *Error {
	e := &Error{Tool: tool, Reason: text, Code: CodeError}
	var p payload
	if len(structured) > 0 && json.Unmarshal(structured, &p) == nil && p.Code != "" {
		e.Code, e.Remedy, e.Retryable, e.Details = p.Code, p.Remedy, p.Retryable, p.Details
	}
	return e
}
