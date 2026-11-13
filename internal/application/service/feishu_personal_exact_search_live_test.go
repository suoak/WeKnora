package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func exactSearchLiveFixture(t *testing.T) *PersonalRecovery {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE task_pending_ops (
id INTEGER PRIMARY KEY,scope TEXT,scope_id TEXT,tenant_id INTEGER,payload JSON);
CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,
parse_status TEXT,deleted_at DATETIME);
INSERT INTO knowledges VALUES('deleted',1,'kb','deleting',CURRENT_TIMESTAMP),
('normal',1,'kb','completed',NULL),('live',1,'kb','completed',NULL);
INSERT INTO task_pending_ops VALUES(1,'personal_exact_receipt_v1','deleted',1,
'{"knowledge_base_id":"kb"}');`).Error)
	return &PersonalRecovery{service: &FeishuPersonalService{db: db}}
}

func TestExactSearchSkipsDeletedIMReceipts(t *testing.T) {
	p := exactSearchLiveFixture(t)
	s := &knowledgeBaseService{personalRecovery: p}
	ctx := types.WithPrincipal(types.WithCaller(t.Context(), types.Caller{
		TenantID: 1, UserID: "system-1", Role: types.TenantRoleViewer,
	}), types.Principal{Type: types.PrincipalIMUser, ID: "1:channel:feishu:external-user"})
	for _, kbIDs := range [][]string{{"kb"}, {"kb", "other"}} {
		handled, rows, err := s.exactRecoveryHybridSearch(ctx, "kb", types.SearchParams{KnowledgeBaseIDs: kbIDs})
		require.NoError(t, err)
		require.False(t, handled, "a deleted personal import must leave ordinary search routing intact")
		require.Nil(t, rows, "this routing check grants no document read")
	}
	pageService := &knowledgeService{personalRecovery: p}
	page, handled, err := pageService.exactPublicKnowledgePage(ctx, "kb",
		&types.Pagination{Page: 1, PageSize: 10}, types.KnowledgeListFilter{})
	require.NoError(t, err)
	require.False(t, handled, "deleted receipts must also leave ordinary catalog counting intact")
	require.Nil(t, page)
	// A live receipt still selects the exact path. Missing authorization data
	// in this minimal fixture must refuse it, never fall back to ordinary reads.
	require.NoError(t, p.service.db.Exec(`INSERT INTO task_pending_ops VALUES(
2,'personal_exact_receipt_v1','live',1,'{"knowledge_base_id":"kb"}')`).Error)
	handled, rows, err := s.exactRecoveryHybridSearch(ctx, "kb", types.SearchParams{KnowledgeBaseIDs: []string{"kb"}})
	require.True(t, handled)
	require.Error(t, err)
	require.Nil(t, rows)
}

func TestExactSearchLiveReceiptBindings(t *testing.T) {
	p := exactSearchLiveFixture(t)
	count := func(knowledgeIDs []string) int64 {
		t.Helper()
		var n int64
		require.NoError(t, p.liveExactSearchReceipts(t.Context(), []string{"kb"}, knowledgeIDs).Count(&n).Error)
		return n
	}
	require.Zero(t, count(nil))
	require.NoError(t, p.service.db.Exec(`INSERT INTO task_pending_ops VALUES
(2,'personal_exact_receipt_v1','missing',1,'{"knowledge_base_id":"kb"}'),
(3,'personal_exact_receipt_v1','live',2,'{"knowledge_base_id":"kb"}'),
(4,'personal_exact_receipt_v1','live',1,'{"knowledge_base_id":"other"}')`).Error)
	require.Zero(t, count(nil), "missing documents and mismatched tenant/KB bindings stay outside the route")
	require.NoError(t, p.service.db.Exec(`INSERT INTO task_pending_ops VALUES
(5,'personal_exact_receipt_v1','live',1,'{"knowledge_base_id":"kb"}')`).Error)
	require.EqualValues(t, 1, count(nil))
	require.Zero(t, count([]string{"normal", "missing"}), "receipt outside selected documents is irrelevant")
	require.NoError(t, p.service.db.Exec(`UPDATE knowledges SET parse_status='deleting' WHERE id='live'`).Error)
	require.Zero(t, count(nil), "an in-progress deletion is not a searchable publication")
}
