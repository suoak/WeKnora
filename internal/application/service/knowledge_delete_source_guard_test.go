package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAdmittedDeleteDoesNotRequireFeishuBodyRead(t *testing.T) {
	for _, legacyTask := range []bool{false, true} {
		t.Run(map[bool]string{false: "bound task", true: "legacy task"}[legacyTask], func(t *testing.T) {
			f := newDocumentWriteFixture(t)
			for _, sql := range []string{
				`CREATE TABLE tenants (id integer PRIMARY KEY, status text, deleted_at datetime)`,
				`INSERT INTO tenants(id,status) VALUES (7,'active')`,
				`CREATE TABLE knowledge_bases (id text PRIMARY KEY, tenant_id integer, deleted_at datetime)`,
				`INSERT INTO knowledge_bases(id,tenant_id) VALUES ('kb',7),('other',7)`,
				`CREATE TABLE data_sources (id text PRIMARY KEY, knowledge_base_id text, type text, legacy_access_state text)`,
				`INSERT INTO data_sources VALUES ('held-source','kb','feishu_drive','hold')`,
				`CREATE TABLE feishu_legacy_resources (knowledge_base_id text, knowledge_id text, state text)`,
			} {
				require.NoError(t, f.db.Exec(sql).Error)
			}
			types.EnableLegacyReadGuards(f.db)
			_, err := f.repo.GetKnowledgeBatch(context.Background(), 7, []string{"doc"})
			require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
			payload := types.KnowledgeListDeletePayload{TenantID: 7, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"doc"}}
			if legacyTask {
				payload.KnowledgeBaseID = ""
			}
			require.NoError(t, f.svc.ProcessKnowledgeListDelete(context.Background(), deleteDocumentTask(t, payload)))
			require.NoError(t, f.svc.ProcessKnowledgeListDelete(context.Background(), deleteDocumentTask(t, payload)))
			var count int64
			require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id = ?", "doc").Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id = ?", "other-doc").Count(&count).Error)
			require.EqualValues(t, 1, count)
			var state string
			require.NoError(t, f.db.Raw("SELECT legacy_access_state FROM data_sources WHERE id='held-source'").Scan(&state).Error)
			require.Equal(t, "hold", state)
			require.ErrorIs(t, types.CheckLegacySourceRead(f.db.WithContext(context.Background()), "", "kb"), types.ErrLegacySourceRestricted)
		})
	}
}
