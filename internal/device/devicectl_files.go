package device

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Files on a real iPhone: an app's Documents folder, as on a simulator,
// reached through CoreDevice's file service (`devicectl device copy` and
// `device info files`). Two things about it, measured on the iPhone 15 Plus
// on iOS 26.6.2, decide how it is used:
//
//   - `copy to` skips a file whose size and modification time match the
//     phone's copy, and reports success: a changed file of the same size,
//     stamped with the same time, left the old content on the phone, exit 0.
//     So an upload goes from a fresh copy stamped now, and is confirmed by
//     reading its bytes back, never by its size.
//   - There is no delete. A directory copied with --remove-existing-content
//     empties the destination, which is the whole folder, not one file; so
//     nothing here removes a file, and an upload replaces one by name.

// phoneDocuments is the folder in an app's data container a transfer uses.
const phoneDocuments = "Documents"

// phoneFile mirrors one entry of `devicectl device info files`.
type phoneFile struct {
	RelativePath string `json:"relativePath"`
	Metadata     struct {
		Size        int64  `json:"size"`
		LastModDate string `json:"lastModDate"`
	} `json:"metadata"`
	Resources struct {
		IsDirectory bool `json:"isDirectory"`
	} `json:"resources"`
}

func (d *Devicectl) container(bundleID string) []string {
	return []string{"--device", d.Phone.UDID, "--domain-type", "appDataContainer", "--domain-identifier", bundleID}
}

// UploadFile puts a local file in an app's Documents on the phone, and reads
// it back to confirm the phone holds these bytes.
func (d *Devicectl) UploadFile(ctx context.Context, local, name, bundleID string) (Transfer, error) {
	if err := CheckFileName(name); err != nil {
		return Transfer{}, err
	}
	want, err := os.ReadFile(local)
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.InvalidArgument, "no file at %s to upload", local)
	}
	if err := d.requireApp(ctx, bundleID); err != nil {
		return Transfer{}, err
	}
	tmp, err := os.MkdirTemp("", "mobium-phone-")
	if err != nil {
		return Transfer{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	fresh, back := filepath.Join(tmp, name), filepath.Join(tmp, "back")
	remote := path.Join(phoneDocuments, name)

	// Twice at most: a copy stamped in the same second as the phone's, and
	// the same size, is skipped, and a second later it is not.
	for attempt := 1; ; attempt++ {
		if err := os.WriteFile(fresh, want, 0o600); err != nil {
			return Transfer{}, err
		}
		args := append([]string{"device", "copy", "to"}, d.container(bundleID)...)
		if _, err := d.run(ctx, append(args, "--source", fresh, "--destination", remote)...); err != nil {
			return Transfer{}, err
		}
		_ = os.Remove(back)
		args = append([]string{"device", "copy", "from"}, d.container(bundleID)...)
		if _, err := d.run(ctx, append(args, "--source", remote, "--destination", back)...); err != nil {
			return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "copied %s to the phone and could not read it back: %w", name, err)
		}
		got, err := os.ReadFile(back)
		if err == nil && sha256.Sum256(got) == sha256.Sum256(want) {
			break
		}
		if attempt == 2 {
			return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "copied %s to the phone, and what it holds is "+
				"not what was sent (%d bytes back of %d)", name, len(got), len(want))
		}
		time.Sleep(1100 * time.Millisecond)
	}
	return Transfer{Name: name, Where: bundleID + " Documents/" + name, Bytes: int64(len(want)),
		Checked: "its bytes read back from the phone and compared"}, nil
}

// DownloadFile copies a file from an app's Documents on the phone to a local
// path.
func (d *Devicectl) DownloadFile(ctx context.Context, name, bundleID, local string) (Transfer, error) {
	if err := CheckFileName(name); err != nil {
		return Transfer{}, err
	}
	files, err := d.ListFiles(ctx, bundleID)
	if err != nil {
		return Transfer{}, err
	}
	var size int64 = -1
	for _, f := range files {
		if f.Name == name {
			size = f.Bytes
		}
	}
	if size < 0 {
		return Transfer{}, mobiumerr.New(mobiumerr.NoSuchElement, "no %s in %s's Documents", name, bundleID).
			WithRemedy("app_download with no name lists what is there")
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return Transfer{}, err
	}
	args := append([]string{"device", "copy", "from"}, d.container(bundleID)...)
	if _, err := d.run(ctx, append(args, "--source", path.Join(phoneDocuments, name), "--destination", local)...); err != nil {
		return Transfer{}, err
	}
	info, err := os.Stat(local)
	if err != nil || info.Size() != size {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "copied %s from the phone, and %d of its %d bytes arrived",
			name, sizeOf(info), size)
	}
	return Transfer{Name: name, Where: bundleID + " Documents/" + name, Bytes: size,
		Checked: "the copy's size read back against the phone's"}, nil
}

// ListFiles lists the files in an app's Documents on the phone.
func (d *Devicectl) ListFiles(ctx context.Context, bundleID string) ([]DeviceFile, error) {
	if err := d.requireApp(ctx, bundleID); err != nil {
		return nil, err
	}
	args := append([]string{"device", "info", "files"}, d.container(bundleID)...)
	raw, err := d.run(ctx, append(args, "--subdirectory", phoneDocuments)...)
	if err != nil {
		return nil, err
	}
	return parsePhoneFiles(raw)
}

// parsePhoneFiles reads a listing: the regular files directly in the folder.
func parsePhoneFiles(raw json.RawMessage) ([]DeviceFile, error) {
	var res struct {
		Files []phoneFile `json:"files"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "devicectl's file listing is unreadable: %w", err)
	}
	var files []DeviceFile
	for _, f := range res.Files {
		if f.Resources.IsDirectory || strings.Contains(f.RelativePath, "/") {
			continue
		}
		mod, _ := time.Parse(time.RFC3339, f.Metadata.LastModDate)
		files = append(files, DeviceFile{Name: f.RelativePath, Bytes: f.Metadata.Size, Modified: mod.UTC()})
	}
	sortFiles(files)
	return files, nil
}

// requireApp refuses an app that is not on the phone. devicectl says only
// that it "failed to get a list of files on the remote device", which names
// neither the app nor the reason.
func (d *Devicectl) requireApp(ctx context.Context, bundleID string) error {
	apps, err := d.ListApps(ctx, false)
	if err != nil {
		return err
	}
	for _, a := range apps {
		if a.ID == bundleID {
			return nil
		}
	}
	return mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this iPhone", bundleID)
}
