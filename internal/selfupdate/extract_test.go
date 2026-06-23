package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// makeArchiveNamed returns a gzipped tar containing a single regular-file entry
// with the given name and body. It is a sibling of makeArchive (which always
// names the entry "orc") so a test can build an archive whose only member is
// NOT named "orc", exercising the no-match path of extractBinary.
func makeArchiveNamed(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
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

// TestDownload_NoOrcEntry proves the name+typeflag filter in extractBinary: an
// otherwise valid, checksum-consistent archive whose only regular-file member is
// named something other than "orc" must fail extraction (mentioning "no orc
// entry") and leave no temp file behind. This guards the security-relevant
// branch that decides which tar member becomes the installed binary.
func TestDownload_NoOrcEntry(t *testing.T) {
	binBody := []byte("not the orc binary\n")
	archive := makeArchiveNamed(t, "README", binBody)
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
		t.Fatal("expected no-orc-entry error, got nil")
	}
	if !strings.Contains(err.Error(), "no orc entry") {
		t.Errorf("error = %v, want it to mention the missing orc entry (no orc entry)", err)
	}
	if path != "" {
		t.Errorf("expected no temp path on failure, got %q", path)
	}
	if after := countOrcUpdateTemps(t); after != before {
		t.Errorf("temp file leaked: %d before, %d after", before, after)
	}
}
