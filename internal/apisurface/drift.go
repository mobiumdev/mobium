package apisurface

import (
	"fmt"
	"sort"
	"strings"
)

// Drift control.
//
// The architecture's central claim is that every front door reaches the same
// tool layer. That claim decays quietly: a tool added to the schema and not to
// the CLI, or to three clients and not the fourth, is invisible until somebody
// reaches for it and finds nothing. The sweep makes the claim checkable; this
// makes it enforced.
//
// The rule is not "everything must cover everything" — that would be a rule
// nobody could keep, and the first legitimate exception would see it deleted.
// The rule is **no undeclared gap**: a surface may miss a tool if the omission
// is written down here with a reason. A declared gap passes; an undeclared one
// fails; and an exemption for a tool or surface that no longer exists fails
// too, so the list cannot rot into a blanket excuse.

// Exemption is one deliberate gap.
type Exemption struct {
	Tool string
	// Surface is "cli" or a client language.
	Surface string
	Reason  string
}

// exemptions are the gaps that are meant to be there.
//
// Empty, today, and that is worth saying out loud: every one of the 35 tools
// is reachable from the CLI and from all five clients. The mechanism exists so
// the first real exception is a decision somebody writes down rather than a
// silent regression — and so that adding one is visibly a choice.
var exemptions = []Exemption{}

// Check reports every undeclared gap and every exemption that no longer
// describes anything real.
func Check(s *Surface) []string {
	var problems []string

	declared := map[string]string{}
	for _, e := range exemptions {
		declared[e.Tool+"/"+e.Surface] = e.Reason
	}

	known := map[string]bool{}
	for _, t := range s.Tools {
		known[t.Name] = true
		for _, missing := range t.Missing {
			key := t.Name + "/" + missing
			if _, ok := declared[key]; !ok {
				problems = append(problems, fmt.Sprintf(
					"%s is not reachable from %s, and that gap is not declared in "+
						"apisurface.exemptions — either wire it up or record why not",
					t.Name, missing))
			}
			delete(declared, key)
		}
	}

	// Anything left is an exemption for a gap that has been closed, or for a
	// tool that no longer exists. Both are stale and both must be removed, or
	// the list slowly becomes a place where real drift can hide.
	for key, reason := range declared {
		problems = append(problems, fmt.Sprintf(
			"the exemption for %s is stale — that surface now covers the tool, "+
				"or the tool is gone. Reason given was: %s", key, reason))
	}
	return problems
}

// Surfaces names every surface a tool can be reached from, for messages.
func Surfaces() []string {
	out := []string{"cli"}
	for _, c := range clientSources {
		out = append(out, c.Language)
	}
	return out
}

// Flag drift.
//
// The same shape as above, one level down. Where Check asks whether a surface
// reaches a tool at all, CheckFlags asks whether the CLI and the schema agree
// about what that tool accepts — and it is worth enforcing separately because
// the two ends fail in opposite directions and neither one shouts.
//
// An undeclared key is refused by an MCP client, because every schema is
// `additionalProperties: false`, and silently ignored by the CLI, because a
// handler reads the keys it knows by name. A misspelled flag therefore works
// on neither surface and complains on only one.

// ArgExemption is one argument deliberately unreachable from the CLI.
type ArgExemption struct {
	Tool   string
	Arg    string
	Reason string
}

// argExemptions are the arguments an MCP client can send and no command can.
//
// A gap here is a real asymmetry between the front doors, so each one needs a
// reason that survives being read out loud.
var argExemptions = []ArgExemption{
	{"app_install", "content", "set by the CLI and pipe from --path when the daemon is on another machine; a person gives a path"},
	{"app_install", "name", "set by the CLI and pipe with content, from the path's file name"},
	{"app_location", "gpx_data", "set by the CLI and pipe from --gpx when the daemon is on another machine; a person gives a path"},
	{"app_upload", "content", "set by the CLI and pipe from the file when the daemon is on another machine; a person gives a path"},
	{"app_record", "return_data", "set by the CLI and pipe on stop when the daemon is on another machine, which then save the video at the path given"},
}

// CheckFlags reports keys the CLI sends that no tool accepts, arguments no
// command can set, and exemptions that have stopped describing anything.
func CheckFlags(args []ArgEntry, cmds []CommandFlags) []string {
	var problems []string

	for _, c := range cmds {
		for _, key := range c.Unknown {
			problems = append(problems, fmt.Sprintf(
				"the %s command sends %q, which none of the tools it dispatches (%s) "+
					"declares — an MCP client would be refused for that key and the CLI "+
					"ignores it, so it works on neither surface and says so on one",
				c.Command, key, strings.Join(c.Tools, ", ")))
		}
	}

	declared := map[string]string{}
	for _, e := range argExemptions {
		declared[e.Tool+"/"+e.Arg] = e.Reason
	}

	for _, a := range args {
		if len(a.SetBy) > 0 {
			// An exemption for something that is in fact reachable is stale,
			// exactly as next door.
			delete(declared, a.Tool+"/"+a.Name)
			continue
		}
		key := a.Tool + "/" + a.Name
		if _, ok := declared[key]; ok {
			delete(declared, key)
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s accepts %q and no CLI command can set it — either add a flag or "+
				"record the gap in apisurface.argExemptions", a.Tool, a.Name))
	}

	for key, reason := range declared {
		problems = append(problems, fmt.Sprintf(
			"the argument exemption for %s is stale — the CLI now sets it, or the "+
				"tool or argument is gone. Reason given was: %s", key, reason))
	}
	sort.Strings(problems)
	return problems
}
