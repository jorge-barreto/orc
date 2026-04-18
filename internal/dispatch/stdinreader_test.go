package dispatch

import (
	"io"
	"strings"
	"testing"
	"time"
)

type readRes struct {
	line string
	ok   bool
}

func TestStdinReader_ReadLine_ReturnsData(t *testing.T) {
	r := strings.NewReader("hello\n")
	sr := NewStdinReader(r)
	defer sr.Stop()

	deadline := time.Now().Add(time.Second)
	for {
		line, ok := sr.ReadLine()
		if ok {
			if line != "hello" {
				t.Fatalf("line = %q, want %q", line, "hello")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("did not receive line within 1s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStdinReader_ReadLine_ClosedChannel(t *testing.T) {
	sr := NewStdinReader(strings.NewReader(""))
	defer sr.Stop()

	// Give the goroutine a moment to hit EOF and close the channel.
	time.Sleep(50 * time.Millisecond)

	line, ok := sr.ReadLine()
	if ok || line != "" {
		t.Fatalf("first ReadLine = (%q, %v), want (\"\", false)", line, ok)
	}
	// A second call must return the same — proves it's durable (channel
	// closed), not just a transient empty-buffer read.
	line2, ok2 := sr.ReadLine()
	if ok2 || line2 != "" {
		t.Fatalf("second ReadLine = (%q, %v), want (\"\", false)", line2, ok2)
	}
}

func TestStdinReader_ReadLineBlocking_UnblocksOnClose(t *testing.T) {
	pr, pw := io.Pipe()
	sr := NewStdinReader(pr)
	defer sr.Stop()

	resultCh := make(chan readRes, 1)
	go func() {
		l, ok := sr.ReadLineBlocking()
		resultCh <- readRes{l, ok}
	}()

	time.AfterFunc(50*time.Millisecond, func() { pw.Close() })

	select {
	case res := <-resultCh:
		if res.ok || res.line != "" {
			t.Fatalf("ReadLineBlocking = (%q, %v), want (\"\", false)", res.line, res.ok)
		}
	case <-time.After(time.Second):
		t.Fatal("ReadLineBlocking did not unblock within 1s")
	}
}

func TestStdinReader_ReadLineBlocking_ReturnsBufferedBeforeClose(t *testing.T) {
	sr := NewStdinReader(strings.NewReader("queued\n"))
	defer sr.Stop()

	// First call: must observe the buffered value even though the channel
	// will also be closed (Go spec: receivers drain buffered values before
	// seeing the close signal).
	firstCh := make(chan readRes, 1)
	go func() {
		l, ok := sr.ReadLineBlocking()
		firstCh <- readRes{l, ok}
	}()
	select {
	case res := <-firstCh:
		if !res.ok || res.line != "queued" {
			t.Fatalf("first ReadLineBlocking = (%q, %v), want (%q, true)", res.line, res.ok, "queued")
		}
	case <-time.After(time.Second):
		t.Fatal("first ReadLineBlocking did not return within 1s")
	}

	// Second call: channel drained, then closed → ("", false).
	secondCh := make(chan readRes, 1)
	go func() {
		l, ok := sr.ReadLineBlocking()
		secondCh <- readRes{l, ok}
	}()
	select {
	case res := <-secondCh:
		if res.ok || res.line != "" {
			t.Fatalf("second ReadLineBlocking = (%q, %v), want (\"\", false)", res.line, res.ok)
		}
	case <-time.After(time.Second):
		t.Fatal("second ReadLineBlocking did not return within 1s")
	}
}
