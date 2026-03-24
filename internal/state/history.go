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
	RunID           string
	Dir             string
	Status          string
	Ticket          string
	Elapsed         time.Duration
	CostUSD         float64
	FailureCategory string
}

// HistoryDir returns the path to the history directory within artifactsDir.
func HistoryDir(artifactsDir string) string {
	return filepath.Join(artifactsDir, "history")
}

// LatestHistoryDir returns the path to the most recent history entry
// (newest timestamped subdirectory) within artifactsDir/history/.
// Returns ("", nil) if no history entries exist.
func LatestHistoryDir(artifactsDir string) (string, error) {
	histDir := HistoryDir(artifactsDir)
	entries, err := os.ReadDir(histDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	// ReadDir returns entries sorted alphabetically; timestamp-based names
	// sort chronologically, so the last entry is the newest.
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].IsDir() {
			return filepath.Join(histDir, entries[i].Name()), nil
		}
	}
	return "", nil
}

// copyEntry recursively copies src to dst.
func copyEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	// Skip symlinks — artifacts shouldn't contain them, and following
	// them during archive could be surprising.
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
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
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copying %s: %w", src, err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return fmt.Errorf("syncing %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", dst, err)
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
	if limit <= 0 {
		limit = 1
	}
	histDir := HistoryDir(artifactsDir)
	entries, err := os.ReadDir(histDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	// Filter to directories only — consistent with ListHistory/LatestHistoryDir.
	dirs := entries[:0]
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		}
	}
	entries = dirs

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

		if !HasState(entryDir) {
			continue // skip entries without state.json
		}

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
			costUSD = costs.TotalCost()
		}

		result = append(result, HistoryEntry{
			RunID:           e.Name(),
			Dir:             entryDir,
			Status:          st.GetStatus(),
			Ticket:          st.GetTicket(),
			Elapsed:         elapsed,
			CostUSD:         costUSD,
			FailureCategory: st.GetFailureCategory(),
		})
	}

	// Sort newest-first (reverse alphabetical)
	sort.Slice(result, func(i, j int) bool {
		return result[i].RunID > result[j].RunID
	})

	return result, nil
}
