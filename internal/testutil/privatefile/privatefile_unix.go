//go:build !windows

// Package privatefile provides restricted fixture-file helpers.
package privatefile

import "os"

// WriteFile provides the write file operation.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	return os.WriteFile(path, data, mode)
}
