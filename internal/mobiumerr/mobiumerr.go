// Package mobiumerr is the one error type Mobium's tools fail with, and the
// stable codes that classify it.
//
// Before it, a failure was a sentence: 470 fmt.Errorf calls, flattened to text
// where a tool call ends, one exception type in every client, and exit status
// 1 for everything. The sentences were good — the project requires each to
// name a cause and a remedy that works — but nothing could act on them without
// parsing prose, and the code itself decided "stale session" and "no alert" by
// matching message text. See docs/decisions/0005-errors.md.
//
// The codes are modeled on W3C WebDriver's — lowercase, underscore-separated,
// stable — because that is the vocabulary WebDriver users already
// catch, and the one WebDriverAgent and UiAutomator2 answer in. Where a W3C
// code names the same thing, the Mobium code has the same spelling
// (no_such_element, no_such_alert, timeout). `not_confirmed` has no W3C
// counterpart: it is the failure this project exists to report, a command
// that claimed success when reading the state back said otherwise.
//
// Codes are public API. Adding one is safe; renaming or removing one breaks
// every client that catches it. internal/apisurface checks that every code has
// an exception in every client, the same way it checks tools.
package mobiumerr

import (
	"errors"
	"fmt"
)

// Code classifies a failure. The string is what crosses the wire.
type Code string

const (
	// Unclassified is a failure nobody has given a code yet. It is a code in
	// its own right so the message still reaches a caller unchanged, and so a
	// count of it is a count of the migration left to do.
	Unclassified Code = "error"

	// NoDevice means there is nothing to drive: no device matches, or none is
	// connected.
	NoDevice Code = "no_device"
	// DeviceNotReady means the device is there and cannot be driven yet — a
	// locked phone, one not trusted, Developer Mode off, an emulator still
	// booting.
	DeviceNotReady Code = "device_not_ready"
	// ToolchainMissing means something on this machine is missing: adb, Xcode,
	// a signing certificate.
	ToolchainMissing Code = "toolchain_missing"

	// NoSuchElement means a locator or ref matched nothing on screen. Worth
	// scrolling for: the element may be below the fold.
	NoSuchElement Code = "no_such_element"
	// AmbiguousLocator means a locator matched more than one element. Never
	// resolved by guessing; the caller narrows it.
	AmbiguousLocator Code = "ambiguous_locator"
	// ElementNotReachable means the element was found and cannot be touched
	// where it is.
	ElementNotReachable Code = "element_not_reachable"
	// NoSuchContext means a WebView context that is not there.
	NoSuchContext Code = "no_such_context"
	// NoSuchAlert means a dialog was expected and none is on screen.
	NoSuchAlert Code = "no_such_alert"

	// Unsupported means this backend or platform cannot do what was asked,
	// and says why. Retrying cannot help; a different device might.
	Unsupported Code = "unsupported"
	// NotConfirmed means the command reported success and reading the state
	// back disagreed — `pm grant` on a permission the app never declared, a
	// keystroke iOS dropped.
	NotConfirmed Code = "not_confirmed"
	// Timeout means a wait ran out.
	Timeout Code = "timeout"
	// InvalidArgument means the request itself is wrong.
	InvalidArgument Code = "invalid_argument"
	// DeviceServer means the device side failed — WebDriverAgent,
	// UiAutomator2, or a device tool (adb, simctl, devicectl, lockdown) — in a
	// way no narrower code names. A device server's own W3C code, when it
	// sent one, is in Details["w3c"].
	DeviceServer Code = "device_server"
	// Internal means a bug in Mobium.
	Internal Code = "internal"
)

// Codes is every code, in a stable order. It is what the clients are checked
// against.
var Codes = []Code{
	Unclassified,
	NoDevice, DeviceNotReady, ToolchainMissing,
	NoSuchElement, AmbiguousLocator, ElementNotReachable, NoSuchContext, NoSuchAlert,
	Unsupported, NotConfirmed, Timeout, InvalidArgument, DeviceServer, Internal,
}

// Error is a classified failure.
//
// Message is the whole sentence a person reads, remedy included, exactly as
// the text channel has always carried it — so an MCP agent reading Content
// sees no change. Remedy repeats the part that says what to do, separately,
// for a caller that wants to act on it.
type Error struct {
	Code      Code
	Message   string
	Remedy    string
	Retryable bool
	Details   map[string]any
	Cause     error
}

func (e *Error) Error() string { return e.Message }

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Is makes a code usable as a sentinel: errors.Is(err, mobiumerr.Kind(NoDevice))
// is true for any NoDevice error, whatever it says.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Message == "" && t.Code == e.Code
}

// Kind returns a sentinel matching every error with the code.
func Kind(c Code) *Error { return &Error{Code: c} }

// New makes a classified error, formatted exactly as fmt.Errorf formats — so
// `%w` works, and the error it wraps becomes the Cause. That is what lets a
// site change from fmt.Errorf to New without rewording anything. Retryable
// starts from the code's default.
func New(c Code, format string, args ...any) *Error {
	f := fmt.Errorf(format, args...)
	return &Error{Code: c, Message: f.Error(), Cause: errors.Unwrap(f), Retryable: retryableByDefault[c]}
}

// Wrap classifies an existing error, keeping it as the cause. The message is
// the cause's unless one is given.
func Wrap(c Code, cause error, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	if msg == "" && cause != nil {
		msg = cause.Error()
	}
	return &Error{Code: c, Message: msg, Cause: cause, Retryable: retryableByDefault[c]}
}

// WithRemedy records what to do about it.
func (e *Error) WithRemedy(r string) *Error { e.Remedy = r; return e }

// WithDetail attaches one machine-readable fact.
func (e *Error) WithDetail(k string, v any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[k] = v
	return e
}

// retryableByDefault is whether the same call, made again unchanged, can
// reasonably succeed. Only a wait running out is, by default; a site that
// knows better sets it.
var retryableByDefault = map[Code]bool{Timeout: true}

// As returns the classified error inside err, if there is one.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// CodeOf classifies any error. One that was never given a code is
// Unclassified, never nothing.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	if e, ok := As(err); ok && e.Code != "" {
		return e.Code
	}
	return Unclassified
}

// Payload is an error as it crosses the wire: the structured half of a
// failed tool call, beside the unchanged text.
type Payload struct {
	Code      Code           `json:"code"`
	Message   string         `json:"message"`
	Remedy    string         `json:"remedy,omitempty"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

// PayloadOf renders any error for the wire.
func PayloadOf(err error) Payload {
	if e, ok := As(err); ok && e.Code != "" {
		return Payload{Code: e.Code, Message: err.Error(), Remedy: e.Remedy,
			Retryable: e.Retryable, Details: e.Details}
	}
	return Payload{Code: Unclassified, Message: err.Error()}
}

// FromPayload rebuilds an error received over the wire.
func FromPayload(p Payload) *Error {
	c := p.Code
	if c == "" {
		c = Unclassified
	}
	return &Error{Code: c, Message: p.Message, Remedy: p.Remedy,
		Retryable: p.Retryable, Details: p.Details}
}

// ExitCode is the CLI's exit status for a code: grouped, so a script can
// branch on the kind of failure without reading the message.
//
//	1  anything not below — including unclassified, device_server, internal
//	2  invalid_argument, and a command line cobra rejected
//	3  no_device, device_not_ready, toolchain_missing
//	4  no_such_element, ambiguous_locator, element_not_reachable,
//	   no_such_context, no_such_alert
//	5  unsupported
//	6  timeout
//	7  not_confirmed
func ExitCode(c Code) int {
	switch c {
	case InvalidArgument:
		return 2
	case NoDevice, DeviceNotReady, ToolchainMissing:
		return 3
	case NoSuchElement, AmbiguousLocator, ElementNotReachable, NoSuchContext, NoSuchAlert:
		return 4
	case Unsupported:
		return 5
	case Timeout:
		return 6
	case NotConfirmed:
		return 7
	}
	return 1
}

// Name is the code's name in the clients, in PascalCase: each client's
// exception is this plus its language's suffix — NoSuchElementError in Python
// and JavaScript, NoSuchElementException in Java and .NET, ErrNoSuchElement as
// a Go sentinel. Unclassified has no name: it is the base type itself.
//
// One departs from the code: timeout is TimedOut, because TimeoutError is a
// Python builtin and TimeoutException a Java one, and shadowing either would
// make `except TimeoutError` catch the wrong thing silently.
func Name(c Code) string {
	if c == Timeout {
		return "TimedOut"
	}
	if c == Unclassified {
		return ""
	}
	out := []byte{}
	up := true
	for i := 0; i < len(c); i++ {
		ch := c[i]
		if ch == '_' {
			up = true
			continue
		}
		if up && ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		out = append(out, ch)
		up = false
	}
	return string(out)
}
