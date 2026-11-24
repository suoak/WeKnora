//go:build feishu_live_acceptance

package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Both snapshots contain actual, separately observed OAuth consent epochs.
// Only the disposable clone is changed; Feishu calls are permission reads.
func TestPersonalRetryCurrentOAuthLive(t *testing.T) {
	if os.Getenv("FEISHU_PERSONAL_RETRY_LIVE_ACCEPTANCE") != "1" {
		t.Skip("explicit isolated current-OAuth acceptance opt-in required")
	}
	read := func(path string) derivedAcceptanceSnapshot {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		var snapshot derivedAcceptanceSnapshot
		require.NoError(t, json.Unmarshal(data, &snapshot))
		require.EqualValues(t, 10000, snapshot.Tenant)
		return snapshot
	}
	before := read(os.Getenv("FEISHU_PERSONAL_RETRY_BEFORE_SNAPSHOT"))
	current := read(os.Getenv("FEISHU_DERIVED_SNAPSHOT"))
	require.Equal(t, before.Owner, current.Owner)
	for key, value := range current.Env {
		allowed := strings.HasPrefix(key, "FEISHU_") || strings.HasPrefix(key, "SSRF_") ||
			key == "SYSTEM_AES_KEY" || key == "KNOWHUB_ENVIRONMENT"
		require.True(t, allowed)
		t.Setenv(key, value)
	}
	var previousEpoch []types.FeishuPersonalGrantEpoch
	require.NoError(t, json.Unmarshal(before.Tables["feishu_personal_grant_epochs"], &previousEpoch))
	require.Len(t, previousEpoch, 1)
	db, ks, ctx, knowledge := derivedAcceptanceClone(t, current)
	personal := ks.personalRecovery.(*PersonalRecovery).service
	remote, client, ref, err := personal.ResolveCredentialReference(ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	require.NotEqual(t, previousEpoch[0].Epoch, ref.GrantEpoch, "actual callback must rotate consent")
	oldConnection := connectionID(ref.Kind, ref.OwnerID, ref.AppID, ref.OpenID, previousEpoch[0].Epoch)
	_, _, _, err = personal.ResolveCredentialReference(ctx, types.FeishuUserOAuth, oldConnection)
	require.Error(t, err, "old epoch references must retain their normal fence")
	var imported types.FeishuPersonalImport
	require.NoError(t, db.Where("id=? AND user_id=?", current.Source, current.Owner).Take(&imported).Error)
	resource := core.PersonalResource{Type: imported.ResourceType, Token: imported.DocumentToken}
	resource, err = personal.recovery.resolveResource(remote, &imported, resource)
	require.NoError(t, err)
	for _, action := range []string{"view", "copy", "export"} {
		allowed, permissionErr := client.PersonalResourcePermission(remote, resource, action)
		require.NoError(t, permissionErr)
		require.True(t, allowed, "current same-account consent must retain required source permissions")
	}
	digest := func() string {
		var value string
		require.NoError(t, db.Raw(`SELECT md5(to_jsonb(k)::text||
 COALESCE((SELECT string_agg(to_jsonb(c)::text,'' ORDER BY id) FROM chunks c WHERE knowledge_id=k.id),'')||
 COALESCE((SELECT string_agg(to_jsonb(e)::text,'' ORDER BY id) FROM embeddings e WHERE knowledge_id=k.id),'')||
 COALESCE((SELECT to_jsonb(d)::text FROM data_sources d WHERE d.id=?),'')) FROM knowledges k WHERE k.id=?`,
			current.Source, knowledge.ID).Scan(&value).Error)
		return value
	}
	unchanged := digest()
	batch := types.FeishuBatch{
		ID: uuid.NewString(), OwnerID: current.Owner, TenantID: current.Tenant, KBID: current.KB,
		AuthMode: types.FeishuUserOAuth, ConnectionID: oldConnection, ConfigVersion: ref.ConfigVersion,
		CreatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&batch).Error)
	item := types.FeishuBatchItem{
		ID: uuid.NewString(), BatchID: batch.ID, URL: imported.URL,
		Status: "failed", ErrorCategory: "authorization_expired", UpdatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, personal.RetryBatch(ctx, batch.ID))
	var retried types.FeishuBatch
	require.NoError(t, db.Where("id=?", batch.ID).Take(&retried).Error)
	require.Equal(t, ref.ConnectionID, retried.ConnectionID)
	require.NoError(t, db.Where("id=?", item.ID).Take(&item).Error)
	require.Equal(t, "pending", item.Status)
	require.Equal(t, 1, item.RetryCount)
	require.Equal(t, unchanged, digest(), "published document, vectors and source must remain unchanged")
	// OAuth consent does not itself replace a source binding. Until its actual
	// claimed source worker acknowledges the transition, derived writes must
	// stay closed. Never fabricate a SyncExecution to make this fixture pass.
	var source types.DataSource
	require.NoError(t, db.Where("id=?", current.Source).Take(&source).Error)
	configuration, err := source.ParseConfig()
	require.NoError(t, err)
	sourceReference, err := types.ParseFeishuSourceReference(configuration)
	require.NoError(t, err)
	require.NotNil(t, sourceReference)
	version, managed, err := knowledgeDerivedVersion(ctx, ks.repo, knowledge.ID)
	require.NoError(t, err)
	require.True(t, managed)
	ctx = context.WithValue(ctx, derivedTaskVersionKey{}, version)
	bound, protected, err := ks.beginDerivedSummary(ctx, knowledge.ID, current.Tenant)
	if sourceReference.ConnectionID != ref.ConnectionID {
		require.True(t, protected)
		require.ErrorIs(t, err, repository.ErrDerivedWriteRejected)
		require.Equal(t, unchanged, digest())
		t.Log("unacknowledged source consent remains fenced; no SyncLog or execution identity fabricated")
		return
	}
	require.NoError(t, err)
	require.True(t, protected)
	require.NoError(t, ks.repo.UpdateKnowledgeColumn(bound, knowledge.ID, "description", "isolated serving proof"))
	fresh, protected, err := ks.beginDerivedSummary(ctx, knowledge.ID, current.Tenant)
	require.NoError(t, err)
	require.True(t, protected)
	chunks, err := ks.chunkRepo.ListChunksByKnowledgeID(fresh, current.Tenant, knowledge.ID)
	require.NoError(t, err)
	require.Len(t, chunks, 63)
	served, err := ks.repo.GetKnowledgeByID(fresh, current.Tenant, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, "isolated serving proof", served.Description)
	t.Log("real same-account OAuth rotation, current permission reads and original-batch retry verified in a clone")
}
