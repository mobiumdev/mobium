package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

func TestResultCarriesBothRenderings(t *testing.T) {
	r := Result("@e1 Sign In (button)", MapView{
		Elements: []ElementView{{Ref: "@e1", Label: "Sign In", Role: "button"}},
	})
	if len(r.Content) != 1 || r.Content[0].Text != "@e1 Sign In (button)" {
		t.Errorf("text rendering = %+v", r.Content)
	}
	if r.StructuredContent == nil {
		t.Fatal("no structured content")
	}
}

func TestStructuredContentIsOmittedWhenAbsent(t *testing.T) {
	// A tool with nothing structured to say must not emit a null field: MCP
	// clients that predate structuredContent should see exactly what they saw
	// before.
	data, err := json.Marshal(TextResult("done"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "structuredContent") {
		t.Errorf("plain text result carried the field: %s", data)
	}
}

func TestStructuredContentSurvivesJSON(t *testing.T) {
	// Everything crosses the daemon socket as JSON, so a shape that does not
	// round-trip is a shape the CLI and the clients never see.
	original := Result("x", MapView{
		Context: "NATIVE_APP",
		Device:  "emulator-5554",
		Elements: []ElementView{{
			Ref:     "@e1",
			Label:   "Sign In",
			Role:    "button",
			Locator: &LocatorView{Kind: "testid", Value: "com.x:id/submit", Exact: true},
			Bounds:  BoundsView{X1: 48, Y1: 860, X2: 1032, Y2: 1000},
		}},
	})

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var round struct {
		StructuredContent MapView `json:"structuredContent"`
	}
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("round trip: %v", err)
	}

	got := round.StructuredContent
	if len(got.Elements) != 1 {
		t.Fatalf("elements = %d", len(got.Elements))
	}
	e := got.Elements[0]
	if e.Ref != "@e1" || e.Label != "Sign In" || e.Role != "button" {
		t.Errorf("element = %+v", e)
	}
	if e.Locator == nil || e.Locator.Kind != "testid" || !e.Locator.Exact {
		t.Errorf("locator = %+v", e.Locator)
	}
	if e.Bounds != (BoundsView{48, 860, 1032, 1000}) {
		t.Errorf("bounds = %+v", e.Bounds)
	}
	if got.Context != "NATIVE_APP" || got.Device != "emulator-5554" {
		t.Errorf("context/device = %q/%q", got.Context, got.Device)
	}
}

func TestBoundsCenter(t *testing.T) {
	x, y := BoundsView{48, 860, 1032, 1000}.Center()
	if x != 540 || y != 930 {
		t.Errorf("center = (%d,%d), want (540,930)", x, y)
	}
}

func TestElementViewFromEntry(t *testing.T) {
	entry := uitree.Entry{
		Ref:     "@e5",
		Label:   "Sign In",
		Role:    "button",
		Locator: uitree.Locator{Kind: uitree.KindTestID, Value: "submit", Exact: true, Role: "button"},
		Bounds:  uitree.Rect{X1: 1, Y1: 2, X2: 3, Y2: 4},
	}
	v := elementView(entry)
	if v.Ref != "@e5" || v.Label != "Sign In" || v.Role != "button" {
		t.Errorf("view = %+v", v)
	}
	if v.Locator.Kind != "testid" || v.Locator.Value != "submit" ||
		!v.Locator.Exact || v.Locator.Role != "button" {
		t.Errorf("locator = %+v", v.Locator)
	}
	if v.Bounds != (BoundsView{1, 2, 3, 4}) {
		t.Errorf("bounds = %+v", v.Bounds)
	}
}

func TestEmptyCollectionsMarshalAsArrays(t *testing.T) {
	// A client that indexes the result should not have to special-case null.
	for name, v := range map[string]interface{}{
		"map":      MapView{Elements: []ElementView{}},
		"devices":  DevicesView{Devices: []DeviceView{}},
		"contexts": ContextsView{Contexts: []ContextView{}},
	} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(data), "null") {
			t.Errorf("%s marshalled a null collection: %s", name, data)
		}
	}
}
