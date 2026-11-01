package service

import (
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
)

func feishuSourceAdmissionState(ds *types.DataSource) error {
	if ds == nil {
		return nil
	}
	switch ds.Type {
	case "feishu", "feishu_drive", "lark", "lark_drive":
		if ds.LegacyAccessState == "hold" || ds.LegacyAccessState == "revoked" {
			return &datasource.SyncAdmissionError{Reason: "sync_source_protected"}
		}
		if ds.Status == types.DataSourceStatusPaused {
			return &datasource.SyncAdmissionError{Reason: "sync_source_paused"}
		}
	}
	return nil
}
