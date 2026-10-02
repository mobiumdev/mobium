// Package reader holds Mobium's own reader of an Android screen: a small dex,
// run with app_process, that writes what `uiautomator dump` writes over a
// UiAutomation connection that leaves other accessibility services running.
//
// `uiautomator dump` suppresses every other accessibility service while it
// reads, so on a device with TalkBack or VoiceView on, each read silenced the
// screen reader, and an app that publishes its contents only to a screen
// reader stopped publishing them before the read could see them (measured on
// a Fire TV, CHALLENGES 208). Main.java is the source; reader.dex is built
// from it by `make reader` and pinned here by checksum, as every device-side
// artifact is.
package reader

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

//go:embed reader.dex
var dex []byte

// SHA256 is reader.dex's checksum. `make reader` rebuilds the dex and prints
// the new one; d8 9.3.16 builds the same bytes from the same source.
const SHA256 = "8d7ff8b46994cdfb88226e282cfe5328a9ffb076aa73923311ebb167ba61aa97"

// Class is the entry point app_process runs.
const Class = "dev.mobium.reader.Main"

// Dir is where a read puts the dex on the device. A folder of its own, because
// the runtime compiles the dex into an oat/ folder beside it, and deleting the
// folder takes both.
const Dir = "/data/local/tmp/mobium-reader"

const (
	begin     = "MOBIUM-READER-BEGIN"
	end       = "MOBIUM-READER-END"
	errorMark = "MOBIUM-READER-ERROR: "
)

// Dex is the reader, checked against its pinned checksum.
func Dex() ([]byte, error) {
	sum := sha256.Sum256(dex)
	if got := hex.EncodeToString(sum[:]); got != SHA256 {
		return nil, mobiumerr.New(mobiumerr.Internal, "the embedded screen reader-safe reader has checksum %s, "+
			"want %s — rebuild it with `make reader`", got, SHA256)
	}
	return dex, nil
}

// Hierarchy takes the hierarchy out of what the reader printed. Anything a
// vendor library wrote first is ignored: a Fire TV prints a line of its own
// before a screencap's PNG, and app_process runs the same libraries.
func Hierarchy(out []byte) ([]byte, error) {
	if i := bytes.Index(out, []byte(errorMark)); i >= 0 {
		msg := out[i+len(errorMark):]
		if j := bytes.IndexByte(msg, '\n'); j >= 0 {
			msg = msg[:j]
		}
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the reader could not read the screen: %s",
			bytes.TrimSpace(msg))
	}
	b := bytes.Index(out, []byte(begin))
	e := bytes.LastIndex(out, []byte(end))
	if b < 0 || e < b {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the reader printed no hierarchy: %q",
			firstLine(bytes.TrimSpace(out)))
	}
	xml := bytes.TrimSpace(out[b+len(begin) : e])
	if len(xml) == 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the reader printed an empty hierarchy")
	}
	return xml, nil
}

func firstLine(b []byte) []byte {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i]
	}
	return b
}
