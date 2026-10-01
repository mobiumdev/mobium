package mobiumdriver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// WebDriverAgent's answer as an iPhone gave it: a hit-region finding naming
// its element, in points, and a contrast finding naming none.
const auditAnswer = `{"value":[
 {"auditType":"XCUIAccessibilityAuditTypeHitRegion","compactDescription":"Hit area is too small",
  "detailedDescription":"The size of this element is too small for user to interact.","element":"\"Dictate\" Button",
  "elementAttributes":{"label":"Dictate","identifier":"Dictate","rect":{"x":335.7,"y":811,"width":17.3,"height":22}}},
 {"auditType":"XCUIAccessibilityAuditTypeContrast","compactDescription":"Contrast failed",
  "detailedDescription":"Contrast failed for element","element":"","elementAttributes":{}}]}`

func TestTheAuditIsReadInPixelsAndKeepsFindingsWithNoElement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/wda/performAccessibilityAudit") {
			w.Write([]byte(auditAnswer))
			return
		}
		w.Write([]byte(`{"value":null}`))
	}))
	defer srv.Close()
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	got, err := (&WDA{w3c: c, scale: 3}).Audit(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("Audit = %+v, %v", got, err)
	}
	if f := got[0]; f.Type != "HitRegion" || f.TestID != "Dictate" || f.Bounds.X1 != 1005 || f.Bounds.Y2 != 2499 {
		t.Errorf("the hit-region finding read as %+v", f)
	}
	if f := got[1]; f.Type != "Contrast" || !f.Bounds.Empty() || f.Element != "" {
		t.Errorf("the finding with no element read as %+v", f)
	}
}
