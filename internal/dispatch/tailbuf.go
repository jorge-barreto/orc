package dispatch

import "sync"

type tailWriter struct {
	mu       sync.Mutex
	buf      []byte
	pos      int
	full     bool
	cap      int
	overflow bool
}

func newTailWriter(cap int) *tailWriter {
	return &tailWriter{buf: make([]byte, cap), cap: cap}
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	if n > w.cap {
		// Only keep last cap bytes
		copy(w.buf, p[n-w.cap:])
		w.pos = 0
		w.full = true
		w.overflow = true
		return n, nil
	}
	// How much fits before end of buf
	space := w.cap - w.pos
	if n <= space {
		if w.full {
			w.overflow = true
		}
		copy(w.buf[w.pos:], p)
		w.pos += n
		if w.pos == w.cap {
			w.pos = 0
			w.full = true
		}
	} else {
		// Split: fill to end, wrap remainder to start
		copy(w.buf[w.pos:], p[:space])
		copy(w.buf[0:], p[space:])
		w.pos = n - space
		w.full = true
		w.overflow = true
	}
	return n, nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var s string
	if !w.full {
		s = string(w.buf[:w.pos])
	} else {
		s = string(w.buf[w.pos:]) + string(w.buf[:w.pos])
	}
	if w.overflow {
		s = "[...truncated...]\n" + s
	}
	return s
}
