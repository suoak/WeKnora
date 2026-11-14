package container

import (
	"runtime/debug"
	"testing"
)

func TestFeishuSyncRevisionUsesExecutableBeforeInheritedEnvironment(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "vcs-build"}}}
	for _, tc := range []struct {
		name, commit, configured, want string
		info                           *debug.BuildInfo
	}{
		{"release archive with stale environment", "actual-release", "previous-release", "actual-release", nil},
		{"release build", "actual-release", "previous-release", "actual-release", info},
		{"vcs build", "unknown", "previous-release", "vcs-build", info},
		{"legacy unversioned executable", "unknown", "legacy-config", "legacy-config", nil},
		{"no revision keeps strict configuration empty", "unknown", "", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveFeishuSyncRevision(tc.commit, tc.configured, tc.info); got != tc.want {
				t.Fatalf("revision = %q, want %q", got, tc.want)
			}
		})
	}
}
