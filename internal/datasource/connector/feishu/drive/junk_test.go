package drive

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/stretchr/testify/require"
)

func TestSystemJunkFilesDoNotIssueFetches(t *testing.T) {
	for _, name := range []string{".DS_Store", "Thumbs.db", "desktop.ini", "._report.docx", "~$report.xlsx"} {
		t.Run(name, func(t *testing.T) {
			require.True(t, isSystemJunkFileName(name))
			items, err := (driveOps{}).Fetch(t.Context(), nil, core.DriveFile{
				Type: "file", Name: name,
			}, "root", false)
			require.NoError(t, err)
			require.Empty(t, items)
		})
	}
	for _, name := range []string{"report.xlsx", "DS_Store.docx", "notes.md", "budget~.xlsx"} {
		require.False(t, isSystemJunkFileName(name))
	}
}
