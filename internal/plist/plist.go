// Package plist reads and writes the subset of Apple's binary property list
// format that the WebKit Remote Web Inspector protocol uses.
//
// iOS WebViews are reached over a Unix socket whose framing is four bytes of
// big-endian length followed by a binary plist; the JSON that Mobium actually
// cares about travels inside that envelope. The standard library has no
// plist, and this is the only reason iOS WebView support is not simply a
// matter of reusing the Android code.
//
// It is deliberately not a general-purpose plist library. Dictionaries,
// strings, integers, booleans, arrays and data are what the protocol uses,
// and anything else is refused rather than guessed at. Dates, sets and UIDs
// are absent on purpose. XML is read, never written: a real iPhone's web
// inspector is reached through usbmuxd and lockdown, which answer only in XML
// whatever they are asked in.
package plist

import (
	"encoding/binary"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"math"
	"sort"
	"unicode/utf16"
)

var magic = []byte("bplist00")

// Marker types, in the high nibble of an object's first byte.
const (
	tagSimple  = 0x0 // null, false, true
	tagInt     = 0x1
	tagReal    = 0x2
	tagData    = 0x4
	tagASCII   = 0x5
	tagUnicode = 0x6
	tagArray   = 0xA
	tagDict    = 0xD
)

// Marshal encodes a value as a binary property list.
//
// Accepts map[string]any, []any, string, []byte, bool, and the integer and
// float types. Anything else is an error: a plist that silently drops a value
// is worse than one that refuses to be written.
func Marshal(v any) ([]byte, error) {
	e := &encoder{index: map[any]int{}}
	root, err := e.add(v)
	if err != nil {
		return nil, err
	}
	return e.finish(root)
}

type encoder struct {
	objects [][]byte
	// pending holds containers whose bodies cannot be written until every
	// object exists and the reference width is known. On the encoder rather
	// than at package level: a global here would corrupt one Marshal with
	// another's containers the first time two ran at once.
	pending []pending
	// index deduplicates the hashable scalars. Dictionary keys repeat
	// constantly in this protocol — every message carries
	// WIRConnectionIdentifierKey — and writing each one once keeps the
	// envelope small enough not to think about.
	index map[any]int
}

// add appends the encoded form of v and returns its object reference.
func (e *encoder) add(v any) (int, error) {
	if ref, ok := e.dedup(v); ok {
		return ref, nil
	}
	switch t := v.(type) {
	case nil:
		return e.emit(v, []byte{0x00}), nil
	case bool:
		if t {
			return e.emit(v, []byte{0x09}), nil
		}
		return e.emit(v, []byte{0x08}), nil
	case string:
		return e.emit(v, encodeString(t)), nil
	case []byte:
		return e.emitAnon(append(marker(tagData, len(t)), t...)), nil
	case int:
		return e.emit(v, encodeInt(int64(t))), nil
	case int64:
		return e.emit(v, encodeInt(t)), nil
	case uint64:
		if t > math.MaxInt64 {
			return 0, mobiumerr.New(mobiumerr.DeviceServer, "plist: %d does not fit a signed 64-bit integer", t)
		}
		return e.emit(v, encodeInt(int64(t))), nil
	case float64:
		b := make([]byte, 9)
		b[0] = tagReal<<4 | 3
		binary.BigEndian.PutUint64(b[1:], math.Float64bits(t))
		return e.emit(v, b), nil
	case []any:
		return e.addArray(t)
	case map[string]any:
		return e.addDict(t)
	}
	return 0, mobiumerr.New(mobiumerr.DeviceServer, "plist: cannot encode %T", v)
}

func (e *encoder) addArray(items []any) (int, error) {
	refs := make([]int, len(items))
	for i, item := range items {
		ref, err := e.add(item)
		if err != nil {
			return 0, err
		}
		refs[i] = ref
	}
	// The reference size is not known until every object exists, so the body
	// is written in finish() and this reserves the slot.
	ref := e.emitAnon(nil)
	e.pending = append(e.pending, pending{ref, tagArray, refs, nil})
	return ref, nil
}

func (e *encoder) addDict(m map[string]any) (int, error) {
	// Sorted so the same dictionary always produces the same bytes, which
	// makes a failure reproducible and a test possible.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	keyRefs := make([]int, len(keys))
	valRefs := make([]int, len(keys))
	for i, k := range keys {
		kr, err := e.add(k)
		if err != nil {
			return 0, err
		}
		vr, err := e.add(m[k])
		if err != nil {
			return 0, fmt.Errorf("plist: key %q: %w", k, err)
		}
		keyRefs[i], valRefs[i] = kr, vr
	}
	ref := e.emitAnon(nil)
	e.pending = append(e.pending, pending{ref, tagDict, keyRefs, valRefs})
	return ref, nil
}

// pending is a container whose body cannot be written until the reference
// width is known.
type pending struct {
	at   int
	tag  byte
	keys []int
	vals []int
}

func (e *encoder) dedup(v any) (int, bool) {
	switch v.(type) {
	case string, bool, int, int64, float64:
		ref, ok := e.index[v]
		return ref, ok
	}
	return 0, false
}

func (e *encoder) emit(key any, body []byte) int {
	ref := e.emitAnon(body)
	switch key.(type) {
	case string, bool, int, int64, float64:
		e.index[key] = ref
	}
	return ref
}

func (e *encoder) emitAnon(body []byte) int {
	e.objects = append(e.objects, body)
	return len(e.objects) - 1
}

// finish writes the containers, the offset table and the trailer, once the
// object count is known and therefore so is the reference width.
func (e *encoder) finish(root int) ([]byte, error) {
	refSize := byteWidth(uint64(len(e.objects)))
	for _, p := range e.pending {
		body := marker(p.tag, len(p.keys))
		for _, r := range p.keys {
			body = appendSized(body, uint64(r), refSize)
		}
		for _, r := range p.vals {
			body = appendSized(body, uint64(r), refSize)
		}
		e.objects[p.at] = body
	}

	out := append([]byte{}, magic...)
	offsets := make([]uint64, len(e.objects))
	for i, o := range e.objects {
		offsets[i] = uint64(len(out))
		out = append(out, o...)
	}

	tableStart := uint64(len(out))
	offSize := byteWidth(tableStart)
	for _, off := range offsets {
		out = appendSized(out, off, offSize)
	}

	trailer := make([]byte, 32)
	trailer[6] = offSize
	trailer[7] = refSize
	binary.BigEndian.PutUint64(trailer[8:], uint64(len(e.objects)))
	binary.BigEndian.PutUint64(trailer[16:], uint64(root))
	binary.BigEndian.PutUint64(trailer[24:], tableStart)
	return append(out, trailer...), nil
}

// marker writes an object's type byte, with the count inline when it fits in
// the low nibble and as a following integer when it does not.
func marker(tag byte, count int) []byte {
	if count < 0x0F {
		return []byte{tag<<4 | byte(count)}
	}
	return append([]byte{tag<<4 | 0x0F}, encodeInt(int64(count))...)
}

func encodeInt(v int64) []byte {
	// Negative values are always eight bytes: the format has no sign bit, so
	// a short encoding of -1 would read as a large positive number.
	switch {
	case v < 0:
		b := make([]byte, 9)
		b[0] = tagInt<<4 | 3
		binary.BigEndian.PutUint64(b[1:], uint64(v))
		return b
	case v <= math.MaxUint8:
		return []byte{tagInt << 4, byte(v)}
	case v <= math.MaxUint16:
		b := []byte{tagInt<<4 | 1, 0, 0}
		binary.BigEndian.PutUint16(b[1:], uint16(v))
		return b
	case v <= math.MaxUint32:
		b := []byte{tagInt<<4 | 2, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], uint32(v))
		return b
	default:
		b := make([]byte, 9)
		b[0] = tagInt<<4 | 3
		binary.BigEndian.PutUint64(b[1:], uint64(v))
		return b
	}
}

func encodeString(s string) []byte {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return append(marker(tagASCII, len(s)), s...)
	}
	// UTF-16 big-endian, and the count is in code units rather than runes —
	// an emoji is two. Getting that wrong truncates every string after it.
	units := utf16.Encode([]rune(s))
	body := marker(tagUnicode, len(units))
	for _, u := range units {
		body = append(body, byte(u>>8), byte(u))
	}
	return body
}

// byteWidth is how many bytes are needed to hold n.
func byteWidth(n uint64) byte {
	switch {
	case n <= math.MaxUint8:
		return 1
	case n <= math.MaxUint16:
		return 2
	case n <= math.MaxUint32:
		return 4
	default:
		return 8
	}
}

func appendSized(dst []byte, v uint64, size byte) []byte {
	for i := int(size) - 1; i >= 0; i-- {
		dst = append(dst, byte(v>>(8*i)))
	}
	return dst
}

var errTruncated = mobiumerr.New(mobiumerr.DeviceServer, "plist: truncated")
