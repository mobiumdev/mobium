package device

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A real iPhone cannot clear an app's data in place: devicectl can copy into
// an app's container but delete nothing from it. Uninstalling and installing
// again is the reset a phone has, and this does it from the app's own bundle.
//
// Measured on an iPhone 15 Plus, iOS 26.6.2, with MobiumApp: an uninstall
// and an install emptied its container (65 entries to 9, no file left but
// the launch snapshot iOS writes at install) and reset its notification,
// location and camera permissions — each denied before, each asked again
// after. Installing over the app without uninstalling kept all three
// denials: that is an update, not a reset, which is why a note from
// 2026-09-28 said a reinstall kept a notification denial.

// BundleIDOf reads the bundle id of a .app, or of the .app inside an .ipa.
func BundleIDOf(ctx context.Context, path string) (string, error) {
	var raw []byte
	info, err := os.Stat(path)
	if err != nil {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "no app bundle at %s", path)
	}
	switch {
	case info.IsDir():
		raw, err = os.ReadFile(filepath.Join(path, "Info.plist"))
		if err != nil {
			return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s has no Info.plist — is it a .app?", path)
		}
	case strings.EqualFold(filepath.Ext(path), ".ipa"):
		raw, err = ipaInfoPlist(path)
		if err != nil {
			return "", err
		}
	default:
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is neither a .app nor an .ipa", path)
	}
	data, err := plistToJSON(ctx, raw)
	if err != nil {
		return "", err
	}
	var plist struct {
		ID string `json:"CFBundleIdentifier"`
	}
	if err := json.Unmarshal(data, &plist); err != nil || plist.ID == "" {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s names no bundle id in its Info.plist", path)
	}
	return plist.ID, nil
}

// ipaInfoPlist reads Payload/<name>.app/Info.plist out of an .ipa.
func ipaInfoPlist(path string) ([]byte, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a readable .ipa: %v", path, err)
	}
	defer z.Close()
	for _, f := range z.File {
		parts := strings.Split(f.Name, "/")
		if len(parts) == 3 && parts[0] == "Payload" && strings.HasSuffix(parts[1], ".app") && parts[2] == "Info.plist" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		}
	}
	return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s has no Payload/*.app/Info.plist — is it an .ipa?", path)
}

// containerFiles lists the files, not folders, in an app's data container.
func (d *Devicectl) containerFiles(ctx context.Context, bundleID string) (files []string, entries int, err error) {
	raw, err := d.run(ctx, "device", "info", "files", "--device", d.Phone.UDID,
		"--domain-type", "appDataContainer", "--domain-identifier", bundleID)
	if err != nil {
		return nil, 0, err
	}
	return parseContainerFiles(raw)
}

func parseContainerFiles(raw json.RawMessage) ([]string, int, error) {
	var list struct {
		Files []struct {
			RelativePath string `json:"relativePath"`
			Resources    struct {
				IsDirectory bool `json:"isDirectory"`
			} `json:"resources"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, 0, fmt.Errorf("parse devicectl file list: %w", err)
	}
	var files []string
	for _, f := range list.Files {
		if !f.Resources.IsDirectory {
			files = append(files, f.RelativePath)
		}
	}
	return files, len(list.Files), nil
}

// installSnapshot is what iOS itself writes into a new container at install:
// the launch screen's snapshot. Anything else in a container straight after
// an install was not emptied.
func installSnapshot(path string) bool {
	return strings.HasPrefix(path, "Library/SplashBoard/")
}

// ResetFromBundle resets an app on a phone to a fresh install: it checks the
// bundle is that app, uninstalls it, installs the bundle, and reads the data
// container back.
func (d *Devicectl) ResetFromBundle(ctx context.Context, appID, bundle string) (ClearedData, error) {
	id, err := BundleIDOf(ctx, bundle)
	if err != nil {
		return ClearedData{}, err
	}
	if id != appID {
		return ClearedData{}, mobiumerr.New(mobiumerr.InvalidArgument, "%s is %s, not %s — the reset installs the "+
			"bundle it is given, so it must be the app's own", bundle, id, appID)
	}
	// Uninstalling first is what makes it a reset: an install over the app
	// is an update and keeps its data and its permissions, measured.
	if err := d.UninstallApp(ctx, appID); err != nil {
		return ClearedData{}, err
	}
	if err := d.InstallApp(ctx, bundle); err != nil {
		return ClearedData{}, mobiumerr.New(mobiumerr.DeviceServer, "uninstalled %s and could not install it again "+
			"from %s — it is not on the phone now: %w", appID, bundle, err)
	}
	files, entries, err := d.containerFiles(ctx, appID)
	if err != nil {
		return ClearedData{}, mobiumerr.New(mobiumerr.NotConfirmed, "reinstalled %s and could not read its data "+
			"container back to confirm it is empty: %w", appID, err)
	}
	var left []string
	for _, f := range files {
		if !installSnapshot(f) {
			left = append(left, f)
		}
	}
	if len(left) > 0 {
		return ClearedData{}, mobiumerr.New(mobiumerr.NotConfirmed, "reinstalled %s and its data container still "+
			"holds %d file(s), %s among them", appID, len(left), left[0])
	}
	return ClearedData{
		Emptied: []string{fmt.Sprintf("its data container, read back through devicectl: %d entries, folders and "+
			"the launch snapshot iOS writes at install, and no file of the app's", entries)},
		NotReadBack: []string{"its privacy permissions, reset by the uninstall: on an iPhone 15 Plus, iOS 26.6.2, " +
			"notifications, location and camera each asked again afterwards. Nothing outside the app can read " +
			"them on a phone, so they are not read back here"},
	}, nil
}
