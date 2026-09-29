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
	return fmt.Sprintf("downloaded %s from %s to %s (%d bytes) — confirmed by %s", v.Name, v.Where, path, v.Bytes, v.Checked)
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
