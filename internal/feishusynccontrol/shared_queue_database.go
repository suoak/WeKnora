package feishusynccontrol

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// SharedQueueProofFromDatabase reads only source/KB/run metadata. It does not
// read private content or invent a caller grant. The original durable payload
// and its initiator remain unchanged, and execution must still obtain Claim.
func SharedQueueProofFromDatabase(
	ctx context.Context, db *gorm.DB, p types.DataSourceSyncPayload,
) (SharedQueueTaskProof, error) {
	var proof SharedQueueTaskProof
	var log types.SyncLog
	if p.TenantID == 0 || p.DataSourceID == "" || p.SyncLogID == "" {
		return proof, ErrRecoveryRejected
	}
	err := db.WithContext(ctx).Select("id,data_source_id,tenant_id,status,execution_generation,execution_claimed").
		Where("id=? AND data_source_id=? AND tenant_id=?", p.SyncLogID, p.DataSourceID, p.TenantID).Take(&log).Error
	if err != nil || log.ExecutionClaimed {
		return proof, ErrRecoveryRejected
	}
	proof = SharedQueueTaskProof{
		SourceID: p.DataSourceID, RunID: p.SyncLogID, TenantID: p.TenantID,
		Status: log.Status, Generation: log.ExecutionGeneration,
	}
	switch log.Status {
	case "success", "partial", "failed", "canceled", "deferred":
		proof.Disposition = "acknowledge_terminal"
		return proof, nil
	case "running":
	default:
		return proof, ErrRecoveryRejected
	}
	// The first release is deliberately narrow: only the ordinary GitLab jobs
	// already blocked by the Feishu-wide pause. Other nonterminal types reject.
	var source types.DataSource
	err = db.WithContext(ctx).Select("id,tenant_id,knowledge_base_id,type,status,legacy_access_state").
		Where("id=? AND tenant_id=?", p.DataSourceID, p.TenantID).Take(&source).Error
	if err != nil || source.Type != types.ConnectorTypeGitLab || source.Status != types.DataSourceStatusActive ||
		(source.LegacyAccessState != "unmanaged" && source.LegacyAccessState != "active") ||
		log.ExecutionGeneration != 0 {
		return proof, ErrRecoveryRejected
	}
	var count int64
	err = db.WithContext(ctx).Model(&types.KnowledgeBase{}).
		Where("id=? AND tenant_id=?", source.KnowledgeBaseID, p.TenantID).Count(&count).Error
	if err != nil || count != 1 {
		return proof, ErrRecoveryRejected
	}
	proof.SourceType, proof.Disposition = source.Type, "execute_original"
	return proof, nil
}
