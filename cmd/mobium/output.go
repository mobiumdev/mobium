package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os"
)

// printError reports a failure in the shape the caller asked for. Errors go
// to stderr in text mode so a piped `mobium text` never mixes them into the
// output an agent is parsing.
func printError(err error) {
	// A silent error carries an exit code and nothing to say — doctor uses it
	// after printing its own report.
	if errors.Is(err, errSilent) {
		return
	}
	if jsonOutput {
		// "error" is the key this has always printed, kept so nothing that
		// reads it breaks; the payload beside it is the classified error.
		_ = printJSON(struct {
			Error string `json:"error"`
			mobiumerr.Payload
		}{err.Error(), mobiumerr.PayloadOf(err)})
		return
	}
	fmt.Fprintln(os.Stderr, "error: "+err.Error())
}

// commandRan is set once cobra accepts the command line. See main.
var commandRan bool

// exitStatus is the process exit code for a failure: grouped by kind, so a
// script can tell "no device" from "no such element" without reading the
// message. mobiumerr.ExitCode has the table.
func exitStatus(err error) int {
	if errors.Is(err, errSilent) {
		return 1
	}
	return mobiumerr.ExitCode(mobiumerr.CodeOf(err))
}

// printJSON writes a value as indented JSON.
//
// Without HTML escaping: Go's default turns > and & into \u003e and \u0026,
// which is safe inside an HTML page and unreadable everywhere this output
// goes — "Settings > Display & Brightness" is a remedy people read.
func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
