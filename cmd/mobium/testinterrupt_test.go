package main

import (
	"testing"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/testrun"
)

// Every project connection a run opens is closed once: by the run, or —
// interrupted — by closeOpen, which closes those still open and never one
// twice. An interrupted run had left its daemons running (CHALLENGES 199).
func TestAnInterruptedRunClosesWhatIsOpen(t *testing.T) {
	closed := map[string]int{}
	connect := func(p testrun.Project) (testrun.Caller, func(), error) {
		call := func(string, map[string]interface{}, ...map[string]interface{}) (*agent.ToolsCallResult, error) {
			return nil, nil
		}
		return call, func() { closed[p.Name]++ }, nil
	}
	wrapped, closeOpen := closedOnExit(connect)
	_, closeA, _ := wrapped(testrun.Project{Name: "a"})
	_, closeB, _ := wrapped(testrun.Project{Name: "b"})
	closeA()
	closeOpen()
	closeB()
	closeA()
	closeOpen()
	if closed["a"] != 1 || closed["b"] != 1 {
		t.Errorf("closed %v — want each once", closed)
	}
}
