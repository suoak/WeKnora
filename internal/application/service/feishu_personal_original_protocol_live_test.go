//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

// Real original bytes + original KB/Knowledge/op identities, with an equivalent
// v3 protocol and synthetic isolated OAuth authority. No production credentials
// or remote authorization are copied. This is not an index/retrieval acceptance.
func TestPersonalOriginalV3ClaimAndExactParse(t *testing.T) {
	runOriginalExactAcceptance(t, nil)
}

// RunOriginalExactQueueAcceptance exposes only the isolated test fixture to an
// external test package, so the real Lite router adapter needs no production refactor.
func RunOriginalExactQueueAcceptance(t *testing.T,
	factory func(asynq.HandlerFunc) interfaces.TaskEnqueuer,
) {
	t.Helper()
	require.NotNil(t, factory)
	runOriginalExactAcceptance(t, factory)
}

func runOriginalExactAcceptance(t *testing.T,
	factory func(asynq.HandlerFunc) interfaces.TaskEnqueuer,
) {
	if os.Getenv("PERSONAL_EXACT_CONSUMER_ACCEPTANCE") == "1" {
		t.Setenv("SYNC_EXECUTION_POSTGRES_PUBLIC_EXTENSIONS", "1")
	}
	const kbID = "d14eb0c0-1ade-46f9-890e-84fa823b335d"
	const knowledgeID = "62b4dfac-8e5c-46c9-be87-86493aca4d40"
	data, err := os.ReadFile(os.Getenv("PERSONAL_EXACT_DOCX_FIXTURE"))
	require.NoError(t, err)
	require.Equal(
		t,
		"58bc916ae4f29f6c1d39fd76913a150a2b312ab3d4e9a9680f83a74e48ae0ff9",
		personalManifestDigest(data, nil),
	)
	f := newMVPFixture(t)
	if os.Getenv("PERSONAL_EXACT_CONSUMER_ACCEPTANCE") == "1" {
		f.p.lease = 2 * time.Minute
	}
	require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET id=? WHERE id='kb'", kbID).Error)
	kb := &types.KnowledgeBase{ID: kbID, TenantID: 1, CreatorID: "A"}
	f.ks.kbService, f.s.kbs = dispatchActualKB{kb: kb}, dispatchActualKB{kb: kb}
	grant, err := access.ResolveKB(
		f.ctx,
		access.KBRequest{Caller: types.CallerFromContext(f.ctx)},
		kb,
		types.OrgRoleEditor,
		nil,
		nil,
	)
	require.NoError(t, err)
	f.ctx = grant.Context(f.ctx)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-A" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = fmt.Fprint(
				w,
				`{"code":0,"data":{"metas":[{"doc_type":"docx","doc_token":"real","title":"original",`+
					`"latest_modify_time":"1"}]}}`,
			)
		case "/open-apis/drive/v1/export_tasks":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"ticket":"ticket"}}`)
		case "/open-apis/drive/v1/export_tasks/ticket":
			_, _ = fmt.Fprint(
				w,
				`{"code":0,"data":{"result":{"job_status":0,"file_token":"download-token",`+
					`"file_name":"original.docx"}}}`,
			)
		case "/open-apis/drive/v1/export_tasks/file/download-token/download":
			_, _ = w.Write(data)
		case "/open-apis/drive/v1/permissions/real/members/auth":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"auth_result":true}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	m, err := f.s.Import(f.ctx, kbID, "https://tenant.feishu.cn/docx/real")
	require.NoError(t, err)
	// Only isolated fixture construction uses SQL to retain the original IDs.
	require.NoError(
		t,
		f.db.Exec(
			"UPDATE feishu_personal_resources SET logical_knowledge_id=? WHERE import_id=?",
			knowledgeID,
			m.ID,
		).Error,
	)
	require.NoError(
		t,
		f.db.Exec(
			"UPDATE feishu_personal_attempts SET logical_knowledge_id=? WHERE id=?",
			knowledgeID,
			m.AttemptID,
		).Error,
	)
	m.LogicalKnowledgeID = knowledgeID
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, op, _ := mvpPublicTask(t, f, m)
	require.Equal(t, knowledgeID, k.ID)
	require.NoError(
		t,
		f.db.AutoMigrate(&types.StoredResource{}, &types.ResourceBinding{}, &types.DataSource{}),
	)
	// Install the production write-fence function and full resource schema.
	// AutoMigrate already created the source state column in this fresh fixture.
	legacyDDL, err := os.ReadFile("../../../migrations/versioned/003036_feishu_legacy_atomic.up.sql")
	require.NoError(t, err)
	legacySQL := strings.Replace(
		string(legacyDDL),
		"ADD COLUMN legacy_access_state",
		"ADD COLUMN IF NOT EXISTS legacy_access_state",
		1,
	)
	require.NoError(t, f.db.Exec(legacySQL).Error)
	require.NoError(
		t,
		f.db.Create(&types.DataSource{
			ID: "held-sibling", TenantID: 1, KnowledgeBaseID: kbID,
			Type: "feishu_drive", Status: "error", LegacyAccessState: "hold", Config: types.JSON(`{}`),
		}).Error,
	)
	file := types.StoredResource{
		ID:           "89b29cb3-d7e5-4a93-a1d5-29099fd8b36b",
		Handle:       "abcdefghijklmnopqrstuv",
		TenantID:     1,
		Provider:     "local",
		PhysicalPath: k.FilePath,
		LocationHash: "isolated-original",
		Size:         int64(len(data)),
		ContentHash:  "",
		State:        "active",
		Lifecycle:    "persistent",
	}
	require.NoError(t, f.db.Create(&file).Error)
	require.NoError(t, f.db.Create(&types.ResourceBinding{
		ResourceID: file.ID, TenantID: 1,
		OwnerType: "knowledge", OwnerID: knowledgeID, Relation: "source_file",
	}).Error)
	ref := types.BuildResourcePath(file.Handle)
	require.NoError(
		t,
		f.db.Exec("UPDATE knowledges SET file_path=? WHERE id=?", ref, knowledgeID).Error,
	)
	var intent types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &intent))
	intent.Task.FilePath = ref
	payload, err := json.Marshal(intent)
	require.NoError(t, err)
	require.NoError(
		t,
		f.db.Exec(
			"UPDATE task_pending_ops SET id=192370,payload=?,op='pending' WHERE id=?",
			string(payload),
			op.ID,
		).Error,
	)
	types.EnableLegacyReadGuards(f.db)
	ready, err := f.p.firstCreateDispatchable(context.Background(), intent)
	require.NoError(t, err)
	require.True(t, ready)
	_, before, _, err := f.p.repo.Load(context.Background(), m.AttemptID)
	require.NoError(t, err)
	require.EqualValues(t, 3, before.Generation)
	if os.Getenv("PERSONAL_EXACT_PUBLIC_DISPATCH_ACCEPTANCE") == "1" {
		exerciseOriginalPublicDispatch(t, f, intent.Task, factory)
		return
	}
	owner, err := f.p.repo.Claim(context.Background(), m.AttemptID, f.p.lease)
	require.NoError(t, err)
	require.EqualValues(t, 4, owner.Fence)
	proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	var parsed *personalExactParsedDocument
	if os.Getenv("PERSONAL_EXACT_CONSUMER_ACCEPTANCE") == "1" {
		parsed = exerciseOriginalExactConsumer(t, f, intent.Task, proof)
	} else {
		parsed, err = f.p.parseExactPersonalDocument(context.Background(), proof)
		require.NoError(t, err)
	}
	require.NotEmpty(t, parsed.outputSHA256)
	require.NotEmpty(t, parsed.markdown)
	// Exact parsing cannot authorize unrelated or whole-KB content reads.
	require.ErrorIs(
		t,
		types.CheckLegacySourceRead(f.db.WithContext(context.Background()), "", kbID),
		types.ErrLegacySourceRestricted,
	)
	require.ErrorIs(
		t,
		types.CheckLegacySourceRead(f.db.WithContext(context.Background()), knowledgeID, kbID),
		types.ErrLegacySourceRestricted,
	)
	record := map[string]any{
		"knowledge_id":                      knowledgeID,
		"knowledge_base_id":                 kbID,
		"pending_operation":                 192370,
		"file_sha256":                       proof.fileSHA256,
		"generation_before_claim":           3,
		"claim_generation":                  owner.Fence,
		"v3_dispatch_verified":              true,
		"claim_verified":                    true,
		"exact_source_attestation_verified": true,
		"anydoc_parse_verified":             true,
		"markdown_runes":                    utf8.RuneCountInString(parsed.markdown),
		"parser_output_sha256":              parsed.outputSHA256,
		"image_assets":                      len(parsed.assets),
		"authorization_provider":            "synthetic isolated OAuth; not production ACL acceptance",
		"legacy_read_protection_preserved":  true,
		"embedding_verified":                false,
		"publication_verified":              false,
		"production_writes":                 false,
	}
	b, err := json.MarshalIndent(record, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(
		filepath.Dir(os.Getenv("PERSONAL_EXACT_EVIDENCE_OUT")),
		"protocol-parser-evidence.json",
	)
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = handle.Write(b)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
}
