package uitree

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// RedactSource hides the contents of every password field in a raw
// hierarchy, as the device server sent it, and reports how many it hid.
//
// The raw source is the one path out of Mobium that skips the parsers, so it
// is the one path Redact does not already guard. Android puts a password's
// typed value in the node's `text` (CHALLENGES 43), so an unredacted source
// prints it. The fields are recognized exactly as the parsers recognize them —
// `password="true"` on Android, `XCUIElementTypeSecureTextField` on iOS — and
// the value is replaced by one bullet per character, which keeps the length,
// as Redact does.
//
// Only the start tags of password fields are rewritten; every other byte is
// the server's, so the answer stays the source rather than a re-rendering of
// it. A value that is already all bullets — React Native's, on both
// platforms — is left as it is and not counted, and so is a placeholder: an
// empty field reports its placeholder where the text goes, which is not a
// secret, and masking it would say the field holds something.
func RedactSource(raw []byte) ([]byte, int, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	type patch struct {
		from, to int64
		tag      []byte
	}
	var patches []patch
	for {
		start := dec.InputOffset()
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		el, ok := tok.(xml.StartElement)
		if !ok || !passwordElement(el) {
			continue
		}
		end := dec.InputOffset()
		if showingPlaceholder(el) {
			continue
		}
		changed := false
		for i, a := range el.Attr {
			if (a.Name.Local == "text" || a.Name.Local == "value") && hideable(a.Value) {
				el.Attr[i].Value = strings.Repeat("•", len([]rune(a.Value)))
				changed = true
			}
		}
		if !changed {
			continue
		}
		selfClosing := bytes.HasSuffix(bytes.TrimRight(raw[start:end], " \t\r\n"), []byte("/>"))
		patches = append(patches, patch{start, end, renderStart(el, selfClosing)})
	}

	if len(patches) == 0 {
		return raw, 0, nil
	}
	var out bytes.Buffer
	var at int64
	for _, p := range patches {
		out.Write(raw[at:p.from])
		out.Write(p.tag)
		at = p.to
	}
	out.Write(raw[at:])
	return out.Bytes(), len(patches), nil
}

// passwordElement is the parsers' test for a password field, on the raw
// element: parse.go reads the attribute, parse_xcui.go the element type.
func passwordElement(el xml.StartElement) bool {
	if el.Name.Local == "XCUIElementTypeSecureTextField" {
		return true
	}
	for _, a := range el.Attr {
		switch {
		case a.Name.Local == "password" && a.Value == "true":
			return true
		case a.Name.Local == "type" && a.Value == "XCUIElementTypeSecureTextField":
			return true
		}
	}
	return false
}

// showingPlaceholder reports whether an element says it is empty and showing
// its placeholder: Android's showing-hint, or on iOS a value that is the
// placeholder, which WebDriverAgent sends in plain text.
func showingPlaceholder(el xml.StartElement) bool {
	var value, placeholder string
	for _, a := range el.Attr {
		switch a.Name.Local {
		case "showing-hint":
			if a.Value == "true" {
				return true
			}
		case "value":
			value = a.Value
		case "placeholderValue":
			placeholder = a.Value
		}
	}
	return placeholder != "" && value == placeholder
}

// hideable reports whether a value says anything a bullet would not.
func hideable(v string) bool {
	return strings.Trim(v, "•● ") != ""
}

// renderStart writes a start tag back out, attributes in their order.
func renderStart(el xml.StartElement, selfClosing bool) []byte {
	var b bytes.Buffer
	b.WriteByte('<')
	b.WriteString(qualified(el.Name))
	for _, a := range el.Attr {
		b.WriteByte(' ')
		b.WriteString(qualified(a.Name))
		b.WriteString(`="`)
		_ = xml.EscapeText(&b, []byte(a.Value)) // a bytes.Buffer does not fail
		b.WriteByte('"')
	}
	if selfClosing {
		b.WriteString(" />")
	} else {
		b.WriteByte('>')
	}
	return b.Bytes()
}

func qualified(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}
