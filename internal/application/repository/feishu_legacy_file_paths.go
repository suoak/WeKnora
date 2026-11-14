package repository

import (
	"os"
	"path/filepath"
)

// A retired source file may be gone while its images still exist. Resolve the
// surviving parent prefix so a symlink cannot hide that protected directory.
func canonicalLegacyFilePath(path string) (string, error) {
	path = filepath.Clean(path)
	candidate, suffix := path, ""
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
		suffix = filepath.Join(filepath.Base(candidate), suffix)
		candidate = parent
	}
}
