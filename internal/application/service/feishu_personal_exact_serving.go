package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

type exactPublishedFileKey struct{}

// Request-local checked bytes, never an authorization cache. Only a successful
// full local published read may replace this snapshot; every file consumer revalidates.
type exactPublishedFileSnapshot struct {
	mu        sync.Mutex
	knowledge string
	filename  string
	data      []byte
}

// Serve one published local document for a live KnowHub user or user-owned MCP Key.
// It grants neither a KB body read nor access to unrelated historical content.
func (p *PersonalRecovery) exactPublishedReadContext(ctx context.Context, knowledgeID string) (context.Context, error) {
	if _, _, bound, _, exact := types.ExactPublishedDocumentIdentity(ctx); exact && bound != knowledgeID {
		return ctx, types.ErrExactDocumentScope
	}
	principal, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	caller := types.CallerFromContext(ctx)
	if !ok || principal.Type != types.PrincipalWebUser || principal.ID == "" ||
		caller.UserID != principal.ID || caller.TenantID == 0 || knowledgeID == "" {
		return ctx, access.ErrUnauthorized
	}
	var rows []types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where("scope=? AND scope_id=?",
		types.PersonalExactReceiptScope, knowledgeID).Limit(2).Find(&rows).Error; err != nil {
		return ctx, err
	}
	var receipt types.PersonalExactReceipt
	if len(rows) != 1 || rows[0].Op != "published" || json.Unmarshal(rows[0].Payload, &receipt) != nil ||
		receipt.KnowledgeID != knowledgeID || rows[0].TenantID != receipt.TenantID {
		return ctx, errPersonalExactDocument
	}
	keyScope, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	receiptID := rows[0].ID
	file := &exactPublishedFileSnapshot{knowledge: knowledgeID}
	live := func(current context.Context) error {
		localStarted := time.Now()
		if tenant, ok := types.TenantIDFromContext(current); !ok || tenant != receipt.TenantID {
			return access.ErrForbidden
		}
		currentPrincipal, ok := current.Value(types.PrincipalContextKey).(types.Principal)
		if !ok || currentPrincipal != principal || types.CallerFromContext(current) != caller {
			return access.ErrUnauthorized
		}
		currentKey, currentKeyed := types.TenantAPIKeyScopeFromContext(current)
		if currentKeyed != keyed || !reflect.DeepEqual(currentKey, keyScope) {
			return access.ErrUnauthorized
		}
		kb, err := p.checkExactPublishedViewer(current, receipt)
		if err != nil {
			return err
		}
		logger.Infof(current, "Personal exact viewer timing: knowledge_id=%s elapsed_ms=%d",
			knowledgeID, time.Since(localStarted).Milliseconds())
		source, err := p.prepareExactPublishedSnapshot(current, receipt)
		if err != nil || source.kb != kb.ID {
			if err != nil {
				return err
			}
			return errPersonalExactDocument
		}
		var currentReceipt types.TaskPendingOp
		if err = p.service.db.WithContext(current).Where("id=? AND scope=? AND scope_id=?",
			receiptID, types.PersonalExactReceiptScope, knowledgeID).Take(&currentReceipt).Error; err != nil {
			return err
		}
		var stored types.PersonalExactReceipt
		if currentReceipt.TenantID != receipt.TenantID || currentReceipt.Op != "published" ||
			json.Unmarshal(currentReceipt.Payload, &stored) != nil ||
			stored != receipt {
			return errPersonalExactDocument
		}
		receiptStarted := time.Now()
		receiptErr := p.verifyExactPublishedDatabase(current, receipt)
		logger.Infof(current, "Personal exact receipt timing: knowledge_id=%s elapsed_ms=%d verified=%t",
			knowledgeID, time.Since(receiptStarted).Milliseconds(), receiptErr == nil)
		if receiptErr != nil {
			return receiptErr
		}
		file.mu.Lock()
		file.filename, file.data = source.filename, source.file
		file.mu.Unlock()
		return nil
	}
	// Issue the ordinary canonical KB grant for downstream repository checks;
	// it remains separate from source proof and does not release the KB profile.
	kb, err := p.loadAuthorizationKB(ctx, receipt.KnowledgeBaseID)
	if err != nil {
		return ctx, err
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleViewer, p.shares, nil)
	if err != nil {
		return ctx, err
	}
	ctx = types.WithExecutionTenant(grant.Context(ctx), receipt.TenantID)
	guarded, err := types.WithExactPublishedDocumentRead(ctx, receiptID, types.ExactDocumentScope{
		TenantID: receipt.TenantID, KnowledgeID: knowledgeID, KBID: kb.ID, Read: live,
	})
	if err != nil {
		return ctx, err
	}
	if err = access.RetainExactPublishedReadOutput(guarded); err != nil {
		return ctx, err
	}
	return context.WithValue(guarded, exactPublishedFileKey{}, file), nil
}

// No document/chunk/vector body is selected by this verification. A changed
// or unreceipted row fails the database predicate before content retrieval.
func (p *PersonalRecovery) verifyExactPublishedDatabase(ctx context.Context, receipt types.PersonalExactReceipt) error {
	var chunks, vectors map[string]string
	if len(receipt.ChunksDBManifest) > 1<<20 || len(receipt.VectorsDBManifest) > 1<<20 ||
		json.Unmarshal([]byte(receipt.ChunksDBManifest), &chunks) != nil ||
		json.Unmarshal([]byte(receipt.VectorsDBManifest), &vectors) != nil ||
		len(chunks) == 0 || len(vectors) != receipt.VectorRows || receipt.TextChunks <= 0 {
		return errPersonalExactDocument
	}
	for _, manifest := range []map[string]string{chunks, vectors} {
		for id, hash := range manifest {
			decoded, err := hex.DecodeString(hash)
			if id == "" || err != nil || len(decoded) != 32 {
				return errPersonalExactDocument
			}
		}
	}
	var valid bool
	err := p.service.db.WithContext(ctx).Raw(`SELECT EXISTS(
SELECT 1 FROM knowledges k JOIN feishu_personal_resources r ON r.logical_knowledge_id=k.id
JOIN feishu_personal_attempts a ON a.id=r.current_attempt_id
JOIN feishu_personal_imports i ON i.id=a.import_id
WHERE k.id=? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL
AND k.parse_status='completed' AND k.enable_status='enabled'
AND r.import_id=? AND r.current_attempt_id=? AND r.generation=? AND a.fence=r.generation
AND r.active_version_id=? AND r.candidate_version_id=r.active_version_id
AND a.status='indexed' AND a.owner_nonce='' AND a.lease_until IS NULL
AND a.grant_epoch=? AND a.app_config_version=? AND a.revision=?
AND i.id=? AND i.knowledge_id=k.id AND i.kb_id=k.knowledge_base_id
AND i.app_id || ':' || i.open_id=?
AND encode(sha256(convert_to((to_jsonb(k)-'created_at'-'updated_at')::text,'UTF8')),'hex')=?
AND (SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id)=?
AND NOT EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id AND
 COALESCE((?::jsonb)->>c.id,'')<>encode(sha256(convert_to(
 (to_jsonb(c)-'created_at'-'updated_at'-'seq_id')::text,'UTF8')),'hex'))
AND (SELECT count(*) FROM embeddings e WHERE e.knowledge_id=k.id)=?
AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE e.knowledge_id=k.id AND
 COALESCE((?::jsonb)->>e.source_id,'')<>encode(sha256(convert_to(
 (to_jsonb(e)-'created_at'-'updated_at'-'id')::text,'UTF8')),'hex'))
)`, receipt.KnowledgeID, receipt.TenantID, receipt.KnowledgeBaseID, receipt.ImportID, receipt.AttemptID,
		receipt.Generation, receipt.CandidateVersionID, receipt.GrantEpoch, receipt.AppConfigVersion, receipt.Revision,
		receipt.ImportID, receipt.ConnectionIdentity, receipt.KnowledgeDBSHA256,
		len(chunks), receipt.ChunksDBManifest, len(vectors), receipt.VectorsDBManifest).Scan(&valid).Error
	if err != nil {
		return err
	}
	if !valid {
		return errPersonalExactDocument
	}
	return nil
}
