package dispatch

import (
	"bufio"
	"io"
)

// StdinReader monitors stdin in a goroutine and buffers any input.
// The caller checks for buffered input between agent turns.
type StdinReader struct {
	lines chan string
	done  chan struct{}
}

// NewStdinReader starts a background goroutine reading lines from r.
// The goroutine exits when r returns an error (EOF) or Stop() is called.
func NewStdinReader(r io.Reader) *StdinReader {
	sr := &StdinReader{
		lines: make(chan string, 16),
		done:  make(chan struct{}),
	}
	go sr.readLoop(r)
	return sr
}

func (sr *StdinReader) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for {
		select {
		case <-sr.done:
			return
		default:
		}
		if !scanner.Scan() {
			close(sr.lines) // signal EOF; buffered items drain first (Go spec)
			return
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		select {
		case sr.lines <- line:
		case <-sr.done:
			return
		}
	}
}

// ReadLine does a non-blocking check for buffered input.
// Returns the line and true if input is available, or ("", false) otherwise.
func (sr *StdinReader) ReadLine() (string, bool) {
	select {
	case line, ok := <-sr.lines:
		if !ok {
			return "", false
		}
		return line, true
	default:
		return "", false
	}
}

// ReadLineBlocking waits for the next line from stdin.
// Returns ("", false) if the reader is stopped before input arrives.
func (sr *StdinReader) ReadLineBlocking() (string, bool) {
	select {
	case line, ok := <-sr.lines:
		return line, ok // ok==false means EOF (channel closed, all buffered items consumed)
	case <-sr.done:
		return "", false
	}
}

// Stop signals the background goroutine to exit.
// The goroutine may remain blocked on scanner.Scan() until stdin produces
// input or is closed; Stop is best-effort.
func (sr *StdinReader) Stop() {
	select {
	case <-sr.done:
		// already stopped
	default:
		close(sr.done)
	}
}
