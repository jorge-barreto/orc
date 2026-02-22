package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type TimingEntry struct {
	Phase    string    `json:"phase"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end,omitempty"`
	Duration string    `json:"duration,omitempty"`
}

type Timing struct {
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
		if os.IsNotExist(err) {
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
	return os.WriteFile(timingPath(artifactsDir), data, 0644)
}

// RecordStart records the start time for a phase.
func RecordStart(artifactsDir, phaseName string) error {
	t, err := LoadTiming(artifactsDir)
	if err != nil {
		return err
	}
	t.Entries = append(t.Entries, TimingEntry{
		Phase: phaseName,
		Start: time.Now(),
	})
	return t.save(artifactsDir)
}

// RecordEnd records the end time for the most recent entry matching phaseName.
func RecordEnd(artifactsDir, phaseName string) error {
	t, err := LoadTiming(artifactsDir)
	if err != nil {
		return err
	}
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Phase == phaseName && t.Entries[i].End.IsZero() {
			t.Entries[i].End = time.Now()
			d := t.Entries[i].End.Sub(t.Entries[i].Start)
			t.Entries[i].Duration = formatDuration(d)
			break
		}
	}
	return t.save(artifactsDir)
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %02ds", m, s)
}
