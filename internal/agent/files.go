package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// app_upload and app_download: a file between this machine and the folder
// the device keeps downloads in — Android's shared Download folder, an iOS
// app's Documents. See internal/device/files.go for where, and why there.

// TransferView is the result of app_upload, and of app_download with a name.
type TransferView struct {
	Device string `json:"device"`
	// App is the app whose Documents it was, on iOS; absent on Android,
	// which has one Download folder for every app.
	App   string `json:"app,omitempty"`
	Name  string `json:"name"`
	Where string `json:"where"`
	Bytes int64  `json:"bytes"`
	// Checked says how the transfer was confirmed at both ends.
	Checked string `json:"checked"`
	// Path is the file on this machine: what was sent, or where it was saved.
	Path string `json:"path,omitempty"`
	// Data is the file itself, base64, when app_download was given no path —
	// for a caller whose disk is not the daemon's.
	Data string `json:"data,omitempty"`
	// Files and Folder are set by a transfer at a device path: how many files
	// moved, and whether they were a folder.
	Files  int  `json:"files,omitempty"`
	Folder bool `json:"folder,omitempty"`
}

// FilesView is app_download with no name: what the folder holds.
type FilesView struct {
	Device string              `json:"device"`
	App    string              `json:"app,omitempty"`
	Folder string              `json:"folder"`
	Files  []device.DeviceFile `json:"files"`
}

func (h *Handlers) upload(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	if stringArg(args, "device_path") != "" {
		return h.pushPath(ctx, s, args)
	}
	ft, ok := mobiumdriver.AsFileTransfer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapFiles, "move files to and from the device")
	}
	local, name := stringArg(args, "path"), stringArg(args, "name")
	// The file as content, from a caller whose disk is not the daemon's.
	if content := stringArg(args, "content"); content != "" {
		if name == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_upload with content needs its name")
		}
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the file's content is not base64: %w", err)
		}
		dir, err := os.MkdirTemp("", "mobium-upload-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		if err := device.CheckFileName(name); err != nil {
			return nil, err
		}
		local = filepath.Join(dir, name)
		if err := os.WriteFile(local, raw, 0o600); err != nil {
			return nil, err
		}
	}
	if local == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_upload needs the path of a file on this machine")
	}
	if name == "" {
		name = filepath.Base(local)
	}
	app, err := h.filesApp(ctx, s, args)
	if err != nil {
		return nil, err
	}
	t, err := ft.UploadFile(ctx, local, name, app)
	if err != nil {
		return nil, err
	}
	sent := local
	if stringArg(args, "content") != "" {
		sent = name
	}
	return Result(fmt.Sprintf("uploaded %s to %s (%d bytes) — confirmed by %s", sent, t.Where, t.Bytes, t.Checked),
		TransferView{Device: s.dev.Serial, App: app, Name: t.Name, Where: t.Where, Bytes: t.Bytes, Checked: t.Checked,
			Path: stringArg(args, "path")}), nil
}

func (h *Handlers) download(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	if stringArg(args, "device_path") != "" {
		return h.pullPath(ctx, s, args)
	}
	ft, ok := mobiumdriver.AsFileTransfer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapFiles, "move files to and from the device")
	}
	app, err := h.filesApp(ctx, s, args)
	if err != nil {
		return nil, err
	}
	folder := "the Download folder"
	if app != "" {
		folder = app + "'s Documents"
	}

	name := stringArg(args, "name")
	if name == "" {
		files, err := ft.ListFiles(ctx, app)
		if err != nil {
			return nil, err
		}
		lines := []string{fmt.Sprintf("%s holds %d file%s", folder, len(files), plural(len(files)))}
		if len(files) == 0 {
			lines[0] = folder + " is empty"
		}
		for _, f := range files {
			lines = append(lines, "  "+f.String())
		}
		if files == nil {
			files = []device.DeviceFile{}
		}
		return Result(strings.Join(lines, "\n"), FilesView{Device: s.dev.Serial, App: app, Folder: folder, Files: files}), nil
	}

	path := stringArg(args, "path")
	inline := path == ""
	if inline {
		// No path: the file comes back in the answer, which is what an MCP
		// client or a remote caller wants. The CLI always passes a path.
		dir, err := os.MkdirTemp("", "mobium-download-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		path = filepath.Join(dir, "file")
	}
	t, err := ft.DownloadFile(ctx, name, app, path)
	if err != nil {
		return nil, err
	}
	view := TransferView{Device: s.dev.Serial, App: app, Name: t.Name, Where: t.Where, Bytes: t.Bytes, Checked: t.Checked}
	if inline {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		view.Data = base64.StdEncoding.EncodeToString(raw)
		return Result(fmt.Sprintf("downloaded %s from %s (%d bytes, in this answer)", name, t.Where, t.Bytes), view), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	view.Path = abs
	return Result(DownloadSavedMessage(abs, view), view), nil
}

// DownloadSavedMessage is app_download's answer once the file is saved, the
// same whether the daemon saved it or a remote caller did.
func DownloadSavedMessage(path string, v TransferView) string {
	what := fmt.Sprintf("%d bytes", v.Bytes)
	if v.Folder {
		what = fmt.Sprintf("a folder of %d file%s, %d bytes", v.Files, plural(v.Files), v.Bytes)
	}
	return fmt.Sprintf("downloaded %s from %s to %s (%s) — confirmed by %s", v.Name, v.Where, path, what, v.Checked)
}

// filesApp is the app whose Documents a file goes to on iOS: the one named,
// or the one in front. Android has one Download folder for every app, and
// takes no app — one named there is not an error, since a script written
// for both platforms names it on both.
func (h *Handlers) filesApp(ctx context.Context, s *session, args map[string]interface{}) (string, error) {
	if s.backend != BackendWDA {
		return "", nil
	}
	if app := stringArg(args, "app"); app != "" {
		return app, nil
	}
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	if pkg := tree.Package(); pkg != "" && pkg != "com.apple.springboard" {
		return pkg, nil
	}
	return "", mobiumerr.New(mobiumerr.InvalidArgument, "on iOS a file goes to an app's own Documents, and no app "+
		"is in front — name one with app").WithRemedy("pass app, the bundle id whose Documents to use")
}

// pathApp is the app a device path is inside: on Android the one named, or
// none, for a shell path; on iOS the one named or the one in front, whose
// data container holds every path.
func (h *Handlers) pathApp(ctx context.Context, s *session, args map[string]interface{}) (string, error) {
	if s.backend != BackendWDA {
		return stringArg(args, "app"), nil
	}
	return h.filesApp(ctx, s, args)
}

// transferSummary is what moved, for the answer: "a file of 12 bytes" or "a
// folder of 3 files, 4096 bytes".
func transferSummary(t device.Transfer) string {
	if t.Folder {
		return fmt.Sprintf("a folder of %d file%s, %d bytes", t.Files, plural(t.Files), t.Bytes)
	}
	return fmt.Sprintf("%d bytes", t.Bytes)
}

// pushPath is app_upload with device_path: a file or a folder to a path the
// caller names.
func (h *Handlers) pushPath(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	pt, ok := mobiumdriver.AsPathTransfer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapFiles, "move files to a path on the device")
	}
	dp, local := stringArg(args, "device_path"), stringArg(args, "path")
	if content := stringArg(args, "content"); content != "" {
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the file's content is not base64: %w", err)
		}
		dir, err := os.MkdirTemp("", "mobium-upload-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		local = filepath.Join(dir, "file")
		if err := os.WriteFile(local, raw, 0o600); err != nil {
			return nil, err
		}
	}
	if local == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_upload needs the path of a file or a folder on this machine")
	}
	app, err := h.pathApp(ctx, s, args)
	if err != nil {
		return nil, err
	}
	t, err := pt.PushPath(ctx, local, dp, app)
	if err != nil {
		return nil, err
	}
	sent := stringArg(args, "path")
	if sent == "" {
		sent = "the file"
	}
	return Result(fmt.Sprintf("uploaded %s to %s (%s) — confirmed by %s", sent, t.Where, transferSummary(t), t.Checked),
		TransferView{Device: s.dev.Serial, App: app, Name: t.Name, Where: t.Where, Bytes: t.Bytes, Checked: t.Checked,
			Path: stringArg(args, "path"), Files: t.Files, Folder: t.Folder}), nil
}

// pullPath is app_download with device_path: a file or a folder from a path
// the caller names. A folder comes back only to a path on this machine; a
// file with no path comes back in the answer, as app_download's always has.
func (h *Handlers) pullPath(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	pt, ok := mobiumdriver.AsPathTransfer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapFiles, "move files from a path on the device")
	}
	if stringArg(args, "name") != "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_download takes a name in the Download folder or a "+
			"device_path, not both")
	}
	dp := stringArg(args, "device_path")
	app, err := h.pathApp(ctx, s, args)
	if err != nil {
		return nil, err
	}
	local := stringArg(args, "path")
	inline := local == ""
	if inline {
		dir, err := os.MkdirTemp("", "mobium-download-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		local = filepath.Join(dir, "file")
	}
	t, err := pt.PullPath(ctx, dp, app, local)
	if err != nil {
		return nil, err
	}
	view := TransferView{Device: s.dev.Serial, App: app, Name: t.Name, Where: t.Where, Bytes: t.Bytes, Checked: t.Checked,
		Files: t.Files, Folder: t.Folder}
	if inline {
		if t.Folder {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is a folder, which comes back only to a path on "+
				"this machine, not in the answer", dp).WithRemedy("give path: where to put the folder")
		}
		raw, err := os.ReadFile(local)
		if err != nil {
			return nil, err
		}
		view.Data = base64.StdEncoding.EncodeToString(raw)
		return Result(fmt.Sprintf("downloaded %s from %s (%d bytes, in this answer)", t.Name, t.Where, t.Bytes), view), nil
	}
	abs, err := filepath.Abs(local)
	if err != nil {
		abs = local
	}
	view.Path = abs
	return Result(DownloadSavedMessage(abs, view), view), nil
}
