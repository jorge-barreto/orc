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
	want := "[...truncated...]\nllo world!"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
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

func TestTailWriter_ZeroLengthWrite(t *testing.T) {
	w := newTailWriter(10)
	w.Write([]byte("abc")) //nolint:errcheck

	n, err := w.Write(nil)
	if err != nil {
		t.Fatalf("Write(nil) error: %v", err)
	}
	if n != 0 {
		t.Fatalf("Write(nil) returned %d, want 0", n)
	}

	n, err = w.Write([]byte{})
	if err != nil {
		t.Fatalf("Write([]byte{}) error: %v", err)
	}
	if n != 0 {
		t.Fatalf("Write([]byte{}) returned %d, want 0", n)
	}

	got := w.String()
	if got != "abc" {
		t.Fatalf("String() = %q, want %q after zero-length writes", got, "abc")
	}
}

func TestTailWriter_LargeOverflow(t *testing.T) {
	w := newTailWriter(5)
	// Use distinct byte values to detect content-ordering bugs
	data := make([]byte, 50)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 50 {
		t.Fatalf("Write returned %d, want 50", n)
	}
	got := w.String()
	// Last 5 bytes: indices 45-49 → 45%26=19='t', 46%26=20='u', 47%26=21='v', 48%26=22='w', 49%26=23='x'
	want := "[...truncated...]\ntuvwx"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
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
	prefix := "[...truncated...]\n"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("expected truncation prefix, got %q", got[:min(len(got), 30)])
	}
	data := got[len(prefix):]
	if len(data) != 100 {
		t.Fatalf("data portion length = %d, want 100", len(data))
	}
	for i, b := range []byte(data) {
		if b != 'x' {
			t.Fatalf("data[%d] = %q, want 'x'", i, b)
		}
	}
}
