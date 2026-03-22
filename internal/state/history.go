package state

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// HistoryEntry holds metadata for a single archived run.
type HistoryEntry struct {
	RunID   string
	Dir     string
	Status  string
	Ticket  string
	Elapsed time.Duration
	CostUSD float64
}

// HistoryDir returns the path to the history directory within artifactsDir.
func HistoryDir(artifactsDir string) string {
	return filepath.Join(artifactsDir, "history")
}

// copyEntry recursively copies src to dst.
func copyEntry(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyEntry(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	// file copy
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %s: %w", src, err)
	}
	return nil
}

// ArchiveRun copies all non-history entries from artifactsDir into a timestamped
// history subdirectory, then removes the originals. Returns the run ID.
func ArchiveRun(artifactsDir string) (string, error) {
	runID := time.Now().Format("2006-01-02T15-04-05.000")
	destDir := filepath.Join(artifactsDir, "history", runID)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(artifactsDir)
	if err != nil {
		os.RemoveAll(destDir)
		return "", err
	}

	for _, e := range entries {
		if e.Name() == "history" {
			continue
		}
		src := filepath.Join(artifactsDir, e.Name())
		dst := filepath.Join(destDir, e.Name())
		if err := copyEntry(src, dst); err != nil {
			os.RemoveAll(destDir) // clean partial archive, leave originals intact
			return "", err
		}
	}

	// All copies succeeded — remove originals
	for _, e := range entries {
		if e.Name() == "history" {
			continue
		}
		target := filepath.Join(artifactsDir, e.Name())
		if err := os.RemoveAll(target); err != nil {
			return runID, fmt.Errorf("removing %s after archive: %w", e.Name(), err)
		}
	}

	return runID, nil
}

// PruneHistory removes the oldest history entries until the count is within limit.
// No-op if the history directory does not exist.
func PruneHistory(artifactsDir string, limit int) error {
	histDir := HistoryDir(artifactsDir)
	entries, err := os.ReadDir(histDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	// entries from ReadDir are already sorted alphabetically (chronologically for timestamp names)
	for len(entries) > limit {
		oldest := entries[0]
		if err := os.RemoveAll(filepath.Join(histDir, oldest.Name())); err != nil {
			return err
		}
		entries = entries[1:]
	}
	return nil
}

// ListHistory returns all history entries, sorted newest-first.
// Returns (nil, nil) if the history directory does not exist.
func ListHistory(artifactsDir string) ([]HistoryEntry, error) {
	histDir := HistoryDir(artifactsDir)
	entries, err := os.ReadDir(histDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var result []HistoryEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		entryDir := filepath.Join(histDir, e.Name())

		st, err := Load(entryDir)
		if err != nil {
			continue
		}

		timing, _ := LoadTiming(entryDir)
		var elapsed time.Duration
		if timing != nil {
			elapsed = timing.TotalElapsed()
		}

		costs, _ := LoadCosts(entryDir)
		var costUSD float64
		if costs != nil {
			costUSD = costs.TotalCostUSD
		}

		result = append(result, HistoryEntry{
			RunID:   e.Name(),
			Dir:     entryDir,
			Status:  st.GetStatus(),
			Ticket:  st.GetTicket(),
			Elapsed: elapsed,
			CostUSD: costUSD,
		})
	}

	// Sort newest-first (reverse alphabetical)
	sort.Slice(result, func(i, j int) bool {
		return result[i].RunID > result[j].RunID
	})

	return result, nil
}
