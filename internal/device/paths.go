package device

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Files at a path the caller names, rather than the folder a person's
// downloads go to: app_upload and app_download with device_path. A file or a
// whole folder, each confirmed at the far end by what is there — every file's
// size, and how many there are.
//
// On Android a path is either the shell's, absolute — /sdcard/..., or
// /data/local/tmp/... — or, with an app, inside that app's private data,
// reached the only way Android allows: `run-as`, which works for a debuggable
// build and refuses a release build ("package not debuggable", measured on
// the Pixel 7 AVD). Bytes cross through `adb shell -T`, which has no
// terminal and so carries them unchanged — a 70,000-byte random file hashed
// the same at both ends — and no copy is left in /data/local/tmp on the way.

// transferTimeout bounds one transfer through run-as: a folder goes as one
// stream, which adb's ordinary thirty seconds would cut short.
const transferTimeout = 5 * time.Minute

// Tree is what is at a path, file by file: each file's path relative to the
// root, with its size. One entry, "", for a single file.
type Tree map[string]int64

// Bytes is the total size.
func (t Tree) Bytes() int64 {
	var n int64
	for _, b := range t {
		n += b
	}
	return n
}

// diff names the first difference between what was sent and what arrived, or
// "" when they agree.
func (t Tree) diff(got Tree) string {
	names := make([]string, 0, len(t))
	for n := range t {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		g, ok := got[n]
		label := n
		if label == "" {
			label = "the file"
		}
		if !ok {
			return label + " did not arrive"
		}
		if g != t[n] {
			return fmt.Sprintf("%s arrived as %d bytes of %d", label, g, t[n])
		}
	}
	if len(got) != len(t) {
		return fmt.Sprintf("%d files arrived where %d were sent", len(got), len(t))
	}
	return ""
}

// LocalTree reads what is at a local path.
func LocalTree(p string) (Tree, bool, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, false, mobiumerr.New(mobiumerr.InvalidArgument, "nothing at %s", p)
	}
	if !info.IsDir() {
		return Tree{"": info.Size()}, false, nil
	}
	t := Tree{}
	err = filepath.Walk(p, func(f string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			rel, _ := filepath.Rel(p, f)
			t[filepath.ToSlash(rel)] = fi.Size()
		}
		return nil
	})
	return t, true, err
}

// checked says how a transfer was confirmed: "its size read back on the
// device", or for a folder "each of its 3 files' sizes read back on the device".
func checked(how string, t Tree, dir bool) string {
	if !dir {
		return "its size " + how
	}
	if len(t) == 1 {
		return "its one file's size " + how
	}
	return fmt.Sprintf("each of its %d files' sizes %s", len(t), how)
}

// CheckDevicePath refuses a path that is empty or climbs out with "..", and
// one that is not absolute where it must be.
func CheckDevicePath(p string, absolute bool) error {
	if p == "" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "device_path is empty")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%q climbs out with \"..\" — name the path itself", p)
		}
	}
	if absolute && !strings.HasPrefix(p, "/") {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%q is not an absolute path — without app, a device "+
			"path is the shell's, from the root: /sdcard/... or /data/local/tmp/...", p).
			WithRemedy("give an absolute device_path, or app with a path inside that app's data")
	}
	return nil
}

// runStdin is Run with stdin from r: `adb shell -T`, so nothing translates it.
func (a *ADB) runStdin(ctx context.Context, r io.Reader, rest ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, transferTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.Path, a.args(rest...)...)
	cmd.Stdin = r
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		reason := strings.TrimSpace(errb.String() + " " + out.String())
		return out.Bytes(), fmt.Errorf("adb %s: %w: %s", strings.Join(rest, " "), err, reason)
	}
	if e := strings.TrimSpace(errb.String()); e != "" {
		return out.Bytes(), mobiumerr.New(mobiumerr.DeviceServer, "adb %s: %s", strings.Join(rest, " "), e)
	}
	return out.Bytes(), nil
}

// remoteTree lists what is at a device path, through the shell or run-as.
// prefix is "" or "run-as <pkg>".
func (a *ADB) remoteTree(ctx context.Context, prefix, p string) (Tree, bool, error) {
	kind, err := a.Shell(ctx, strings.TrimSpace(prefix+" stat -c %F "+shellQuote(p)))
	if err != nil {
		return nil, false, err
	}
	switch k := strings.TrimSpace(string(kind)); {
	case strings.Contains(k, "directory"):
	case strings.Contains(k, "regular"):
		out, err := a.Shell(ctx, strings.TrimSpace(prefix+" stat -c %s "+shellQuote(p)))
		if err != nil {
			return nil, false, err
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return nil, false, mobiumerr.New(mobiumerr.DeviceServer, "the size of %s read as %q", p, out)
		}
		return Tree{"": size}, false, nil
	default:
		return nil, false, mobiumerr.New(mobiumerr.InvalidArgument, "%s is a %s, not a file or a folder", p, k)
	}
	out, err := a.Shell(ctx, strings.TrimSpace(prefix+" find "+shellQuote(p)+" -type f -exec stat -c "+
		shellQuote("%s|%n")+" {} +"))
	if err != nil {
		return nil, false, err
	}
	t := Tree{}
	root := strings.TrimSuffix(p, "/") + "/"
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		size, name, ok := strings.Cut(strings.TrimSpace(sc.Text()), "|")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(size, 10, 64)
		t[strings.TrimPrefix(name, root)] = n
	}
	return t, true, nil
}

// missing reports a device path that is not there, from what stat said.
func missing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "No such file")
}

// PushPath copies a local file or folder to an absolute device path, through
// the shell, and confirms every file arrived at its size.
func (a *ADB) PushPath(ctx context.Context, local, remote string) (Transfer, error) {
	if err := CheckDevicePath(remote, true); err != nil {
		return Transfer{}, err
	}
	want, dir, err := LocalTree(local)
	if err != nil {
		return Transfer{}, err
	}
	if _, err := a.Run(ctx, "push", local, remote); err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "pushing to %s: %w", remote, err)
	}
	got, _, err := a.remoteTree(ctx, "", remote)
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "pushed to %s and could not read it back: %w", remote, err)
	}
	if d := want.diff(got); d != "" {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "pushed to %s, and %s", remote, d)
	}
	return Transfer{Name: path.Base(remote), Where: remote, Bytes: want.Bytes(), Files: len(want), Folder: dir,
		Checked: checked("read back on the device", want, dir)}, nil
}

// PullPath copies a device file or folder, through the shell, to a local path
// that does not exist yet, and confirms every file arrived at its size.
func (a *ADB) PullPath(ctx context.Context, remote, local string) (Transfer, error) {
	if err := CheckDevicePath(remote, true); err != nil {
		return Transfer{}, err
	}
	want, dir, err := a.remoteTree(ctx, "", remote)
	if missing(err) {
		return Transfer{}, mobiumerr.New(mobiumerr.NoSuchElement, "there is nothing at %s on the device", remote)
	}
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "reading %s: %w", remote, err)
	}
	if err := freshLocal(local, dir); err != nil {
		return Transfer{}, err
	}
	// adb pull puts a folder inside an existing destination, so it goes to a
	// fresh name beside the destination and is moved into place.
	tmp, err := os.MkdirTemp(filepath.Dir(local), ".mobium-pull-")
	if err != nil {
		return Transfer{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	staged := filepath.Join(tmp, "x")
	if _, err := a.Run(ctx, "pull", remote, staged); err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "pulling %s: %w", remote, err)
	}
	if err := os.Rename(staged, local); err != nil {
		return Transfer{}, err
	}
	return confirmPulled(remote, local, want, dir)
}

// freshLocal makes sure a download lands somewhere new: a folder is never
// merged into one already there, and a file never replaces a folder.
func freshLocal(local string, dir bool) error {
	if info, err := os.Stat(local); err == nil && (dir || info.IsDir()) {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s already exists — a download makes a new one rather "+
			"than mix into it", local).WithRemedy("give path somewhere that does not exist yet")
	}
	return os.MkdirAll(filepath.Dir(local), 0o755)
}

func confirmPulled(remote, local string, want Tree, dir bool) (Transfer, error) {
	got, _, err := LocalTree(local)
	if err != nil {
		return Transfer{}, err
	}
	if d := want.diff(got); d != "" {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "pulled %s, and %s", remote, d)
	}
	return Transfer{Name: path.Base(remote), Where: remote, Bytes: want.Bytes(), Files: len(want), Folder: dir,
		Checked: checked("read back against the device's", want, dir)}, nil
}

// appPath turns a path inside an app's data into the path run-as is given:
// relative to the app's data folder, which is where run-as starts. An
// absolute path must already be inside it.
func appPath(pkg, p string) (string, error) {
	if err := CheckDevicePath(p, false); err != nil {
		return "", err
	}
	if !strings.HasPrefix(p, "/") {
		return strings.TrimPrefix(p, "./"), nil
	}
	for _, root := range []string{"/data/user/0/" + pkg + "/", "/data/data/" + pkg + "/"} {
		if strings.HasPrefix(p, root) {
			return strings.TrimPrefix(p, root), nil
		}
	}
	return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is not inside %s's data — with app, a path is "+
		"relative to the app's data folder, or under /data/user/0/%s", p, pkg, pkg)
}

// runAsReady asks run-as whether it will act for the app, and turns its
// refusal into an answer that says why.
func (a *ADB) runAsReady(ctx context.Context, pkg string) error {
	out, err := a.Shell(ctx, "run-as", shellQuote(pkg), "id")
	reason := string(out)
	if err != nil {
		reason += err.Error()
	}
	switch {
	case strings.Contains(reason, "not debuggable"):
		return mobiumerr.New(mobiumerr.Unsupported, "%s is not debuggable, and an app's private files can be "+
			"reached only by the app itself or by run-as, which Android allows for a debuggable build", pkg).
			WithRemedy("install a debuggable build of it, or use a path in shared storage, such as /sdcard/...")
	case strings.Contains(reason, "unknown package"):
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this device", pkg)
	case err != nil:
		return mobiumerr.New(mobiumerr.DeviceServer, "run-as %s: %w", pkg, err)
	}
	return nil
}

// PushAppPath copies a local file or folder into an app's private data, and
// confirms every file arrived at its size. A folder goes as one tar stream.
func (a *ADB) PushAppPath(ctx context.Context, pkg, local, p string) (Transfer, error) {
	rel, err := appPath(pkg, p)
	if err != nil {
		return Transfer{}, err
	}
	want, dir, err := LocalTree(local)
	if err != nil {
		return Transfer{}, err
	}
	if err := a.runAsReady(ctx, pkg); err != nil {
		return Transfer{}, err
	}
	q := shellQuote(rel)
	if dir {
		var buf bytes.Buffer
		if err := tarFolder(local, &buf); err != nil {
			return Transfer{}, err
		}
		script := "mkdir -p " + q + " && tar -xf - -C " + q
		if _, err := a.runStdin(ctx, &buf, "shell", "-T", "run-as", shellQuote(pkg), "sh", "-c", shellQuote(script)); err != nil {
			return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "unpacking into %s's %s: %w", pkg, rel, err)
		}
	} else {
		f, err := os.Open(local)
		if err != nil {
			return Transfer{}, err
		}
		defer func() { _ = f.Close() }()
		script := "cat > " + q
		if d := path.Dir(rel); d != "." {
			script = "mkdir -p " + shellQuote(d) + " && " + script
		}
		if _, err := a.runStdin(ctx, f, "shell", "-T", "run-as", shellQuote(pkg), "sh", "-c", shellQuote(script)); err != nil {
			return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "writing %s's %s: %w", pkg, rel, err)
		}
	}
	got, _, err := a.remoteTree(ctx, "run-as "+shellQuote(pkg), rel)
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "wrote %s's %s and could not read it back: %w", pkg, rel, err)
	}
	if d := want.diff(got); d != "" {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "wrote %s's %s, and %s", pkg, rel, d)
	}
	return Transfer{Name: path.Base(rel), Where: pkg + " data/" + rel, Bytes: want.Bytes(), Files: len(want), Folder: dir,
		Checked: checked("read back in the app's data", want, dir)}, nil
}

// PullAppPath copies a file or folder out of an app's private data to a local
// path that does not exist yet. A folder comes as one tar stream.
func (a *ADB) PullAppPath(ctx context.Context, pkg, p, local string) (Transfer, error) {
	rel, err := appPath(pkg, p)
	if err != nil {
		return Transfer{}, err
	}
	if err := a.runAsReady(ctx, pkg); err != nil {
		return Transfer{}, err
	}
	prefix := "run-as " + shellQuote(pkg)
	want, dir, err := a.remoteTree(ctx, prefix, rel)
	if missing(err) {
		return Transfer{}, mobiumerr.New(mobiumerr.NoSuchElement, "there is nothing at %s in %s's data", rel, pkg)
	}
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "reading %s's %s: %w", pkg, rel, err)
	}
	if err := freshLocal(local, dir); err != nil {
		return Transfer{}, err
	}
	if dir {
		// One string: given several arguments, adb exec-out escapes each
		// itself, and quoting them too made the package name 'pkg', quotes
		// and all — run-as answered "unknown package", on stdout, where
		// exec-out puts errors too.
		out, err := a.ExecOut(ctx, "run-as "+shellQuote(pkg)+" tar -cf - -C "+shellQuote(rel)+" .")
		if err != nil {
			return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "packing %s's %s: %w", pkg, rel, err)
		}
		if err := untarInto(bytes.NewReader(out), local); err != nil {
			return Transfer{}, err
		}
	} else {
		out, err := a.ExecOut(ctx, "run-as "+shellQuote(pkg)+" cat "+shellQuote(rel))
		if err != nil {
			return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "reading %s's %s: %w", pkg, rel, err)
		}
		if err := os.WriteFile(local, out, 0o644); err != nil {
			return Transfer{}, err
		}
	}
	t, err := confirmPulled(rel, local, want, dir)
	if err != nil {
		return Transfer{}, err
	}
	t.Where = pkg + " data/" + rel
	return t, nil
}

// tarFolder writes a folder's files as a tar stream, paths relative to it.
func tarFolder(root string, w io.Writer) error {
	tw := tar.NewWriter(w)
	err := filepath.Walk(root, func(f string, fi os.FileInfo, err error) error {
		if err != nil || f == root {
			return err
		}
		rel, _ := filepath.Rel(root, f)
		h, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if fi.IsDir() {
			h.Name += "/"
		}
		h.Uname, h.Gname, h.Uid, h.Gid = "", "", 0, 0
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(f)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		_, err = io.Copy(tw, in)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// untarInto unpacks a tar stream into a new folder, refusing any entry that
// would land outside it.
func untarInto(r io.Reader, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return mobiumerr.New(mobiumerr.DeviceServer, "reading the folder the device sent: %w", err)
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." {
			continue
		}
		if strings.HasPrefix(name, "../") || name == ".." || path.IsAbs(name) {
			return mobiumerr.New(mobiumerr.DeviceServer, "the folder the device sent names %q, outside it", h.Name)
		}
		out := filepath.Join(dst, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

// containerPath turns a path inside an iOS app's data container into a
// path on this Mac, for a simulator, refusing one that climbs out of it.
func (s *Simctl) containerPath(ctx context.Context, bundleID, p string) (string, error) {
	if err := CheckDevicePath(p, false); err != nil {
		return "", err
	}
	docs, err := s.documents(ctx, bundleID)
	if err != nil {
		return "", err
	}
	root := filepath.Dir(docs)
	full := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%q is outside %s's data container", p, bundleID)
	}
	return full, nil
}

// PushPath copies a local file or folder into an app's data container on a
// simulator — Documents/..., Library/..., tmp/... — and confirms every file.
func (s *Simctl) PushPath(ctx context.Context, bundleID, local, p string) (Transfer, error) {
	want, dir, err := LocalTree(local)
	if err != nil {
		return Transfer{}, err
	}
	dst, err := s.containerPath(ctx, bundleID, p)
	if err != nil {
		return Transfer{}, err
	}
	if err := copyTree(local, dst, dir); err != nil {
		return Transfer{}, err
	}
	got, _, err := LocalTree(dst)
	if err != nil {
		return Transfer{}, err
	}
	if d := want.diff(got); d != "" {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "copied into %s's %s, and %s", bundleID, p, d)
	}
	return Transfer{Name: path.Base(p), Where: bundleID + " data/" + strings.TrimPrefix(p, "/"), Bytes: want.Bytes(),
		Files: len(want), Folder: dir, Checked: checked("read back in the app's container", want, dir)}, nil
}

// PullPath copies a file or folder out of an app's data container on a
// simulator, to a local path that does not exist yet.
func (s *Simctl) PullPath(ctx context.Context, bundleID, p, local string) (Transfer, error) {
	src, err := s.containerPath(ctx, bundleID, p)
	if err != nil {
		return Transfer{}, err
	}
	want, dir, err := LocalTree(src)
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.NoSuchElement, "there is nothing at %s in %s's data container", p, bundleID)
	}
	if err := freshLocal(local, dir); err != nil {
		return Transfer{}, err
	}
	if err := copyTree(src, local, dir); err != nil {
		return Transfer{}, err
	}
	t, err := confirmPulled(p, local, want, dir)
	if err != nil {
		return Transfer{}, err
	}
	t.Where = bundleID + " data/" + strings.TrimPrefix(p, "/")
	return t, nil
}

// copyTree copies a file, or a folder's files, from src to dst on this Mac.
func copyTree(src, dst string, dir bool) error {
	if !dir {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		_, err := copyFile(src, dst)
		return err
	}
	return filepath.Walk(src, func(f string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, f)
		out := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		_, err = copyFile(f, out)
		return err
	})
}

// phoneTree lists what is at a path in an app's data container on a phone:
// the path's own entry, from its folder's listing, and for a folder every
// file under it — `devicectl device info files` lists a folder recursively,
// each path relative to it.
//
// On a locked phone the listing comes back empty, with no error — measured on
// the iPhone 15 Plus — so "nothing there" says the lock may be why.
func (d *Devicectl) phoneTree(ctx context.Context, bundleID, p string) (Tree, bool, error) {
	list := func(sub string) ([]phoneFile, error) {
		args := append([]string{"device", "info", "files"}, d.container(bundleID)...)
		if sub != "" && sub != "." {
			args = append(args, "--subdirectory", sub)
		}
		raw, err := d.run(ctx, args...)
		if err != nil {
			return nil, err
		}
		var res struct {
			Files []phoneFile `json:"files"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "devicectl's file listing is unreadable: %w", err)
		}
		return res.Files, nil
	}
	nothing := mobiumerr.New(mobiumerr.NoSuchElement, "there is nothing at %s in %s's data container — or the "+
		"phone is locked, which lists every folder as empty", p, bundleID).
		WithRemedy("unlock the phone, then app_download again")
	parent, base := path.Dir(p), path.Base(p)
	entries, err := list(parent)
	if err != nil {
		return nil, false, err
	}
	for _, f := range entries {
		if f.RelativePath != base {
			continue
		}
		if !f.Resources.IsDirectory {
			return Tree{"": f.Metadata.Size}, false, nil
		}
		inside, err := list(p)
		if err != nil {
			return nil, false, err
		}
		t := Tree{}
		for _, g := range inside {
			if !g.Resources.IsDirectory {
				t[g.RelativePath] = g.Metadata.Size
			}
		}
		return t, true, nil
	}
	return nil, false, nothing
}

// PushPath copies a local file or folder into an app's data container on a
// phone. It goes from a fresh copy stamped now, because `copy to` skips a
// file whose size and time match the phone's, and is confirmed by listing
// what arrived. `copy to` makes the folders on the way, and takes a folder
// whole — both measured on the iPhone 15 Plus.
func (d *Devicectl) PushPath(ctx context.Context, bundleID, local, p string) (Transfer, error) {
	if err := CheckDevicePath(p, false); err != nil {
		return Transfer{}, err
	}
	p = strings.TrimPrefix(p, "/")
	want, dir, err := LocalTree(local)
	if err != nil {
		return Transfer{}, err
	}
	if err := d.requireApp(ctx, bundleID); err != nil {
		return Transfer{}, err
	}
	tmp, err := os.MkdirTemp("", "mobium-phone-")
	if err != nil {
		return Transfer{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	fresh := filepath.Join(tmp, "x")
	if err := copyTree(local, fresh, dir); err != nil {
		return Transfer{}, err
	}
	args := append([]string{"device", "copy", "to"}, d.container(bundleID)...)
	if _, err := d.run(ctx, append(args, "--source", fresh, "--destination", p)...); err != nil {
		return Transfer{}, err
	}
	got, _, err := d.phoneTree(ctx, bundleID, p)
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "copied to %s's %s and could not list it back: %w", bundleID, p, err)
	}
	if df := want.diff(got); df != "" {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "copied to %s's %s, and %s", bundleID, p, df)
	}
	return Transfer{Name: path.Base(p), Where: bundleID + " data/" + p, Bytes: want.Bytes(), Files: len(want), Folder: dir,
		Checked: checked("listed back from the phone", want, dir)}, nil
}

// PullPath copies a file or folder out of an app's data container on a phone
// to a local path that does not exist yet.
func (d *Devicectl) PullPath(ctx context.Context, bundleID, p, local string) (Transfer, error) {
	if err := CheckDevicePath(p, false); err != nil {
		return Transfer{}, err
	}
	p = strings.TrimPrefix(p, "/")
	if err := d.requireApp(ctx, bundleID); err != nil {
		return Transfer{}, err
	}
	want, dir, err := d.phoneTree(ctx, bundleID, p)
	if err != nil {
		return Transfer{}, err
	}
	if err := freshLocal(local, dir); err != nil {
		return Transfer{}, err
	}
	args := append([]string{"device", "copy", "from"}, d.container(bundleID)...)
	if _, err := d.run(ctx, append(args, "--source", p, "--destination", local)...); err != nil {
		return Transfer{}, err
	}
	t, err := confirmPulled(p, local, want, dir)
	if err != nil {
		return Transfer{}, err
	}
	t.Where = bundleID + " data/" + p
	return t, nil
}
