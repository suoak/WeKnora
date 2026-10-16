//go:build !windows

package config

import "os"

func privateSourceIDPilotFile(_ string, info os.FileInfo) bool {
	return info.Mode().Perm()&0077 == 0
}
