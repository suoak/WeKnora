package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
)

func feishuSourceAdmissionState(ctx context.Context, ds *types.DataSource) error {
	if ds == nil {
		return nil
	}
	switch ds.Type {
	case "feishu", "feishu_drive", "lark", "lark_drive":
		if ds.LegacyAccessState == "revoked" ||
			(ds.LegacyAccessState == "hold" && feishusynccontrol.ReconciliationPolicy(ctx, ds.ID) == "") {
			return &datasource.SyncAdmissionError{
				Reason: "sync_source_protected", ProtectionState: ds.LegacyAccessState,
			}
		}
		if ds.Status == types.DataSourceStatusPaused {
			return &datasource.SyncAdmissionError{Reason: "sync_source_paused"}
		}
	}
	return nil
}
