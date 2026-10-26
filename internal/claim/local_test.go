package claim

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A local model that answers in prose is asked again for its verdict, and
// one that keeps to prose ends the check as no verdict, not a loop.
func TestALocalModelAnsweringInProseIsAskedAgain(t *testing.T) {
	for _, tc := range []struct {
		prose int // answers in text before calling a tool
		calls int
	}{{prose: 1, calls: 1}, {prose: 9, calls: 0}} {
		asked := 0
		var last []map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Messages []map[string]interface{} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if got := r.Header.Get("Authorization"); got != "Bearer k" {
				t.Errorf("authorization: %q", got)
			}
			last = req.Messages
			asked++
			msg := map[string]interface{}{"role": "assistant", "content": "The claim is true."}
			if asked > tc.prose {
				msg["tool_calls"] = []interface{}{map[string]interface{}{"id": "1", "type": "function",
					"function": map[string]interface{}{"name": verdictTool, "arguments": `{}`}}}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{map[string]interface{}{"message": msg}}})
		}))
		t.Setenv("MOBIUM_AI_BASE_URL", srv.URL)
		t.Setenv("MOBIUM_AI_API_KEY", "k")
		step, err := NewLocal("m").Begin(context.Background(), "system", "claim", nil)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(step.Calls) != tc.calls || asked != min(tc.prose+1, maxNudges+1) {
			t.Errorf("prose %d: %d calls after %d asks", tc.prose, len(step.Calls), asked)
		}
		if tc.prose == 1 && (len(last) != 4 || last[3]["role"] != "user") {
			t.Errorf("the second ask did not carry the request for a verdict: %v", last)
		}
	}
}

// A refusal's remedy fits its cause: a rate limit is waited out, a key is
// set, and only a missing model sends the caller to its list.
func TestARefusalNamesARemedyThatFits(t *testing.T) {
	for status, want := range map[int]struct {
		code   mobiumerr.Code
		remedy string
	}{
		429: {mobiumerr.Timeout, "wait a minute"},
		503: {mobiumerr.Timeout, "wait a minute"},
		401: {mobiumerr.ToolchainMissing, "MOBIUM_AI_API_KEY"},
		404: {mobiumerr.ToolchainMissing, "ollama list"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		t.Setenv("MOBIUM_AI_BASE_URL", srv.URL)
		_, err := NewLocal("m").Begin(context.Background(), "system", "claim", nil)
		srv.Close()
		if mobiumerr.CodeOf(err) != want.code || !strings.Contains(mobiumerr.Text(err), want.remedy) {
			t.Errorf("%d: %s", status, mobiumerr.Text(err))
		}
	}
}
