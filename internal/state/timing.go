package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TimingEntry struct {
	Phase    string    `json:"phase"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end,omitempty"`
	Duration string    `json:"duration,omitempty"`
}

type Timing struct {
	mu      sync.Mutex
	entries []TimingEntry
}

// NewTiming creates a Timing with the given entries.
func NewTiming(entries []TimingEntry) *Timing {
	return &Timing{entries: entries}
}

// Entries returns a snapshot of all timing entries under the lock.
func (t *Timing) Entries() []TimingEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	snapshot := make([]TimingEntry, len(t.entries))
	copy(snapshot, t.entries)
	return snapshot
}

func timingPath(artifactsDir string) string {
	return filepath.Join(artifactsDir, "timing.json")
}

// LoadTiming reads timing data from the artifacts directory.
func LoadTiming(artifactsDir string) (*Timing, error) {
	path := timingPath(artifactsDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Timing{}, nil
		}
		return nil, err
	}
	var t Timing
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// marshalJSON serializes without locking — caller must hold mu.
func (t *Timing) marshalJSON() ([]byte, error) {
	return json.MarshalIndent(struct {
		Entries []TimingEntry `json:"entries"`
	}{Entries: t.entries}, "", "  ")
}

// MarshalJSON implements json.Marshaler for external callers.
func (t *Timing) MarshalJSON() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.marshalJSON()
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *Timing) UnmarshalJSON(data []byte) error {
	var raw struct {
		Entries []TimingEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.entries = raw.Entries
	return nil
}

func (t *Timing) save(artifactsDir string) error {
	data, err := t.marshalJSON()
	if err != nil {
		return err
	}
	return WriteFileAtomic(timingPath(artifactsDir), data, 0644)
}

// AddStart appends a new timing entry for the given phase.
func (t *Timing) AddStart(phaseName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, TimingEntry{
		Phase: phaseName,
		Start: time.Now(),
	})
}

// AddStartAt appends a new timing entry for the given phase with the specified start time.
func (t *Timing) AddStartAt(phaseName string, startTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, TimingEntry{
		Phase: phaseName,
		Start: startTime,
	})
}

// AddEnd records the end time for the most recent entry matching phaseName.
func (t *Timing) AddEnd(phaseName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.entries) - 1; i >= 0; i-- {
		if t.entries[i].Phase == phaseName && t.entries[i].End.IsZero() {
			t.entries[i].End = time.Now()
			d := t.entries[i].End.Sub(t.entries[i].Start)
			t.entries[i].Duration = formatDuration(d)
			break
		}
	}
}

// AddEndAt records the given end time for the most recent entry matching phaseName.
func (t *Timing) AddEndAt(phaseName string, endTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.entries) - 1; i >= 0; i-- {
		if t.entries[i].Phase == phaseName && t.entries[i].End.IsZero() {
			t.entries[i].End = endTime
			d := t.entries[i].End.Sub(t.entries[i].Start)
			t.entries[i].Duration = formatDuration(d)
			break
		}
	}
}

// Flush writes the in-memory timing data to disk.
func (t *Timing) Flush(artifactsDir string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.save(artifactsDir)
}

// TotalElapsed returns the sum of all completed timing entry durations.
func (t *Timing) TotalElapsed() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	var total time.Duration
	for _, e := range t.entries {
		if !e.End.IsZero() {
			total += e.End.Sub(e.Start)
		}
	}
	return total
}

// FormatDuration formats a duration as "Xm YYs" or "Xh YYm" for longer durations.
func FormatDuration(d time.Duration) string {
	if d >= time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %02ds", m, s)
}

func formatDuration(d time.Duration) string {
	return FormatDuration(d)
}
