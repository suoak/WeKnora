package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoSyncAccessHoldP0(t *testing.T) {
	for _, status := range []string{DataSourceStatusActive, DataSourceStatusError, DataSourceStatusPaused} {
		for _, access := range []string{"", "unmanaged", "active", "hold", "revoked"} {
			t.Run(status+"/"+access, func(t *testing.T) {
				ds := &DataSource{Status: status, LegacyAccessState: access, SyncSchedule: "0 0 0 * * *"}
				want := status != DataSourceStatusPaused && access != "hold" && access != "revoked"
				require.Equal(t, want, ds.AutoSyncEnabled())
				require.Equal(t, access, ds.LegacyAccessState)
			})
		}
	}
}
