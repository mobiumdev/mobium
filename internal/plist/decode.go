package plist

import (
	"encoding/binary"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"math"
	"unicode/utf16"
)

// Unmarshal decodes a property list, binary or XML.
//
// Returns map[string]any, []any, string, []byte, int64, float64, bool or nil.
// Types the protocol does not use — dates, sets, UIDs — are refused by name
// rather than skipped, so a message carrying one is a visible failure instead
// of a silently missing field.
//
// XML is read and never written: usbmuxd and lockdown, which a real iPhone's
// web inspector is reached through, accept a binary request and always answer
// in XML. Measured on macOS 26 against an iPhone on iOS 26.6.2.
func Unmarshal(data []byte) (any, error) {
	if isXML(data) {
		return unmarshalXML(data)
	}
	if len(data) < len(magic)+32 {
		return nil, errTruncated
	}
	if string(data[:len(magic)]) != string(magic) {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: not a binary property list (header %q)", data[:8])
	}

	trailer := data[len(data)-32:]
	d := &decoder{
		data:    data,
		offSize: trailer[6],
		refSize: trailer[7],
		count:   binary.BigEndian.Uint64(trailer[8:]),
		root:    binary.BigEndian.Uint64(trailer[16:]),
		table:   binary.BigEndian.Uint64(trailer[24:]),
	}
	if d.offSize == 0 || d.offSize > 8 || d.refSize == 0 || d.refSize > 8 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: implausible trailer (offset %d, ref %d bytes)",
			d.offSize, d.refSize)
	}
	if d.table+d.count*uint64(d.offSize) > uint64(len(data)) {
		return nil, errTruncated
	}
	if d.root >= d.count {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: root object %d of %d", d.root, d.count)
	}
	return d.object(d.root, 0)
}

type decoder struct {
	data    []byte
	offSize byte
	refSize byte
	count   uint64
	root    uint64
	table   uint64
}

// maxDepth stops a plist whose containers refer to each other from recursing
// until the stack gives out. Nothing legitimate in this protocol is deep.
const maxDepth = 32

func (d *decoder) offsetOf(ref uint64) (uint64, error) {
	if ref >= d.count {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "plist: object reference %d of %d", ref, d.count)
	}
	at := d.table + ref*uint64(d.offSize)
	off := readSized(d.data[at:at+uint64(d.offSize)], d.offSize)
	if off >= uint64(len(d.data)) {
		return 0, errTruncated
	}
	return off, nil
}

func (d *decoder) object(ref uint64, depth int) (any, error) {
	if depth > maxDepth {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: nested more than %d deep", maxDepth)
	}
	off, err := d.offsetOf(ref)
	if err != nil {
		return nil, err
	}
	b := d.data[off]
	tag, info := b>>4, uint64(b&0x0F)
	body := off + 1

	switch tag {
	case tagSimple:
		switch info {
		case 0x0:
			return nil, nil
		case 0x8:
			return false, nil
		case 0x9:
			return true, nil
		}
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: unknown simple value 0x%02x", b)

	case tagInt:
		n := uint64(1) << info
		if body+n > uint64(len(d.data)) {
			return nil, errTruncated
		}
		raw := readSized(d.data[body:body+n], byte(n))
		// Eight-byte integers are signed; shorter ones never are.
		if n == 8 {
			return int64(raw), nil
		}
		return int64(raw), nil

	case tagReal:
		n := uint64(1) << info
		if body+n > uint64(len(d.data)) {
			return nil, errTruncated
		}
		if n == 4 {
			return float64(math.Float32frombits(uint32(readSized(d.data[body:body+n], 4)))), nil
		}
		return math.Float64frombits(readSized(d.data[body:body+n], 8)), nil

	case tagData:
		n, at, err := d.size(info, body)
		if err != nil {
			return nil, err
		}
		if at+n > uint64(len(d.data)) {
			return nil, errTruncated
		}
		return append([]byte{}, d.data[at:at+n]...), nil

	case tagASCII:
		n, at, err := d.size(info, body)
		if err != nil {
			return nil, err
		}
		if at+n > uint64(len(d.data)) {
			return nil, errTruncated
		}
		return string(d.data[at : at+n]), nil

	case tagUnicode:
		n, at, err := d.size(info, body)
		if err != nil {
			return nil, err
		}
		if at+n*2 > uint64(len(d.data)) {
			return nil, errTruncated
		}
		units := make([]uint16, n)
		for i := uint64(0); i < n; i++ {
			units[i] = binary.BigEndian.Uint16(d.data[at+i*2:])
		}
		return string(utf16.Decode(units)), nil

	case tagArray:
		n, at, err := d.size(info, body)
		if err != nil {
			return nil, err
		}
		refs, err := d.refs(at, n)
		if err != nil {
			return nil, err
		}
		out := make([]any, n)
		for i, r := range refs {
			if out[i], err = d.object(r, depth+1); err != nil {
				return nil, err
			}
		}
		return out, nil

	case tagDict:
		n, at, err := d.size(info, body)
		if err != nil {
			return nil, err
		}
		refs, err := d.refs(at, n*2)
		if err != nil {
			return nil, err
		}
		out := make(map[string]any, n)
		for i := uint64(0); i < n; i++ {
			k, err := d.object(refs[i], depth+1)
			if err != nil {
				return nil, err
			}
			key, ok := k.(string)
			if !ok {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: dictionary key is %T, not a string", k)
			}
			if out[key], err = d.object(refs[n+i], depth+1); err != nil {
				return nil, fmt.Errorf("plist: key %q: %w", key, err)
			}
		}
		return out, nil
	}
	return nil, mobiumerr.New(mobiumerr.DeviceServer, "plist: unsupported object type 0x%x — this decoder covers "+
		"dictionaries, strings, integers, reals, booleans, arrays and data", tag)
}

// size reads a container or string length: inline in the low nibble, or an
// integer object immediately after when the nibble is full.
func (d *decoder) size(info, body uint64) (n, at uint64, err error) {
	if info != 0x0F {
		return info, body, nil
	}
	if body >= uint64(len(d.data)) {
		return 0, 0, errTruncated
	}
	b := d.data[body]
	if b>>4 != tagInt {
		return 0, 0, mobiumerr.New(mobiumerr.DeviceServer, "plist: expected an integer length, got 0x%02x", b)
	}
	width := uint64(1) << (b & 0x0F)
	if body+1+width > uint64(len(d.data)) {
		return 0, 0, errTruncated
	}
	return readSized(d.data[body+1:body+1+width], byte(width)), body + 1 + width, nil
}

func (d *decoder) refs(at, n uint64) ([]uint64, error) {
	size := uint64(d.refSize)
	if at+n*size > uint64(len(d.data)) {
		return nil, errTruncated
	}
	out := make([]uint64, n)
	for i := uint64(0); i < n; i++ {
		out[i] = readSized(d.data[at+i*size:at+(i+1)*size], d.refSize)
	}
	return out, nil
}

func readSized(b []byte, size byte) uint64 {
	var v uint64
	for i := 0; i < int(size) && i < len(b); i++ {
		v = v<<8 | uint64(b[i])
	}
	return v
}
