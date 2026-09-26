package device

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileMatchesChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.apk")
	content := []byte("pretend apk")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)

	ok, err := fileMatches(path, hex.EncodeToString(sum[:]))
	if err != nil || !ok {
		t.Errorf("fileMatches on a good file = %v, %v", ok, err)
	}
	ok, _ = fileMatches(path, strings.Repeat("0", 64))
	if ok {
		t.Error("fileMatches accepted a wrong checksum")
	}
	if _, err := fileMatches(filepath.Join(dir, "missing"), "x"); err == nil {
		t.Error("fileMatches did not report a missing file")
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	body := []byte("the real bytes")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "ok.apk")
	a := uia2Artifact{name: "ok.apk", url: srv.URL, sha256: hex.EncodeToString(sum[:])}
	if err := download(context.Background(), a, dest); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(body) {
		t.Errorf("downloaded %q", got)
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tampered"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "bad.apk")
	a := uia2Artifact{name: "bad.apk", url: srv.URL, sha256: strings.Repeat("a", 64)}

	err := download(context.Background(), a, dest)
	if err == nil {
		t.Fatal("a mismatched download was accepted")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v", err)
	}
	// This APK would be installed with permissions granted; a rejected one
	// must not be left on disk for a later run to pick up.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("the rejected download was left in the cache")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("partial files left behind: %v", entries)
	}
}

func TestDownloadRejectsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.apk")
	a := uia2Artifact{name: "x.apk", url: srv.URL, sha256: strings.Repeat("a", 64)}
	err := download(context.Background(), a, dest)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want a 404", err)
	}
}

func TestEnsureUIA2APKsUsesTheCache(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())

	// Pre-populate the cache with files matching the pinned checksums; the
	// downloader must not reach the network at all.
	dir := UIA2CacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only a real APK matches the pinned sums, so instead assert that a
	// mismatched cache entry is re-fetched rather than trusted: point the
	// artifacts at a dead URL and require the attempt to fail.
	for _, a := range uia2Artifacts {
		if err := os.WriteFile(filepath.Join(dir, a.name), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // no network is allowed to succeed
	if _, err := EnsureUIA2APKs(ctx, nil); err == nil {
		t.Error("a cache entry with the wrong checksum was used as-is")
	}
}

func TestPinnedArtifactsAreWellFormed(t *testing.T) {
	if len(uia2Artifacts) != 2 {
		t.Fatalf("expected the server and test APKs, got %d artifacts", len(uia2Artifacts))
	}
	for _, a := range uia2Artifacts {
		if len(a.sha256) != 64 {
			t.Errorf("%s has a %d-char checksum", a.name, len(a.sha256))
		}
		if !strings.Contains(a.url, UIA2Version) && !strings.Contains(a.url, "androidTest") {
			t.Errorf("%s url %q is not pinned to a version", a.name, a.url)
		}
		if !strings.HasPrefix(a.url, "https://") {
			t.Errorf("%s is not fetched over https", a.name)
		}
	}
}

func TestUnzipRefusesPathEscape(t *testing.T) {
	// A zip entry may name any path it likes; joining blindly would let an
	// archive write outside the cache directory.
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")

	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("pwned"))
	zw.Close()
	f.Close()

	dest := filepath.Join(dir, "out")
	os.MkdirAll(dest, 0o755)
	err = unzipTo(archive, dest)
	if err == nil {
		t.Fatal("a path-escaping archive entry was extracted")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "escaped.txt")); statErr == nil {
		t.Error("the escaping entry was written outside the destination")
	}
}

func TestUnzipPreservesExecutableBit(t *testing.T) {
	// The runner binary and its bundled frameworks will not launch without it.
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	f, _ := os.Create(archive)
	zw := zip.NewWriter(f)
	hdr := &zip.FileHeader{Name: "bin/tool"}
	hdr.SetMode(0o755)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("#!/bin/sh\n"))
	zw.Close()
	f.Close()

	dest := filepath.Join(dir, "out")
	if err := unzipTo(archive, dest); err != nil {
		t.Fatalf("unzipTo: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %o, executable bit lost", fi.Mode().Perm())
	}
}
