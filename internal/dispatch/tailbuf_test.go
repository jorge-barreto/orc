package dispatch

import (
	"strings"
	"sync"
	"testing"
)

func TestTailWriter_SmallWrite(t *testing.T) {
	w := newTailWriter(10)
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 5 {
		t.Fatalf("Write returned %d, want 5", n)
	}
	got := w.String()
	if got != "hello" {
		t.Fatalf("String() = %q, want %q", got, "hello")
	}
	if strings.Contains(got, "[...truncated...]") {
		t.Fatalf("unexpected truncation prefix in %q", got)
	}
}

func TestTailWriter_ExactCap(t *testing.T) {
	w := newTailWriter(5)
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 5 {
		t.Fatalf("Write returned %d, want 5", n)
	}
	got := w.String()
	if got != "hello" {
		t.Fatalf("String() = %q, want %q", got, "hello")
	}
	if strings.Contains(got, "[...truncated...]") {
		t.Fatalf("unexpected truncation prefix in %q", got)
	}
}

func TestTailWriter_OverwriteAfterExactFill(t *testing.T) {
	w := newTailWriter(4)
	w.Write([]byte("abcd")) // fills exactly: pos wraps to 0, full=true
	w.Write([]byte("xy"))   // non-wrapping path, overwrites "ab"

	got := w.String()
	want := "[...truncated...]\ncdxy"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestTailWriter_Overflow(t *testing.T) {
	w := newTailWriter(5)
	n, err := w.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 11 {
		t.Fatalf("Write returned %d, want 11", n)
	}
	got := w.String()
	if !strings.HasSuffix(got, "world") {
		t.Fatalf("String() = %q, want suffix %q", got, "world")
	}
	if !strings.Contains(got, "[...truncated...]") {
		t.Fatalf("expected truncation prefix in %q", got)
	}
}

func TestTailWriter_MultipleWrites(t *testing.T) {
	w := newTailWriter(10)
	n1, err := w.Write([]byte("hello "))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n1 != 6 {
		t.Fatalf("first Write returned %d, want 6", n1)
	}
	n2, err := w.Write([]byte("world!"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n2 != 6 {
		t.Fatalf("second Write returned %d, want 6", n2)
	}
	got := w.String()
	if !strings.Contains(got, "world!") {
		t.Fatalf("String() = %q, expected to contain %q", got, "world!")
	}
	if !strings.Contains(got, "[...truncated...]") {
		t.Fatalf("expected truncation prefix in %q", got)
	}
}

func TestTailWriter_WrapAround(t *testing.T) {
	w := newTailWriter(8)
	n1, err := w.Write([]byte("abcdef"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n1 != 6 {
		t.Fatalf("first Write returned %d, want 6", n1)
	}
	n2, err := w.Write([]byte("ghijkl"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n2 != 6 {
		t.Fatalf("second Write returned %d, want 6", n2)
	}
	got := w.String()
	if !strings.Contains(got, "efghijkl") {
		t.Fatalf("String() = %q, expected to contain %q", got, "efghijkl")
	}
	if !strings.Contains(got, "[...truncated...]") {
		t.Fatalf("expected truncation prefix in %q", got)
	}
}

func TestTailWriter_Empty(t *testing.T) {
	w := newTailWriter(10)
	got := w.String()
	if got != "" {
		t.Fatalf("String() = %q, want empty string", got)
	}
}

func TestTailWriter_LargeOverflow(t *testing.T) {
	w := newTailWriter(5)
	data := make([]byte, 50)
	for i := range data {
		data[i] = 'x'
	}
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 50 {
		t.Fatalf("Write returned %d, want 50", n)
	}
	got := w.String()
	if !strings.HasSuffix(got, "xxxxx") {
		t.Fatalf("String() = %q, want last 5 bytes to be 'xxxxx'", got)
	}
	if !strings.Contains(got, "[...truncated...]") {
		t.Fatalf("expected truncation prefix in %q", got)
	}
}

func TestTailWriter_ConcurrentWrites(t *testing.T) {
	w := newTailWriter(100)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				w.Write([]byte("x")) //nolint:errcheck
			}
		}()
	}
	wg.Wait()
	got := w.String()
	if got == "" {
		t.Fatal("String() returned empty, expected non-empty result")
	}
}
