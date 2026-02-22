package state

import (
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to a file atomically by writing to a temporary
// file first and then renaming it to the target path. This prevents corruption
// from crashes mid-write.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // best-effort cleanup
		return err
	}
	_ = dir // suppress unused warning if needed
	return nil
}
