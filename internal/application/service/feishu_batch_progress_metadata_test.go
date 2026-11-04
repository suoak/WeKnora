package service

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuV2PostgresProgressDoesNotRequireDocumentServingGrant(t *testing.T) {
	f := v2PostgresFixture(t)
	m, err := f.s.ImportWithIdentity(f.ctx, "kb", "https://test.feishu.cn/file/real", types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	m, _, _, err = f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	require.NoError(t, f.db.AutoMigrate(&types.DataSource{}))
	require.NoError(t, f.db.Create(&types.DataSource{
		ID: "held-sibling", TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu",
		Status: "error", LegacyAccessState: "hold", Config: types.JSON(`{}`),
	}).Error)
	f.s.db = types.EnableLegacyReadGuards(f.db)
	var body types.Knowledge
	require.ErrorIs(t, f.s.db.WithContext(f.ctx).Where("id=?", m.KnowledgeID).First(&body).Error,
		types.ErrLegacySourceRestricted)
	item := types.FeishuBatchItem{Status: "dispatched", URL: m.URL, ImportID: m.ID}
	require.NoError(t, f.s.batchItemProgress(f.ctx, &item, m, time.Now(), &batchQueueProgress{}))
	require.Equal(t, "dispatched", item.Status)
	require.NotEmpty(t, item.ParseStatus)
	require.False(t, item.IndexVerified, "progress cannot manufacture an index receipt")
	require.Empty(t, item.DocumentName, "metadata polling must not materialize protected document titles")
	require.ErrorIs(t, f.s.db.WithContext(f.ctx).Where("id=?", m.KnowledgeID).First(&body).Error,
		types.ErrLegacySourceRestricted, "content remains protected after progress polling")
	require.NoError(t, f.db.Exec("UPDATE knowledges SET tenant_id=2 WHERE id=?", m.KnowledgeID).Error)
	require.ErrorIs(t, f.s.batchItemProgress(f.ctx, &item, m, time.Now(), &batchQueueProgress{}),
		gorm.ErrRecordNotFound, "a KB ID alone cannot authorize metadata from another tenant")
}
