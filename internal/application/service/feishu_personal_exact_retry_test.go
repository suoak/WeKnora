package service

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestPersonalExactRetryBudgetDoesNotRetryAuthorizationOrSafetyFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"success", nil, false},
		{"timeout", context.DeadlineExceeded, true},
		{"grant", core.ErrAuth, false},
		{"permission", core.ErrPermission, false},
		{"fence", repository.ErrPersonalFence, false},
		{"network", &core.APIError{Cause: io.ErrUnexpectedEOF}, true},
		{"rate_limit", &core.APIError{Category: core.ErrorCategoryRateLimit}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newExactRetryController()
			require.True(t, c.begin("one"))
			require.False(t, c.begin("one"), "only one in-flight admission")
			c.end("one", tc.err)
			require.False(t, c.begin("one"), "no immediate retry")
			for i := 0; i < 3; i++ {
				b := c.budgets["one"]
				b.next = time.Time{}
				c.budgets["one"] = b
				if !tc.retry || i == 2 {
					require.False(t, c.begin("one"))
					break
				}
				require.True(t, c.begin("one"))
				c.end("one", tc.err)
			}
		})
	}
}

func TestPersonalExactRetryWindowRequiresExactIDAndExpiringPolicy(t *testing.T) {
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", "one")
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_GENERATIONS", `{"one":5}`)
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_UNTIL", time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339))
	g, _, ok := exactRetryWindow("one")
	require.True(t, ok)
	require.EqualValues(t, 5, g)
	_, _, ok = exactRetryWindow("other")
	require.False(t, ok)
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_UNTIL", time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339))
	_, _, ok = exactRetryWindow("one")
	require.False(t, ok)
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_UNTIL", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))
	_, _, ok = exactRetryWindow("one")
	require.False(t, ok)
}

func TestPersonalExactRetryStageInventoryPostgresRejectsPublishedOrMixedRows(t *testing.T) {
	for _, change := range []string{
		"valid", "enabled_chunk", "enabled_vector", "foreign_chunk", "derived", "external_reference", "published",
	} {
		t.Run(change, func(t *testing.T) {
			db := syncdb.Open(t, "postgres")
			require.NoError(t, db.Exec(`CREATE TABLE knowledges(
id text PRIMARY KEY, tenant_id bigint, knowledge_base_id text,
deleted_at timestamptz, enable_status text,parse_status text,description text,profile jsonb,custom_metadata jsonb);
CREATE TABLE chunks(id text PRIMARY KEY, knowledge_id text,knowledge_base_id text,tenant_id bigint,is_enabled boolean,
deleted_at timestamptz,content_revision bigint,last_editor_id text,chunk_type text,content text,
pre_chunk_id text,next_chunk_id text,parent_chunk_id text,relation_chunks jsonb,indirect_relation_chunks jsonb);
CREATE TABLE embeddings(id bigint PRIMARY KEY,knowledge_id text,knowledge_base_id text,is_enabled boolean,
source_type int,source_id text,chunk_id text,embedding text);
INSERT INTO knowledges VALUES('doc',1,'kb',NULL,'disabled','processing','',NULL,NULL);
INSERT INTO chunks VALUES('chunk','doc','kb',1,false,NULL,0,'','text','unverified old body','','','','[]','[]');
INSERT INTO embeddings VALUES(1,'doc','kb',false,0,'chunk','chunk','[0.1,0.2]');`).Error)
			switch change {
			case "enabled_chunk":
				require.NoError(t, db.Exec("UPDATE chunks SET is_enabled=true").Error)
			case "enabled_vector":
				require.NoError(t, db.Exec("UPDATE embeddings SET is_enabled=true").Error)
			case "foreign_chunk":
				require.NoError(t, db.Exec("UPDATE chunks SET tenant_id=2").Error)
			case "derived":
				require.NoError(t, db.Exec("UPDATE knowledges SET description='private summary'").Error)
			case "published":
				require.NoError(t, db.Exec("UPDATE knowledges SET parse_status='completed'").Error)
			case "external_reference":
				require.NoError(t, db.Exec(
					"INSERT INTO embeddings VALUES(2,'other','other',false,0,'chunk','chunk','[]')",
				).Error)
			}
			source := types.PersonalExactReceipt{TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "doc", Generation: 4}
			stage, err := inspectExactRetryStage(db, source, true)
			if change != "valid" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, stage.Chunks)
			require.EqualValues(t, 1, stage.Vectors)
			require.Len(t, stage.SHA256, 64)
			require.NoError(t, db.Exec("UPDATE chunks SET content='changed stage'").Error)
			changed, err := inspectExactRetryStage(db, source, false)
			require.NoError(t, err)
			require.NotEqual(t, stage.SHA256, changed.SHA256,
				"body changes invalidate admission without reading bodies into the worker")
			t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
			require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
			ciphertext, digest, err := sealExactRetryStage(db,
				types.PersonalExactRetry{Source: source, SealedGeneration: 5}, 12,
				&types.TaskPendingOp{
					ID: 7, Scope: types.PersonalExactReceiptScope,
					Op: "staging", Payload: json.RawMessage(`{}`),
				}, &types.FeishuPersonalAttempt{
					ID: "terminated", Fence: 4, Status: "needs_attention",
					LastErrorCategory: "transport_security_rejected",
				})
			require.NoError(t, err)
			require.NotContains(t, ciphertext, "changed stage")
			plaintext, err := utils.DecryptStoredSecret(ciphertext)
			require.NoError(t, err)
			require.Equal(t, digest, exactRetrySHA([]byte(plaintext)))
			require.Contains(t, plaintext, "transport_security_rejected")
			var archived struct {
				KnowledgeID string            `json:"knowledge_id"`
				TicketID    int64             `json:"ticket_id"`
				Chunks      []json.RawMessage `json:"chunks"`
				Vectors     []json.RawMessage `json:"vectors"`
			}
			require.NoError(t, json.Unmarshal([]byte(plaintext), &archived))
			require.Equal(t, "doc", archived.KnowledgeID)
			require.EqualValues(t, 12, archived.TicketID)
			require.Len(t, archived.Chunks, 1)
			require.Len(t, archived.Vectors, 1)
			b, err := json.Marshal(stage)
			require.NoError(t, err)
			require.NotContains(t, string(b), "unverified old body")
		})
	}
}
