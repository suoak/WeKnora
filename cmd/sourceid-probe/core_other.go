//go:build !linux

package main

func disableCoreDumps() bool { return false }
