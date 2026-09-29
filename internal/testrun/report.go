package testrun

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Line is the list reporter's line for one result.
func Line(r Result) string {
	mark := map[Status]string{Passed: "ok   ", Failed: "FAIL ", Flaky: "flaky"}[r.Status]
	where := r.Project
	if r.Device != "" && r.Device != r.Project {
		where += " · " + r.Device
	}
	s := fmt.Sprintf("  %s [%s] %s (%s)", mark, where, r.Title, r.Duration.Round(100*time.Millisecond))
	if r.Status == Flaky {
		s += fmt.Sprintf(" — passed on attempt %d", r.Attempts)
	}
	if r.Status == Failed {
		for _, f := range r.all() {
			s += "\n        " + failureLine(f)
		}
	}
	return s
}

// all is every failure of the attempt reported.
func (r Result) all() []*Failure {
	if len(r.Failures) > 0 {
		return r.Failures
	}
	if r.Failure != nil {
		return []*Failure{r.Failure}
	}
	return nil
}

func failureLine(f *Failure) string {
	where := "before its steps"
	if f.Step > 0 {
		where = fmt.Sprintf("step %d", f.Step)
		if f.StepName != "" {
			where += " (" + f.StepName + ")"
		}
	}
	s := fmt.Sprintf("%s: [%s] %s", where, f.Code, f.Message)
	if f.Description != "" {
		s = f.Description + " — " + s
	}
	if f.Soft {
		s = "soft, carried on: " + s
	}
	return s
}

// SummaryLine is the run's last line.
func SummaryLine(s *Summary) string {
	p, f, k := s.Counts()
	parts := []string{fmt.Sprintf("%d passed", p)}
	if k > 0 {
		parts = append(parts, fmt.Sprintf("%d flaky", k))
	}
	if f > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", f))
	}
	return fmt.Sprintf("%s (%s)", strings.Join(parts, ", "), s.Duration.Round(100*time.Millisecond))
}

// WriteJSON writes results.json.
func WriteJSON(dir string, s *Summary) (string, error) {
	p := filepath.Join(dir, "results.json")
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return p, writeFile(p, b)
}

// WriteJUnit writes junit.xml: a suite per project and file, a case per
// test. JUnit has no "flaky", so a flaky test passes and says so in a
// property, where CI systems that read properties show it.
func WriteJUnit(dir string, s *Summary) (string, error) {
	type failure struct {
		Message string `xml:"message,attr"`
		Type    string `xml:"type,attr"`
		Text    string `xml:",chardata"`
	}
	type property struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	}
	type properties struct {
		Property []property `xml:"property"`
	}
	type testcase struct {
		Name       string      `xml:"name,attr"`
		Classname  string      `xml:"classname,attr"`
		Time       string      `xml:"time,attr"`
		Properties *properties `xml:"properties,omitempty"`
		Failure    *failure    `xml:"failure,omitempty"`
	}
	type suite struct {
		Name     string     `xml:"name,attr"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Time     string     `xml:"time,attr"`
		Cases    []testcase `xml:"testcase"`
	}
	type suites struct {
		XMLName  xml.Name `xml:"testsuites"`
		Name     string   `xml:"name,attr"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Time     string   `xml:"time,attr"`
		Suites   []suite  `xml:"testsuite"`
	}
	secs := func(d time.Duration) string { return fmt.Sprintf("%.3f", d.Seconds()) }
	byKey := map[string]*suite{}
	var keys []string
	for _, r := range s.Results {
		k := r.Project + " › " + filepath.Base(r.File)
		st, ok := byKey[k]
		if !ok {
			st = &suite{Name: k}
			byKey[k] = st
			keys = append(keys, k)
		}
		c := testcase{Name: r.Test, Classname: r.Project + "." + filepath.Base(r.File), Time: secs(r.Duration)}
		if r.Status == Flaky {
			c.Properties = &properties{[]property{{Name: "flaky", Value: fmt.Sprintf("passed on attempt %d", r.Attempts)}}}
		}
		if r.Status == Failed && r.Failure != nil {
			var lines []string
			for _, f := range r.all() {
				lines = append(lines, failureLine(f))
			}
			c.Failure = &failure{Message: r.Failure.Message, Type: r.Failure.Code, Text: strings.Join(lines, "\n")}
			st.Failures++
		}
		st.Tests++
		st.Cases = append(st.Cases, c)
	}
	sort.Strings(keys)
	out := suites{Name: "mobium test", Time: secs(s.Duration)}
	for _, k := range keys {
		st := byKey[k]
		var d time.Duration
		for _, r := range s.Results {
			if r.Project+" › "+filepath.Base(r.File) == k {
				d += r.Duration
			}
		}
		st.Time = secs(d)
		out.Tests += st.Tests
		out.Failures += st.Failures
		out.Suites = append(out.Suites, *st)
	}
	b, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "junit.xml")
	return p, writeFile(p, append([]byte(xml.Header), b...))
}

// WriteHTML writes index.html: one page, no scripts from anywhere, the
// screenshots beside it in artifacts/.
func WriteHTML(dir string, s *Summary) (string, error) {
	var b strings.Builder
	p, f, k := s.Counts()
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Mobium test report</title>
<style>
:root{--bg:#fff;--fg:#1d1d1f;--muted:#6e6e73;--line:#e5e5ea;--ok:#1a7f37;--bad:#c62828;--warn:#9a6700;--code:#f5f5f7}
@media (prefers-color-scheme:dark){:root{--bg:#161617;--fg:#f5f5f7;--muted:#a1a1a6;--line:#2c2c2e;--ok:#3fb950;--bad:#f85149;--warn:#d29922;--code:#1f1f21}}
body{margin:0;padding:24px 16px;background:var(--bg);color:var(--fg);font:15px/1.5 -apple-system,system-ui,sans-serif}
main{max-width:980px;margin:0 auto}h1{font-size:22px;margin:0 0 4px}.muted{color:var(--muted)}
.counts span{margin-right:16px;font-weight:600}.passed{color:var(--ok)}.failed{color:var(--bad)}.flaky{color:var(--warn)}
details{border-top:1px solid var(--line);padding:10px 0}summary{cursor:pointer;display:flex;gap:10px;flex-wrap:wrap}
.tag{font-size:12px;border:1px solid var(--line);border-radius:10px;padding:0 8px}
pre{background:var(--code);padding:10px;overflow:auto;font-size:12px;border-radius:6px;white-space:pre-wrap}
img{max-width:320px;width:100%;border:1px solid var(--line);border-radius:8px}
</style></head><body><main>`)
	fmt.Fprintf(&b, "<h1>Mobium test report</h1><p class=\"muted\">%s · %s</p>",
		html.EscapeString(s.Started.Format("2006-01-02 15:04:05")), s.Duration.Round(100*time.Millisecond))
	fmt.Fprintf(&b, `<p class="counts"><span class="passed">%d passed</span><span class="flaky">%d flaky</span><span class="failed">%d failed</span></p>`, p, k, f)
	b.WriteString(`<p class="muted">A failure keeps a screenshot of the device. On a real phone that is a picture of somebody's screen; run with --no-screenshots to keep none.</p>`)
	for _, r := range s.Results {
		open := ""
		if r.Status != Passed {
			open = " open"
		}
		fmt.Fprintf(&b, `<details%s><summary><strong class="%s">%s</strong><span>%s</span><span class="tag">%s</span><span class="muted">%s</span></summary>`,
			open, r.Status, r.Status, html.EscapeString(r.Title), html.EscapeString(r.Project), r.Duration.Round(100*time.Millisecond))
		for i, f := range r.all() {
			label := "Failed"
			if r.Status == Flaky {
				label = fmt.Sprintf("Failed first, passed on attempt %d", r.Attempts)
			}
			if len(r.Failures) > 1 {
				label += fmt.Sprintf(" — %d of %d", i+1, len(r.Failures))
			}
			fmt.Fprintf(&b, "<p>%s</p><pre>%s</pre>", label, html.EscapeString(failureLine(f)))
			if f.Screenshot != "" {
				fmt.Fprintf(&b, `<p><img src="%s" alt="The screen when it failed"></p>`, html.EscapeString(f.Screenshot))
			}
			if f.Map != "" {
				fmt.Fprintf(&b, "<p class=\"muted\">The map when it failed</p><pre>%s</pre>", html.EscapeString(f.Map))
			}
		}
		b.WriteString("</details>")
	}
	b.WriteString("</main></body></html>\n")
	path := filepath.Join(dir, "index.html")
	return path, writeFile(path, []byte(b.String()))
}

func writeFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
