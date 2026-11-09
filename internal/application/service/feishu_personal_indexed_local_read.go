package service

import (
	"context"
	"crypto/md5" // Historical local-file identity; SHA256 also validates the bytes.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

type indexedLocalPublication struct {
	PhysicalPath, LocationHash                                               string
	InvalidBindings                                                          bool
	Registry, Binding                                                        json.RawMessage
	ID, TenantID                                                             uint64
	KnowledgeID, KBID, FilePath, FileHash, FileName, AttemptID, ManifestHash string
	KnowledgeHash, ChunkHash, IndexHash                                      string
	TextChunks, IndexRows                                                    int64
	Payload                                                                  json.RawMessage
}

// Historical first-create index receipts predate exact serving receipts.
// Source retry/OAuth state cannot withdraw an already indexed local copy.
// No source token, API, attempt lease or write authority is used here.
func (p *PersonalRecovery) indexedLocalReadContext(
	ctx context.Context, id string,
) (context.Context, bool, error) {
	var headers []struct {
		TenantID        uint64
		KnowledgeBaseID string
	}
	if err := p.service.db.WithContext(ctx).Table("knowledges").Select("tenant_id,knowledge_base_id").
		Where("id=? AND channel=? AND deleted_at IS NULL", id, "feishu_personal").Scan(&headers).Error; err != nil {
		return ctx, true, err
	}
	if len(headers) != 1 {
		return ctx, false, nil
	}
	header := headers[0]
	var receipts int64
	if err := p.service.db.WithContext(ctx).Table("task_pending_ops").
		Where("scope=? AND scope_id=?", personalIndexReceiptScope, id).Count(&receipts).Error; err != nil {
		return ctx, true, err
	}
	if receipts == 0 {
		return ctx, false, nil
	}
	caller := types.CallerFromContext(ctx)
	principal, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !ok || principal.Type != types.PrincipalWebUser || principal.ID == "" || principal.ID != caller.UserID {
		return ctx, true, access.ErrUnauthorized
	}
	view := types.PersonalExactReceipt{TenantID: header.TenantID, KnowledgeBaseID: header.KnowledgeBaseID}
	kb, err := p.checkExactPublishedViewer(ctx, view)
	if err != nil {
		return ctx, true, err
	}
	grant, err := access.ResolveKB(
		ctx,
		access.KBRequest{Caller: caller},
		kb,
		types.OrgRoleViewer,
		p.shares,
		nil,
	)
	if err != nil {
		return ctx, true, err
	}
	ctx = types.WithExecutionTenant(grant.Context(ctx), header.TenantID)
	initial, data, err := p.loadIndexedLocalPublication(ctx, id, view)
	if err != nil {
		return ctx, true, err
	}
	file := &exactPublishedFileSnapshot{knowledge: id, filename: initial.FileName, data: data}
	key, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	live := func(current context.Context) error {
		active, valid := types.TenantIDFromContext(current)
		currentPrincipal, present := current.Value(types.PrincipalContextKey).(types.Principal)
		currentKey, currentKeyed := types.TenantAPIKeyScopeFromContext(current)
		if !valid || active != view.TenantID || !present || currentPrincipal != principal ||
			types.CallerFromContext(
				current,
			) != caller || currentKeyed != keyed || !reflect.DeepEqual(key, currentKey) {
			return access.ErrUnauthorized
		}
		if _, err := p.checkExactPublishedViewer(current, view); err != nil {
			return err
		}
		actual, bytes, err := p.loadIndexedLocalPublication(current, id, view)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, actual) {
			return errPersonalExactDocument
		}
		file.mu.Lock()
		file.data = bytes
		file.mu.Unlock()
		return nil
	}
	guarded, err := types.WithExactPublishedDocumentRead(ctx, int64(initial.ID), types.ExactDocumentScope{
		TenantID: view.TenantID, KnowledgeID: id, KBID: kb.ID, Read: live,
	})
	if err != nil {
		return ctx, true, err
	}
	if err = access.RetainExactPublishedReadOutput(guarded); err != nil {
		return ctx, true, err
	}
	return context.WithValue(guarded, exactPublishedFileKey{}, file), true, nil
}

func (p *PersonalRecovery) loadIndexedLocalPublication(
	ctx context.Context, id string, view types.PersonalExactReceipt,
) (indexedLocalPublication, []byte, error) {
	var rows []indexedLocalPublication
	err := p.service.db.WithContext(ctx).Raw(`SELECT o.id,k.tenant_id,k.id AS knowledge_id,
k.knowledge_base_id AS kb_id,k.file_path,k.file_hash,k.file_name,o.payload,
k.metadata::jsonb->>'feishu_mvp_attempt' AS attempt_id,
k.metadata::jsonb->>'feishu_manifest_sha256' AS manifest_hash,
md5(to_jsonb(k)::text) AS knowledge_hash,
EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL
 AND (c.tenant_id<>k.tenant_id OR c.knowledge_base_id<>k.knowledge_base_id)) AS invalid_bindings,
(SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL
 AND c.chunk_type='text' AND c.is_enabled) AS text_chunks,
(SELECT md5(coalesce(string_agg(md5(to_jsonb(c)::text),'' ORDER BY c.id),''))
 FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL) AS chunk_hash,
(SELECT count(*) FROM embeddings e JOIN chunks c ON c.id=e.chunk_id
 WHERE e.knowledge_id=k.id AND e.knowledge_base_id=k.knowledge_base_id AND e.is_enabled
 AND c.knowledge_id=k.id AND c.tenant_id=k.tenant_id AND c.deleted_at IS NULL
 AND c.chunk_type='text' AND c.is_enabled AND e.embedding IS NOT NULL AND e.dimension>0) AS index_rows,
(SELECT md5(coalesce(string_agg(md5(to_jsonb(e)::text),'' ORDER BY e.chunk_id),''))
 FROM embeddings e WHERE e.knowledge_id=k.id) AS index_hash
FROM knowledges k JOIN task_pending_ops o ON o.scope=? AND o.scope_id=k.id AND o.tenant_id=k.tenant_id
WHERE k.id=? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL
AND k.channel='feishu_personal' AND k.parse_status='completed' AND k.enable_status='enabled'
AND o.op='verified' AND o.dedup_key='index-'||(k.metadata::jsonb->>'feishu_mvp_attempt')
AND NOT EXISTS(SELECT 1 FROM task_pending_ops x WHERE x.scope=? AND x.scope_id=k.id)
LIMIT 2`, personalIndexReceiptScope, id, view.TenantID, view.KnowledgeBaseID, types.PersonalExactReceiptScope).
		Scan(&rows).Error
	if err != nil {
		return indexedLocalPublication{}, nil, err
	}
	if len(rows) != 1 {
		return indexedLocalPublication{}, nil, errPersonalExactDocument
	}
	row := rows[0]
	var receipt struct {
		Version     int    `json:"payload_version"`
		KnowledgeID string `json:"knowledge_id"`
		AttemptID   string `json:"attempt_id"`
		Manifest    string `json:"manifest_sha256"`
		FileHash    string `json:"file_hash"`
		Chunks      int64  `json:"text_chunks"`
		Vectors     bool   `json:"pg_vector_rows_verified"`
		BM25        bool   `json:"bm25_document_readback"`
	}
	if row.ID == 0 || row.InvalidBindings || json.Unmarshal(row.Payload, &receipt) != nil ||
		receipt.Version != 1 || receipt.KnowledgeID != id ||
		receipt.AttemptID == "" || receipt.AttemptID != row.AttemptID || receipt.Manifest != row.ManifestHash ||
		len(receipt.Manifest) != 64 || receipt.FileHash != row.FileHash ||
		receipt.Chunks <= 0 || receipt.Chunks != row.TextChunks || row.IndexRows != row.TextChunks ||
		!receipt.Vectors || !receipt.BM25 {
		return row, nil, errPersonalExactDocument
	}
	handle, valid := types.ParseResourcePath(row.FilePath)
	if !valid {
		return row, nil, errPersonalExactDocument
	}
	var files []types.StoredResource
	err = p.service.db.WithContext(ctx).Where(
		"handle=? AND tenant_id=? AND provider='local' AND state='active' AND deleted_at IS NULL",
		handle, view.TenantID).Limit(2).Find(&files).Error
	if err != nil {
		return row, nil, err
	}
	if len(files) != 1 {
		return row, nil, errPersonalExactDocument
	}
	stored := files[0]
	row.PhysicalPath, row.LocationHash = stored.PhysicalPath, stored.LocationHash
	row.Registry, err = json.Marshal(stored)
	if err != nil {
		return row, nil, err
	}
	if stored.Size <= 0 || stored.Size > 64*1024*1024 ||
		stored.Lifecycle != types.ResourceLifecyclePersistent ||
		stored.ExpiresAt != nil {
		return row, nil, errPersonalExactDocument
	}
	var bindings []types.ResourceBinding
	if err = p.service.db.WithContext(ctx).Where("resource_id=?", stored.ID).Limit(2).Find(&bindings).Error; err != nil {
		return row, nil, err
	}
	if len(bindings) != 1 || bindings[0].TenantID != view.TenantID || bindings[0].OwnerID != id ||
		bindings[0].OwnerType != types.ResourceOwnerKnowledge || bindings[0].Relation != types.ResourceRelationSourceFile {
		return row, nil, errPersonalExactDocument
	}
	ks, ok := p.service.knowledge.(*knowledgeService)
	row.Binding, err = json.Marshal(bindings[0])
	if err != nil {
		return row, nil, err
	}
	if !ok || ks.fileSvc == nil {
		return row, nil, errPersonalExactDocument
	}
	reader, err := ks.fileSvc.GetFile(ctx, stored.PhysicalPath)
	if err != nil {
		return row, nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, stored.Size+1))
	if err = errors.Join(readErr, reader.Close()); err != nil {
		return row, nil, err
	}
	sha, md := sha256.Sum256(data), md5.Sum(data)
	digest := hex.EncodeToString(sha[:])
	if int64(len(data)) != stored.Size || hex.EncodeToString(md[:]) != row.FileHash ||
		digest != receipt.Manifest ||
		(stored.ContentHash != "" && stored.ContentHash != digest) {
		return row, nil, errPersonalExactDocument
	}
	return row, data, nil
}
