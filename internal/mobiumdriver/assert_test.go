package mobiumdriver

import "testing"

// The compiler checks the capability contracts; this file records which
// backend is expected to satisfy which, so dropping one is a build failure
// rather than a runtime surprise.
var (
	_ Driver    = (*Android)(nil)
	_ Gesturer  = (*Android)(nil)
	_ Driver    = (*UIA2)(nil)
	_ Gesturer  = (*UIA2)(nil)
	_ TextEntry = (*UIA2)(nil)
	_ Starter   = (*UIA2)(nil)
)

func TestDumpBackendDoesNotClaimTextEntry(t *testing.T) {
	// `adb shell input text` types into whatever has focus and mangles
	// quotes and non-ASCII. Claiming the capability would make app_type
	// silently enter the wrong string instead of saying it cannot.
	if _, ok := interface{}((*Android)(nil)).(TextEntry); ok {
		t.Error("the dump backend claims TextEntry")
	}
}

// WebDriverAgent satisfies every capability, so an iOS session is not a
// second-class one.
var (
	_ Driver    = (*WDA)(nil)
	_ Gesturer  = (*WDA)(nil)
	_ TextEntry = (*WDA)(nil)
	_ Starter   = (*WDA)(nil)
	_ Health    = (*WDA)(nil)
	_ Health    = (*UIA2)(nil)

	// App lifecycle is supported everywhere: driving an app you cannot launch
	// is most of the job missing.
	_ AppControl = (*Android)(nil)
	_ AppControl = (*UIA2)(nil)
	_ AppControl = (*WDA)(nil)
)
