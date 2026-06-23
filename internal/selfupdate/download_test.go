package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// makeArchive returns a gzipped tar containing a single file "orc" with body.
func makeArchive(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "orc", Mode: 0o755, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testSHA256 is the test's own checksum helper; the production sha256hex is
// unexported in the same package and added in B2 Step 3, so the test must not
// redefine it.
func testSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestResolveLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			fmt.Fprint(w, `{"tag_name":"v0.3.0","name":"orc v0.3.0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()

	tag, err := ResolveLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.3.0" {
		t.Errorf("tag = %q, want v0.3.0", tag)
	}
}

func TestDownload_GoodChecksum(t *testing.T) {
	binBody := []byte("#!/bin/sh\necho fake orc\n")
	archive := makeArchive(t, binBody)
	archiveName := ArchiveName("v0.3.0", "linux", "amd64")
	checksums := fmt.Sprintf("%s  %s\n", testSHA256(archive), archiveName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, archiveName):
			w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			fmt.Fprint(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := DownloadBase
	DownloadBase = srv.URL
	defer func() { DownloadBase = old }()

	path, err := Download(context.Background(), "v0.3.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binBody) {
		t.Errorf("extracted binary body mismatch")
	}
}

func TestDownload_BadChecksum(t *testing.T) {
	binBody := []byte("fake orc")
	archive := makeArchive(t, binBody)
	archiveName := ArchiveName("v0.3.0", "linux", "amd64")
	// Wrong checksum on purpose.
	checksums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), archiveName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, archiveName):
			w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			fmt.Fprint(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := DownloadBase
	DownloadBase = srv.URL
	defer func() { DownloadBase = old }()

	path, err := Download(context.Background(), "v0.3.0", "linux", "amd64")
	if err == nil {
		os.Remove(path)
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error = %v, want a checksum mismatch", err)
	}
	if path != "" {
		t.Errorf("expected no temp path on failure, got %q", path)
	}
}
