package codegen

import (
	"fmt"
	"strconv"
	"strings"
)

// quote writes s as a string literal in lang. Printable text is kept as it
// is, so a label in another script reads as it does on screen; what a
// literal cannot hold is escaped the way that language reads it.
func quote(lang, s string) string {
	switch lang {
	case "go":
		return strconv.Quote(s)
	case "javascript":
		return cQuote(s, '\'', unicodeEscape)
	case "java":
		// Java reads \u escapes before it reads the program, so a \u000a in a
		// literal ends the line: control characters are octal there.
		return cQuote(s, '"', octalEscape)
	default: // python, dotnet
		return cQuote(s, '"', unicodeEscape)
	}
}

func unicodeEscape(r rune) string { return fmt.Sprintf(`\u%04x`, r) }
func octalEscape(r rune) string   { return fmt.Sprintf(`\%03o`, r) }

func cQuote(s string, q rune, control func(rune) string) string {
	var b strings.Builder
	b.WriteRune(q)
	for _, r := range s {
		switch {
		case r == q || r == '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			b.WriteString(control(r))
		case r == 0x2028 || r == 0x2029:
			// Line and paragraph separators end a line in some of these.
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteRune(q)
	return b.String()
}
