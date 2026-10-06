package agent

import (
	"encoding/json"
	"testing"
)

// A hook call is typed on an iPhone's keyboard, which has dropped letters
// outside its layout: everything outside ASCII goes as an escape the app's
// JSON parser turns back.
func TestHookCallIsASCII(t *testing.T) {
	call, err := asciiJSON(map[string]interface{}{"i": "x", "h": "signIn", "a": []string{"Лана", "café", "🙂"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range call {
		if r > 0x7f {
			t.Fatalf("non-ASCII %q in %s", r, call)
		}
	}
	var back struct {
		A []string `json:"a"`
	}
	if err := json.Unmarshal([]byte(call), &back); err != nil {
		t.Fatal(err)
	}
	if back.A[0] != "Лана" || back.A[1] != "café" || back.A[2] != "🙂" {
		t.Errorf("round trip: %v", back.A)
	}
}
