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
	Entries []TimingEntry `json:"entries"`
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

func (t *Timing) save(artifactsDir string) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(timingPath(artifactsDir), data, 0644)
}

// AddStart appends a new timing entry for the given phase.
func (t *Timing) AddStart(phaseName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Entries = append(t.Entries, TimingEntry{
		Phase: phaseName,
		Start: time.Now(),
	})
}

// AddEnd records the end time for the most recent entry matching phaseName.
func (t *Timing) AddEnd(phaseName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Phase == phaseName && t.Entries[i].End.IsZero() {
			t.Entries[i].End = time.Now()
			d := t.Entries[i].End.Sub(t.Entries[i].Start)
			t.Entries[i].Duration = formatDuration(d)
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

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %02ds", m, s)
}
