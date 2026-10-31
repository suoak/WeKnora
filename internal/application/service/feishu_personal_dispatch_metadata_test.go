package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalRecoveryDispatchMetadataRejectsChangedIdentity(t *testing.T) {
	for _, change := range []string{"generation", "attempt", "manifest", "file", "member"} {
		t.Run(change, func(t *testing.T) {
			f := newMVPFixture(t)
			m := f.prepare("https://tenant.feishu.cn/file/real")
			require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			k, op, _ := mvpPublicTask(t, f, m)
			var intent types.KnowledgeDispatch
			require.NoError(t, json.Unmarshal(op.Payload, &intent))
			switch change {
			case "generation":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE feishu_personal_resources SET generation=generation+1 WHERE import_id=?",
						m.ID,
					).Error,
				)
			case "attempt":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE feishu_personal_resources SET current_attempt_id='different' WHERE import_id=?",
						m.ID,
					).Error,
				)
			case "manifest":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE knowledges SET metadata="+
							"jsonb_set(metadata::jsonb,'{feishu_manifest_sha256}','\"changed\"') WHERE id=?",
						k.ID,
					).Error,
				)
			case "file":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE knowledges SET file_path='local://different' WHERE id=?",
						k.ID,
					).Error,
				)
			case "member":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE tenant_members SET status='inactive' WHERE user_id='A'",
					).Error,
				)
			}
			ready, err := f.p.firstCreateDispatchable(context.Background(), intent)
			require.Error(t, err)
			require.False(t, ready)
			_, _, a, err := f.p.repo.Load(context.Background(), m.AttemptID)
			if err == nil {
				require.Empty(t, a.OwnerNonce)
				require.Nil(t, a.LeaseUntil)
			}
		})
	}
}
