package device

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// AFC is Apple File Conduit, the file protocol a phone's lockdown services
// speak when what they serve is files — crash reports among them. Only the
// read side is here: list, stat, read. Mobium leaves nothing on a device,
// and has no reason to write to one.
//
// Every packet is a 40-byte header — magic, the whole packet's length, the
// header-plus-arguments length, a sequence number and an operation, all
// little-endian — then the operation's arguments, then any payload.
type afcClient struct {
	conn net.Conn
	seq  uint64
}

const afcMagic = "CFA6LPAA"

const (
	afcOpStatus      = 0x01
	afcOpData        = 0x02
	afcOpReadDir     = 0x03
	afcOpGetFileInfo = 0x0a
	afcOpFileOpen    = 0x0d
	afcOpFileOpenRes = 0x0e
	afcOpFileRead    = 0x0f
	afcOpFileClose   = 0x14
)

// afcModeRead is FILE_OPEN's read-only mode.
const afcModeRead = 1

// afcMaxRead is how much one FILE_READ asks for. A crash report is tens of
// kilobytes; a few reads cover it.
const afcMaxRead = 1 << 20

// afcTimeout bounds one request and its answer.
const afcTimeout = 30 * time.Second

func (a *afcClient) request(op uint64, args, payload []byte) (uint64, []byte, error) {
	_ = a.conn.SetDeadline(time.Now().Add(afcTimeout))
	defer func() { _ = a.conn.SetDeadline(time.Time{}) }()

	head := make([]byte, 40)
	copy(head, afcMagic)
	thisLen := uint64(40 + len(args))
	binary.LittleEndian.PutUint64(head[8:], thisLen+uint64(len(payload)))
	binary.LittleEndian.PutUint64(head[16:], thisLen)
	binary.LittleEndian.PutUint64(head[24:], a.seq)
	binary.LittleEndian.PutUint64(head[32:], op)
	a.seq++
	packet := append(append(head, args...), payload...)
	if _, err := a.conn.Write(packet); err != nil {
		return 0, nil, fmt.Errorf("afc: %w", err)
	}

	if _, err := io.ReadFull(a.conn, head); err != nil {
		return 0, nil, fmt.Errorf("afc: %w", err)
	}
	if string(head[:8]) != afcMagic {
		return 0, nil, mobiumerr.New(mobiumerr.DeviceServer, "afc: reply is not an AFC packet")
	}
	total := binary.LittleEndian.Uint64(head[8:])
	replyOp := binary.LittleEndian.Uint64(head[32:])
	if total < 40 || total > 64<<20 {
		return 0, nil, mobiumerr.New(mobiumerr.DeviceServer, "afc: implausible reply length %d", total)
	}
	body := make([]byte, total-40)
	if _, err := io.ReadFull(a.conn, body); err != nil {
		return 0, nil, fmt.Errorf("afc: %w", err)
	}
	if replyOp == afcOpStatus {
		if len(body) < 8 {
			return 0, nil, mobiumerr.New(mobiumerr.DeviceServer, "afc: short status")
		}
		if code := binary.LittleEndian.Uint64(body); code != 0 {
			return replyOp, nil, afcError(code)
		}
	}
	return replyOp, body, nil
}

// afcError names the codes worth naming. The rest are reported as numbers.
func afcError(code uint64) error {
	switch code {
	case 8:
		return mobiumerr.New(mobiumerr.InvalidArgument, "afc: no such file")
	case 10:
		return mobiumerr.New(mobiumerr.DeviceServer, "afc: permission denied")
	}
	return mobiumerr.New(mobiumerr.DeviceServer, "afc: error %d", code)
}

func nulTerminated(s string) []byte { return append([]byte(s), 0) }

// splitNul reads AFC's lists: NUL-terminated strings, back to back.
func splitNul(b []byte) []string {
	var out []string
	for _, part := range bytes.Split(bytes.TrimRight(b, "\x00"), []byte{0}) {
		out = append(out, string(part))
	}
	return out
}

// ReadDir lists a directory, without "." and "..".
func (a *afcClient) ReadDir(path string) ([]string, error) {
	op, body, err := a.request(afcOpReadDir, nulTerminated(path), nil)
	if err != nil {
		return nil, err
	}
	if op != afcOpData {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "afc: read dir answered with operation %d", op)
	}
	var names []string
	for _, n := range splitNul(body) {
		if n != "" && n != "." && n != ".." {
			names = append(names, n)
		}
	}
	return names, nil
}

// afcInfo is what GET_FILE_INFO reports that Mobium reads.
type afcInfo struct {
	Dir   bool
	Size  int64
	MTime time.Time
}

// Stat reports a path's type, size and modification time.
func (a *afcClient) Stat(path string) (afcInfo, error) {
	var info afcInfo
	op, body, err := a.request(afcOpGetFileInfo, nulTerminated(path), nil)
	if err != nil {
		return info, err
	}
	if op != afcOpData {
		return info, mobiumerr.New(mobiumerr.DeviceServer, "afc: stat answered with operation %d", op)
	}
	kv := splitNul(body)
	for i := 0; i+1 < len(kv); i += 2 {
		switch kv[i] {
		case "st_ifmt":
			info.Dir = kv[i+1] == "S_IFDIR"
		case "st_size":
			info.Size, _ = strconv.ParseInt(kv[i+1], 10, 64)
		case "st_mtime":
			if ns, err := strconv.ParseInt(kv[i+1], 10, 64); err == nil {
				info.MTime = time.Unix(0, ns).UTC()
			}
		}
	}
	return info, nil
}

// ReadFile reads a whole file, refusing one larger than limit.
func (a *afcClient) ReadFile(path string, limit int64) ([]byte, error) {
	return a.read(path, limit, false)
}

// ReadPrefix reads at most n bytes from the start of a file — enough for an
// .ips header, which is one line, without fetching the report behind it.
func (a *afcClient) ReadPrefix(path string, n int64) ([]byte, error) {
	return a.read(path, n, true)
}

func (a *afcClient) read(path string, limit int64, prefix bool) ([]byte, error) {
	args := make([]byte, 8)
	binary.LittleEndian.PutUint64(args, afcModeRead)
	op, body, err := a.request(afcOpFileOpen, append(args, nulTerminated(path)...), nil)
	if err != nil {
		return nil, err
	}
	if op != afcOpFileOpenRes || len(body) < 8 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "afc: open answered with operation %d", op)
	}
	handle := body[:8]
	defer func() { _, _, _ = a.request(afcOpFileClose, handle, nil) }()

	var out []byte
	for {
		want := int64(afcMaxRead)
		if prefix {
			want = limit - int64(len(out))
		}
		req := make([]byte, 16)
		copy(req, handle)
		binary.LittleEndian.PutUint64(req[8:], uint64(want))
		op, chunk, err := a.request(afcOpFileRead, req, nil)
		if err != nil {
			return nil, err
		}
		if op != afcOpData || len(chunk) == 0 {
			return out, nil
		}
		out = append(out, chunk...)
		if prefix && int64(len(out)) >= limit {
			return out[:limit], nil
		}
		if int64(len(out)) > limit {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "afc: %s is over %d bytes", path, limit)
		}
	}
}

// joinAFC joins path elements with the phone's separator, whatever the host's.
func joinAFC(dir, name string) string {
	return strings.TrimRight(dir, "/") + "/" + name
}
