package repository

import (
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// WikiPersonalPublication proves only already-published local model inputs.
// It grants no source access, execution, viewer identity or content mutation.
// Wiki's caller, private-KB, selection, epoch and output-seal checks still apply.
// A personal channel alone, an indexed status or a legacy receipt is insufficient.
func WikiPersonalPublication(db *gorm.DB, tenant uint64, kb, knowledge string) (string, error) {
	if db.Name() != "postgres" {
		return "", ErrWikiMCPUnverified
	}
	var rows []types.TaskPendingOp
	if err := db.Select("id,tenant_id,scope_id,op,payload").Where(
		"scope=? AND scope_id=? AND tenant_id=? AND length(payload::text)<=?",
		types.PersonalExactReceiptScope, knowledge, tenant, (2<<20)+(32<<10),
	).Limit(2).Find(&rows).Error; err != nil {
		return "", err
	}
	var proof types.PersonalExactReceipt
	if len(rows) != 1 || rows[0].Op != "published" || json.Unmarshal(rows[0].Payload, &proof) != nil ||
		proof.Version != 1 || proof.TenantID != tenant || proof.KnowledgeBaseID != kb ||
		proof.KnowledgeID != knowledge || proof.CandidateVersionID == "" ||
		proof.ExpectedActive != "" || proof.Generation < 1 || proof.TextChunks < 1 || proof.VectorRows < 1 ||
		proof.ImportID == "" || proof.AttemptID == "" || proof.ResourceID == "" || proof.BindingID == "" {
		return "", ErrWikiMCPUnverified
	}
	for _, digest := range []string{
		proof.FileSHA256, proof.ManifestSHA256, proof.ParserOutputSHA256, proof.ChunksSHA256,
		proof.EmbeddingsSHA256, proof.DerivedSHA256, proof.KnowledgeDBSHA256,
	} {
		value, err := hex.DecodeString(digest)
		if err != nil || len(value) != 32 {
			return "", ErrWikiMCPUnverified
		}
	}
	var chunks, vectors map[string]string
	if len(proof.ChunksDBManifest) > 1<<20 || len(proof.VectorsDBManifest) > 1<<20 ||
		json.Unmarshal([]byte(proof.ChunksDBManifest), &chunks) != nil ||
		json.Unmarshal([]byte(proof.VectorsDBManifest), &vectors) != nil ||
		len(chunks) < proof.TextChunks || len(vectors) != proof.VectorRows {
		return "", ErrWikiMCPUnverified
	}
	for _, manifest := range []map[string]string{chunks, vectors} {
		for id, digest := range manifest {
			value, err := hex.DecodeString(digest)
			if id == "" || err != nil || len(value) != 32 {
				return "", ErrWikiMCPUnverified
			}
		}
	}
	var valid bool
	err := db.Raw(`SELECT EXISTS(
SELECT 1 FROM knowledges k
JOIN knowledge_bases kb ON kb.id=k.knowledge_base_id AND kb.tenant_id=k.tenant_id AND kb.deleted_at IS NULL
JOIN tenants t ON t.id=k.tenant_id AND t.status='active' AND t.deleted_at IS NULL
JOIN feishu_personal_resources r ON r.logical_knowledge_id=k.id
JOIN feishu_personal_attempts a ON a.id=r.current_attempt_id AND a.import_id=r.import_id
JOIN feishu_personal_imports i ON i.id=a.import_id AND i.knowledge_id=k.id AND i.kb_id=kb.id
JOIN resources f ON f.id=? AND f.tenant_id=k.tenant_id AND f.handle=?
JOIN resource_bindings b ON b.id=? AND b.resource_id=f.id AND b.tenant_id=k.tenant_id
 AND b.owner_type='knowledge' AND b.owner_id=k.id AND b.relation='source_file'
WHERE k.id=? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL
AND EXISTS(SELECT 1 FROM task_pending_ops current_receipt WHERE current_receipt.id=?
 AND current_receipt.scope=? AND current_receipt.scope_id=k.id AND current_receipt.tenant_id=k.tenant_id
 AND current_receipt.op='published' AND current_receipt.payload::jsonb=?::jsonb)
AND k.channel='feishu_personal' AND k.parse_status='completed' AND k.enable_status='enabled'
AND r.import_id=? AND r.current_attempt_id=? AND r.generation=? AND a.fence=r.generation
AND r.active_version_id=? AND r.candidate_version_id=r.active_version_id
AND a.logical_knowledge_id=k.id AND a.caller_tenant_id=k.tenant_id
AND a.status='indexed' AND a.owner_nonce='' AND a.lease_until IS NULL
AND a.expected_active_version='' AND a.candidate_version_id=r.active_version_id
AND i.auth_mode='user_oauth' AND a.auth_mode='user_oauth'
AND a.grant_epoch=? AND a.app_config_version=? AND a.revision=?
AND i.app_id||':'||i.open_id=? AND a.manifest_hash=?
AND k.file_path=? AND k.file_size=a.manifest_size AND k.file_name=a.manifest_name AND k.file_hash=?
AND f.provider='local' AND f.state='active' AND f.deleted_at IS NULL
AND f.lifecycle='persistent' AND f.expires_at IS NULL AND f.size=k.file_size
AND (COALESCE(f.content_hash,'')='' OR f.content_hash=?)
AND (f.physical_path NOT LIKE '%://%' OR f.physical_path LIKE 'local://%')
AND (SELECT count(*) FROM resource_bindings all_b WHERE all_b.resource_id=f.id)=1
AND encode(sha256(convert_to((to_jsonb(k)-'created_at'-'updated_at')::text,'UTF8')),'hex')=?
AND (SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id)=?
AND NOT EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id AND
 COALESCE((?::jsonb)->>c.id,'')<>encode(sha256(convert_to(
 (to_jsonb(c)-'created_at'-'updated_at'-'seq_id')::text,'UTF8')),'hex'))
AND (SELECT count(*) FROM embeddings e WHERE e.knowledge_id=k.id)=?
AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE e.knowledge_id=k.id AND
 COALESCE((?::jsonb)->>e.source_id,'')<>encode(sha256(convert_to(
 (to_jsonb(e)-'created_at'-'updated_at'-'id')::text,'UTF8')),'hex'))
)`, proof.ResourceID, proof.ResourceHandle, proof.BindingID, knowledge, tenant, kb,
		rows[0].ID, types.PersonalExactReceiptScope, string(rows[0].Payload),
		proof.ImportID, proof.AttemptID, proof.Generation, proof.CandidateVersionID,
		proof.GrantEpoch, proof.AppConfigVersion, proof.Revision, proof.ConnectionIdentity, proof.ManifestSHA256,
		types.BuildResourcePath(proof.ResourceHandle), proof.FileMD5, proof.FileSHA256, proof.KnowledgeDBSHA256,
		len(chunks), proof.ChunksDBManifest, len(vectors), proof.VectorsDBManifest).Scan(&valid).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrWikiMCPUnverified
		}
		return "", err
	}
	if !valid {
		return "", ErrWikiMCPUnverified
	}
	return wikiDigest(proof), nil
}
