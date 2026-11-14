package container

import (
	"runtime/debug"
	"strings"
)

// A replacement binary can inherit its predecessor's environment. Recovery
// approvals must identify the executable, using legacy configuration only when
// neither build mechanism recorded a revision.
func feishuSyncRuntimeRevision(commit, configured string) string {
	info, _ := debug.ReadBuildInfo()
	return resolveFeishuSyncRevision(commit, configured, info)
}

func resolveFeishuSyncRevision(commit, configured string, info *debug.BuildInfo) string {
	if commit = strings.TrimSpace(commit); commit != "" && commit != "unknown" {
		return commit
	}
	if info != nil {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return "build-unversioned"
}
