package device

import (
	"strings"
	"testing"
	"time"
)

// A listing of an app's Documents, in the shape `devicectl device info
// files --subdirectory Documents` gave on the iPhone 15 Plus: the files in
// the folder are kept, sorted, and a folder, or what is inside one, is not.
func TestParsePhoneFilesKeepsTheFolderFilesAlone(t *testing.T) {
	raw := []byte(`{"domain": "appDataContainer", "files": [
		{"relativePath": "probe.txt", "metadata": {"size": 20, "lastModDate": "2020-01-01T00:00:00.000Z"},
		 "resources": {"isDirectory": false}},
		{"relativePath": "Inbox", "metadata": {"size": 64, "lastModDate": "2026-09-29T19:30:00.000Z"},
		 "resources": {"isDirectory": true}},
		{"relativePath": "Inbox/sent.pdf", "metadata": {"size": 900, "lastModDate": "2026-09-29T19:30:00.000Z"},
		 "resources": {"isDirectory": false}},
		{"relativePath": "invoice-42.txt", "metadata": {"size": 24, "lastModDate": "2026-09-29T19:37:12.000Z"},
		 "resources": {"isDirectory": false}}
	]}`)
	files, err := parsePhoneFiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "invoice-42.txt" || files[1].Name != "probe.txt" {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Bytes != 24 || !files[0].Modified.Equal(time.Date(2026, 9, 29, 19, 37, 12, 0, time.UTC)) {
		t.Errorf("invoice-42.txt = %+v", files[0])
	}
}

// A message names a phone by its model, never by its owner's name for it;
// with no model it says "the iPhone" (CHALLENGES 175).
func TestAPhoneIsLabeledByItsModel(t *testing.T) {
	p := Phone{Name: "Somebody's iPhone", Model: "iPhone 15 Plus"}
	if got := p.Label(); got != "iPhone 15 Plus" {
		t.Errorf("Label() = %q", got)
	}
	p.Model = ""
	if got := p.Label(); got != "the iPhone" {
		t.Errorf("with no model, Label() = %q", got)
	}
	for _, err := range []error{Phone{Name: "Somebody's iPhone", Model: "iPhone 15 Plus"}.Usable()} {
		if err != nil && strings.Contains(err.Error(), "Somebody") {
			t.Errorf("a refusal names the owner: %v", err)
		}
	}
}
