package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestIncrementalFeishuDeletionFailureDoesNotConfirmCursor(t *testing.T) {
	for _, kind := range []string{"feishu", "feishu_drive", "lark", "lark_drive"} {
		for _, phase := range []string{"lookup", "delete", "hard-delete"} {
			t.Run(kind+"/"+phase, func(t *testing.T) {
				cause := errors.New("synthetic storage failure")
				repo := &deletionLookupKnowledgeRepo{knowledge: &types.Knowledge{ID: "stable-kid"}}
				ks := &sweepFakeKS{repo: repo}
				switch phase {
				case "lookup":
					repo.lookupErr = cause
				case "delete":
					ks.deleteErr = cause
				case "hard-delete":
					repo.hardDeleteErr = cause
				}
				svc := &DataSourceService{knowledgeService: ks}
				result := &types.SyncResult{}
				err := svc.applyFetchedItem(context.Background(), &types.DataSource{ID: "source", TenantID: 1, KnowledgeBaseID: "kb", Type: kind, SyncDeletions: true}, &types.FetchedItem{ExternalID: "remote", IsDeleted: true}, nil, result)
				var unaccepted *dataSourceIngestError
				require.ErrorAs(t, err, &unaccepted, "failed deletion must latch Emit failure before the connector advances cursor")
				require.ErrorIs(t, err, cause)
				require.Equal(t, 1, result.DeletionFailed)
				require.Zero(t, result.Deleted)
			})
		}
	}
}
