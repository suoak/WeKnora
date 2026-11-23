package repository

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// VerifyPersonalExactPublishedDatabase checks the same manifests used at the
// serving boundary, without selecting any document, chunk or vector body.
// Callers retain responsibility for principal, source and transaction fencing.
func VerifyPersonalExactPublishedDatabase(
	ctx context.Context, db *gorm.DB, receipt types.PersonalExactReceipt,
) (bool, error) {
	var chunks, vectors map[string]string
	if len(receipt.ChunksDBManifest) > 1<<20 || len(receipt.VectorsDBManifest) > 1<<20 ||
		json.Unmarshal([]byte(receipt.ChunksDBManifest), &chunks) != nil ||
		json.Unmarshal([]byte(receipt.VectorsDBManifest), &vectors) != nil ||
		len(chunks) == 0 || len(vectors) != receipt.VectorRows || receipt.TextChunks <= 0 {
		return false, nil
	}
	for _, manifest := range []map[string]string{chunks, vectors} {
		for id, hash := range manifest {
			decoded, err := hex.DecodeString(hash)
			if id == "" || err != nil || len(decoded) != 32 {
				return false, nil
			}
		}
	}
	var valid bool
	err := db.WithContext(ctx).Raw(`SELECT EXISTS(
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
AND encode(sha256(convert_to((jsonb_set(to_jsonb(k),'{folder_path}',
 COALESCE(?::jsonb,to_jsonb(k)->'folder_path'))-'created_at'-'updated_at')::text,'UTF8')),'hex')=?
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
		receipt.ImportID, receipt.ConnectionIdentity, receipt.KnowledgeFolderJSON(), receipt.KnowledgeDBSHA256,
		len(chunks), receipt.ChunksDBManifest, len(vectors), receipt.VectorsDBManifest).Scan(&valid).Error
	return valid, err
}
