package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := WriteFileAtomic(path, []byte(`{"ok":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("got %q", string(data))
	}

	// No temp files should remain
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "test.json" {
			t.Fatalf("unexpected file remaining: %s", e.Name())
		}
	}
}

func TestWriteFileAtomic_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(path, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("got %q, want %q", string(data), "new")
	}
}

func TestWriteFileAtomic_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.json")

	const n = 20
	errs := make(chan error, n)
	for i := range n {
		go func(i int) {
			data := fmt.Sprintf(`{"writer":%d}`, i)
			errs <- WriteFileAtomic(path, []byte(data), 0644)
		}(i)
	}
	for range n {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	// File must exist and contain valid content from one writer
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("file is empty")
	}

	// No temp files should remain
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "concurrent.json" {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
