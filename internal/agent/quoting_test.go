package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// handQuoted is a quote written around a %s: it lets the text inside close
// the quote and write on as if it were Mobium. An app's text in a message
// goes through %q, which escapes quotes and line breaks (CHALLENGES 378).
var handQuoted = regexp.MustCompile(`\\"%s\\"|“%s”|‘%s’|'%s'`)

func TestHandQuotedIsCaught(t *testing.T) {
	for _, s := range []string{`"no element matches \"%s\""`, "`pressed “%s”`", `"on '%s'"`} {
		if !handQuoted.MatchString(s) {
			t.Errorf("not caught: %s", s)
		}
	}
	for _, s := range []string{`"no element matches %q"`, `"%s is %s"`} {
		if handQuoted.MatchString(s) {
			t.Errorf("caught, but safe: %s", s)
		}
	}
}

// No message in Mobium's source quotes a %s by hand. The app's words reach
// an agent in errors and in text, so a closing quote they carry must not end
// Mobium's quote (ROADMAP, Red teaming: any fence uses a marker an app cannot
// print).
func TestNoMessageQuotesAPercentSByHand(t *testing.T) {
	root := filepath.Join("..", "..")
	var found []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(b), "\n") {
				if handQuoted.MatchString(line) {
					rel, _ := filepath.Rel(root, p)
					found = append(found, rel+":"+strconv.Itoa(i+1))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(found) > 0 {
		t.Errorf("quoted by hand, use %%q: %s", strings.Join(found, ", "))
	}
}
