package plist

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"strconv"
	"strings"
)

// maxXMLDepth bounds nesting, for the same reason the binary decoder does: a
// peer that sends a thousand nested arrays should get an error, not a stack.
const maxXMLDepth = 64

func isXML(data []byte) bool {
	t := bytes.TrimLeft(data, " \t\r\n\ufeff")
	return bytes.HasPrefix(t, []byte("<?xml")) || bytes.HasPrefix(t, []byte("<plist"))
}

// unmarshalXML reads the XML property list format, for the same value types
// the binary decoder returns.
func unmarshalXML(data []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("plist: no <plist> element: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local != "plist" {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: root element is <%s>, not <plist>", se.Name.Local)
			}
			break
		}
	}
	v, end, err := xmlValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if end {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: empty <plist>")
	}
	return v, nil
}

// xmlValue reads the next value element. end reports that the enclosing
// element closed instead, which is how arrays and dictionaries know to stop.
func xmlValue(dec *xml.Decoder, depth int) (v any, end bool, err error) {
	if depth > maxXMLDepth {
		return nil, false, mobiumerr.New(mobiumerr.DeviceServer, "plist: nested too deeply")
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, false, errTruncated
		}
		if err != nil {
			return nil, false, fmt.Errorf("plist: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil, true, nil
		case xml.StartElement:
			v, err := xmlElement(dec, t, depth)
			return v, false, err
		}
	}
}

func xmlElement(dec *xml.Decoder, se xml.StartElement, depth int) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("plist: unterminated <dict>: %w", err)
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return m, nil
			case xml.StartElement:
				if t.Name.Local != "key" {
					return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: <%s> where a <key> belongs", t.Name.Local)
				}
				var key string
				if err := dec.DecodeElement(&key, &t); err != nil {
					return nil, fmt.Errorf("plist: %w", err)
				}
				v, end, err := xmlValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				if end {
					return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: key %q has no value", key)
				}
				m[key] = v
			}
		}
	case "array":
		out := []any{}
		for {
			v, end, err := xmlValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			if end {
				return out, nil
			}
			out = append(out, v)
		}
	case "true", "false":
		if err := dec.Skip(); err != nil {
			return nil, fmt.Errorf("plist: %w", err)
		}
		return se.Name.Local == "true", nil
	}

	var text string
	if err := dec.DecodeElement(&text, &se); err != nil {
		return nil, fmt.Errorf("plist: %w", err)
	}
	switch se.Name.Local {
	case "string":
		return text, nil
	case "integer":
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("plist: integer %q: %w", text, err)
		}
		return n, nil
	case "real":
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, fmt.Errorf("plist: real %q: %w", text, err)
		}
		return f, nil
	case "data":
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
		if err != nil {
			return nil, fmt.Errorf("plist: data: %w", err)
		}
		return b, nil
	}
	return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: <%s> is not a type this protocol uses", se.Name.Local)
}
