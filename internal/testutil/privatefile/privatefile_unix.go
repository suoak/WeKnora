//go:build !windows

package privatefile

import "os"

func WriteFile(path string, data []byte, mode os.FileMode) error {
	return os.WriteFile(path, data, mode)
}
