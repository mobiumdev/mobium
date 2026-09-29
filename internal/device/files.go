package device

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Files crossing between this machine and a device: app_upload and
// app_download. Each goes where the device keeps what a person downloads,
// because that is where an app's file picker looks and where an app's own
// downloads land:
//
//   - Android: the shared Download folder. A file pushed there is invisible to
//     the picker until MediaStore indexes it, measured on Android 15 — the
//     picker reads MediaStore, not the folder — so an upload asks for the scan
//     and reads the index back.
//   - iOS simulator: the app's own Documents folder, which the Files app shows
//     under On My iPhone for an app that declares file sharing. iOS has no
//     shared Downloads folder an app writes into.
//
// An app's private folders are out of reach on Android — `run-as` refuses a
// release build ("package not debuggable") — so nothing here reads them.

// DeviceFile is one file in the folder downloads are kept in.
type DeviceFile struct {
	Name     string    `json:"name"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

// Transfer is one file moved, as read back at both ends.
type Transfer struct {
	Name string `json:"name"`
	// Where is the file on the device, as a person would find it:
	// "Download/report.txt", or "MobiumApp's Documents/report.txt".
	Where string `json:"where"`
	Bytes int64  `json:"bytes"`
	// Checked says how the transfer was confirmed.
	Checked string `json:"checked"`
}

// androidDownloads is the shared Download folder.
const androidDownloads = "/sdcard/Download"

// mediaIndexWait bounds how long an upload waits for MediaStore to list it.
const mediaIndexWait = 10 * time.Second

// CheckFileName refuses a name that is not one file in the folder: a path,
// "." or "..", or nothing. A name is where a file lands, never where to look.
func CheckFileName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%q is not a file name — a name, not a path: "+
			"files go in the folder the device keeps downloads in", name)
	}
	return nil
}

// UploadFile puts a local file in the Download folder and waits for MediaStore
// to list it, which is what makes a file picker find it.
func (a *ADB) UploadFile(ctx context.Context, local, name string) (Transfer, error) {
	if err := CheckFileName(name); err != nil {
		return Transfer{}, err
	}
	info, err := os.Stat(local)
	if err != nil || info.IsDir() {
		return Transfer{}, mobiumerr.New(mobiumerr.InvalidArgument, "no file at %s to upload", local)
	}
	remote := androidDownloads + "/" + name
	if _, err := a.Run(ctx, "push", local, remote); err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "pushing %s: %w", name, err)
	}
	if got, err := a.remoteSize(ctx, remote); err != nil || got != info.Size() {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "pushed %s, and the device holds %d bytes of its %d",
			name, got, info.Size())
	}
	// The scan broadcast is deprecated and still honored, measured on
	// Android 15; MediaStore's own index is read back rather than trusted.
	if _, err := a.Shell(ctx, "am", "broadcast", "-a", "android.intent.action.MEDIA_SCANNER_SCAN_FILE",
		"-d", shellQuote("file://"+remote)); err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "asking MediaStore to index %s: %w", name, err)
	}
	deadline := time.Now().Add(mediaIndexWait)
	for {
		if indexed, err := a.mediaIndexed(ctx, name, info.Size()); err == nil && indexed {
			break
		}
		if time.Now().After(deadline) {
			return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "%s is in the Download folder, and MediaStore "+
				"did not list it within %s — a file picker reads MediaStore, so it may not show it", name, mediaIndexWait)
		}
		time.Sleep(300 * time.Millisecond)
	}
	return Transfer{Name: name, Where: "Download/" + name, Bytes: info.Size(),
		Checked: "its size read back on the device, and MediaStore listing it"}, nil
}

// DownloadFile copies a file from the Download folder to a local path.
func (a *ADB) DownloadFile(ctx context.Context, name, local string) (Transfer, error) {
	if err := CheckFileName(name); err != nil {
		return Transfer{}, err
	}
	remote := androidDownloads + "/" + name
	size, err := a.remoteSize(ctx, remote)
	if err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.NoSuchElement, "no %s in the Download folder", name).
			WithRemedy("app_download with no name lists what is there")
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return Transfer{}, err
	}
	if _, err := a.Run(ctx, "pull", remote, local); err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.DeviceServer, "pulling %s: %w", name, err)
	}
	info, err := os.Stat(local)
	if err != nil || info.Size() != size {
		return Transfer{}, mobiumerr.New(mobiumerr.NotConfirmed, "pulled %s, and %d of its %d bytes arrived", name, sizeOf(info), size)
	}
	return Transfer{Name: name, Where: "Download/" + name, Bytes: size,
		Checked: "the copy's size read back against the device's"}, nil
}

// ListFiles lists the Download folder.
func (a *ADB) ListFiles(ctx context.Context) ([]DeviceFile, error) {
	out, err := a.Shell(ctx, "find", androidDownloads, "-maxdepth", "1", "-type", "f",
		"-exec", "stat", "-c", shellQuote("%s|%Y|%n"), "{}", "+")
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "listing the Download folder: %w", err)
	}
	var files []DeviceFile
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		parts := strings.SplitN(strings.TrimSpace(sc.Text()), "|", 3)
		if len(parts) != 3 {
			continue
		}
		size, _ := strconv.ParseInt(parts[0], 10, 64)
		secs, _ := strconv.ParseInt(parts[1], 10, 64)
		files = append(files, DeviceFile{Name: filepath.Base(parts[2]), Bytes: size, Modified: time.Unix(secs, 0).UTC()})
	}
	sortFiles(files)
	return files, nil
}

func (a *ADB) remoteSize(ctx context.Context, remote string) (int64, error) {
	out, err := a.Shell(ctx, "stat", "-c", "%s", shellQuote(remote))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

// mediaIndexed reads MediaStore's downloads for a name and size.
func (a *ADB) mediaIndexed(ctx context.Context, name string, size int64) (bool, error) {
	where := "_display_name='" + strings.ReplaceAll(name, "'", "''") + "'"
	out, err := a.Shell(ctx, "content", "query", "--uri", "content://media/external/downloads",
		"--projection", "_display_name:_size", "--where", shellQuote(where))
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "_size="+strconv.FormatInt(size, 10)), nil
}

// documents is an app's Documents folder in its simulator container. The
// container is asked for each time: reinstalling an app moves it.
func (s *Simctl) documents(ctx context.Context, bundleID string) (string, error) {
	if !s.AppInstalled(ctx, bundleID) {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this simulator", bundleID)
	}
	out, err := s.Run(ctx, "get_app_container", s.UDID, bundleID, "data")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" || !filepath.IsAbs(root) {
		return "", mobiumerr.New(mobiumerr.DeviceServer, "simctl gave no data container for %s: %q", bundleID, root)
	}
	return filepath.Join(root, "Documents"), nil
}

// UploadFile puts a local file in an app's Documents folder.
func (s *Simctl) UploadFile(ctx context.Context, local, name, bundleID string) (Transfer, error) {
	if err := CheckFileName(name); err != nil {
		return Transfer{}, err
	}
	docs, err := s.documents(ctx, bundleID)
	if err != nil {
		return Transfer{}, err
	}
	if err := os.MkdirAll(docs, 0o755); err != nil {
		return Transfer{}, err
	}
	n, err := copyFile(local, filepath.Join(docs, name))
	if err != nil {
		return Transfer{}, err
	}
	return Transfer{Name: name, Where: bundleID + " Documents/" + name, Bytes: n,
		Checked: "its size read back in the app's container"}, nil
}

// DownloadFile copies a file from an app's Documents folder to a local path.
func (s *Simctl) DownloadFile(ctx context.Context, name, bundleID, local string) (Transfer, error) {
	if err := CheckFileName(name); err != nil {
		return Transfer{}, err
	}
	docs, err := s.documents(ctx, bundleID)
	if err != nil {
		return Transfer{}, err
	}
	src := filepath.Join(docs, name)
	if _, err := os.Stat(src); err != nil {
		return Transfer{}, mobiumerr.New(mobiumerr.NoSuchElement, "no %s in %s's Documents", name, bundleID).
			WithRemedy("app_download with no name lists what is there")
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return Transfer{}, err
	}
	n, err := copyFile(src, local)
	if err != nil {
		return Transfer{}, err
	}
	return Transfer{Name: name, Where: bundleID + " Documents/" + name, Bytes: n,
		Checked: "the copy's size read back against the app's"}, nil
}

// ListFiles lists an app's Documents folder.
func (s *Simctl) ListFiles(ctx context.Context, bundleID string) ([]DeviceFile, error) {
	docs, err := s.documents(ctx, bundleID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(docs)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var files []DeviceFile
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			files = append(files, DeviceFile{Name: e.Name(), Bytes: info.Size(), Modified: info.ModTime().UTC()})
		}
	}
	sortFiles(files)
	return files, nil
}

// copyFile copies src to dst and confirms the size arrived.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, mobiumerr.New(mobiumerr.InvalidArgument, "cannot read %s: %w", src, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || info.IsDir() {
		return 0, mobiumerr.New(mobiumerr.InvalidArgument, "no file at %s", src)
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "cannot write %s: %w", dst, err)
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, mobiumerr.New(mobiumerr.DeviceServer, "copying %s: %w", src, err)
	}
	if got, err := os.Stat(dst); err != nil || got.Size() != info.Size() {
		return n, mobiumerr.New(mobiumerr.NotConfirmed, "copied %s, and %d of its %d bytes arrived", src, sizeOf(got), info.Size())
	}
	return info.Size(), nil
}

func sizeOf(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

func sortFiles(files []DeviceFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
}

// String is a file as a listing prints it.
func (f DeviceFile) String() string {
	return fmt.Sprintf("%-40s %10d bytes  %s", f.Name, f.Bytes, f.Modified.Local().Format("2006-01-02 15:04"))
}
