package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countTempFiles reports how many orc-update temp files currently exist, so a
// test can prove a failed Download left nothing behind.
func countOrcUpdateTemps(t *testing.T) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "orc-update-*"))
	if err != nil {
		t.Fatal(err)
	}
	return len(matches)
}

// TestDownload_ExtractedBinaryExceedsCap proves the read cap on the extracted
// tar entry: a tiny archive whose orc entry is larger than maxArtifactBytes must
// error (mentioning the size/limit) and leave no temp file behind. The cap is
// lowered to a few bytes for the test so we never allocate anything large.
func TestDownload_ExtractedBinaryExceedsCap(t *testing.T) {
	old := maxArtifactBytes
	maxArtifactBytes = 16
	defer func() { maxArtifactBytes = old }()

	// 100-byte body > 16-byte cap.
	binBody := []byte(strings.Repeat("A", 100))
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
	oldBase := DownloadBase
	DownloadBase = srv.URL
	defer func() { DownloadBase = oldBase }()

	before := countOrcUpdateTemps(t)
	path, err := Download(context.Background(), "v0.3.0", "linux", "amd64")
	if err == nil {
		os.Remove(path)
		t.Fatal("expected oversize-extract error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want it to mention the size limit (exceeds)", err)
	}
	if path != "" {
		t.Errorf("expected no temp path on failure, got %q", path)
	}
	if after := countOrcUpdateTemps(t); after != before {
		t.Errorf("temp file leaked: %d before, %d after", before, after)
	}
}

// TestFetch_ResponseExceedsCap proves the read cap on the raw HTTP download: a
// response body larger than maxArtifactBytes must error before the bytes are
// returned. The archive fetch is the first network read in Download, so an
// oversize archive response surfaces here.
func TestFetch_ResponseExceedsCap(t *testing.T) {
	old := maxArtifactBytes
	maxArtifactBytes = 16
	defer func() { maxArtifactBytes = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("A", 100))) // 100 bytes > 16-byte cap
	}))
	defer srv.Close()
	oldBase := DownloadBase
	DownloadBase = srv.URL
	defer func() { DownloadBase = oldBase }()

	path, err := Download(context.Background(), "v0.3.0", "linux", "amd64")
	if err == nil {
		os.Remove(path)
		t.Fatal("expected oversize-response error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want it to mention the size limit (exceeds)", err)
	}
	if path != "" {
		t.Errorf("expected no temp path on failure, got %q", path)
	}
}
