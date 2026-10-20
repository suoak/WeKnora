//go:build linux

package main

import "syscall"

func disableCoreDumps() bool {
	return syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0}) == nil
}
