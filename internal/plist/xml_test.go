package plist

import (
	"bytes"
	"testing"
)

// The shape usbmuxd answered ListDevices with on macOS 26, trimmed, and a
// pairing record's kinds of value. usbmuxd and lockdown accept a binary
// request and always answer in XML.
const usbmuxdReply = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>DeviceList</key>
	<array>
		<dict>
			<key>DeviceID</key>
			<integer>10</integer>
			<key>MessageType</key>
			<string>Attached</string>
			<key>Properties</key>
			<dict>
				<key>ConnectionType</key>
				<string>USB</string>
				<key>SerialNumber</key>
				<string>00008120-0001234567890ABC</string>
			</dict>
		</dict>
	</array>
	<key>EnableServiceSSL</key>
	<true/>
	<key>PairRecordData</key>
	<data>
	aGVsbG8g
	d29ybGQ=
	</data>
	<key>Ratio</key>
	<real>0.5</real>
	<key>Empty</key>
	<array/>
</dict>
</plist>`

func TestUnmarshalXML(t *testing.T) {
	v, err := Unmarshal([]byte(usbmuxdReply))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	dev := m["DeviceList"].([]any)[0].(map[string]any)
	if dev["DeviceID"] != int64(10) {
		t.Errorf("DeviceID = %#v", dev["DeviceID"])
	}
	if dev["Properties"].(map[string]any)["SerialNumber"] != "00008120-0001234567890ABC" {
		t.Errorf("nested dict lost: %#v", dev["Properties"])
	}
	if m["EnableServiceSSL"] != true {
		t.Errorf("true = %#v", m["EnableServiceSSL"])
	}
	// Data wraps across lines in usbmuxd's replies; the whitespace is not data.
	if !bytes.Equal(m["PairRecordData"].([]byte), []byte("hello world")) {
		t.Errorf("data = %q", m["PairRecordData"])
	}
	if m["Ratio"] != 0.5 {
		t.Errorf("real = %#v", m["Ratio"])
	}
	if a, ok := m["Empty"].([]any); !ok || len(a) != 0 {
		t.Errorf("empty array = %#v", m["Empty"])
	}

	// The binary path is untouched: a round trip still decodes as binary.
	b, err := Marshal(map[string]any{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := Unmarshal(b); err != nil || v.(map[string]any)["k"] != "v" {
		t.Errorf("binary round trip: %v %v", v, err)
	}
}

func TestUnmarshalXMLRefusesWhatItDoesNotKnow(t *testing.T) {
	for name, doc := range map[string]string{
		"a date":          `<plist><date>2026-09-23T00:00:00Z</date></plist>`,
		"a key w/o value": `<plist><dict><key>a</key></dict></plist>`,
		"not a plist":     `<?xml version="1.0"?><html></html>`,
		"deep nesting":    `<plist>` + repeat("<array>", 100) + repeat("</array>", 100) + `</plist>`,
	} {
		if _, err := Unmarshal([]byte(doc)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func repeat(s string, n int) string {
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		b.WriteString(s)
	}
	return b.String()
}
