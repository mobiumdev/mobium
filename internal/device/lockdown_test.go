package device

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/plist"
)

// fakeUsbmuxd answers each connection's one request with reply(request).
func fakeUsbmuxd(t *testing.T, reply func(req map[string]any) string) {
	t.Helper()
	sock := filepath.Join(shortTempDir(t), "mux")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	t.Setenv("MOBIUM_USBMUXD_SOCKET", sock)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				hdr := make([]byte, 16)
				if _, err := io.ReadFull(c, hdr); err != nil {
					return
				}
				body := make([]byte, binary.LittleEndian.Uint32(hdr)-16)
				io.ReadFull(c, body)
				v, _ := plist.Unmarshal(body)
				out := []byte(reply(v.(map[string]any)))
				binary.LittleEndian.PutUint32(hdr, uint32(16+len(out)))
				c.Write(append(hdr, out...))
			}(c)
		}
	}()
}

const muxDevices = `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>
<key>DeviceList</key><array>
 <dict><key>Properties</key><dict><key>DeviceID</key><integer>7</integer><key>SerialNumber</key><string>other</string></dict></dict>
 <dict><key>Properties</key><dict><key>DeviceID</key><integer>10</integer><key>SerialNumber</key><string>00008120-0001234567890ABC</string></dict></dict>
</array></dict></plist>`

func TestMuxDeviceIDAndConnect(t *testing.T) {
	var gotPort any
	fakeUsbmuxd(t, func(req map[string]any) string {
		switch req["MessageType"] {
		case "ListDevices":
			return muxDevices
		case "Connect":
			gotPort = req["PortNumber"]
			return `<plist><dict><key>MessageType</key><string>Result</string><key>Number</key><integer>0</integer></dict></plist>`
		}
		return `<plist><dict/></plist>`
	})
	ctx := context.Background()

	id, err := muxDeviceID(ctx, "00008120-0001234567890ABC")
	if err != nil || id != 10 {
		t.Fatalf("device id = %d, %v; want 10", id, err)
	}
	if _, err := muxDeviceID(ctx, "not-plugged-in"); err == nil ||
		!strings.Contains(err.Error(), "cable") {
		t.Errorf("a missing phone was not reported with its remedy: %v", err)
	}

	// usbmuxd wants the port byte-swapped inside a little-endian integer:
	// lockdown's 62078 (0xF27E) goes over as 0x7EF2. Getting this wrong
	// connects to some other port entirely.
	c, err := muxConnect(ctx, id, lockdownPort)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if gotPort != int64(0x7EF2) {
		t.Errorf("PortNumber = %#v, want 0x7EF2", gotPort)
	}
}

func TestMuxConnectRefusal(t *testing.T) {
	fakeUsbmuxd(t, func(req map[string]any) string {
		return `<plist><dict><key>MessageType</key><string>Result</string><key>Number</key><integer>3</integer></dict></plist>`
	})
	if _, err := muxConnect(context.Background(), 10, 1234); err == nil {
		t.Error("a refused connect was reported as open")
	}
}
