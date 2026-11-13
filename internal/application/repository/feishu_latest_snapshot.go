package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/feishudocx"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// LegacyLatestSnapshotBaseline binds a fresh observation to a locked resource and execution.
// Callers cannot synthesize its private fields from JSON.
type LegacyLatestSnapshotBaseline struct {
	resource  types.FeishuLegacyResource
	execution types.SyncExecution
	issued    time.Time
}

// LegacyLatestSnapshotProof carries the validated latest content identity for an attempt.
type LegacyLatestSnapshotProof struct {
	baseline LegacyLatestSnapshotBaseline
	hash     string
	revision string
}

// LatestSnapshotBaseline captures the current resource generation before fetching proof.
func (s *FeishuLegacyStore) LatestSnapshotBaseline(
	ctx context.Context,
	ds *types.DataSource,
	externalID string,
) (*LegacyLatestSnapshotBaseline, error) {
	b := &LegacyLatestSnapshotBaseline{issued: time.Now().UTC()}
	var ok bool
	b.execution, ok = types.SyncExecutionFromContext(ctx)
	if !ok {
		return nil, legacyFence("execution_context_invalid")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.execution(tx, ctx, ds); err != nil {
			return err
		}
		return tx.Where("id=? AND data_source_id=? AND tenant_id=? AND knowledge_base_id=?",
			legacyResourceID(ds, externalID), ds.ID, ds.TenantID, ds.KnowledgeBaseID).
			First(&b.resource).
			Error
	})
	return b, err
}

// MatchLatestFetchedSnapshot compares complete package content and source names,
// never extracted text alone. The second input must come from CurrentSnapshot.
func MatchLatestFetchedSnapshot(original, current *types.FetchedItem) error {
	if original == nil || current == nil || original.ExternalID == "" ||
		original.ExternalID != current.ExternalID ||
		original.IsDeleted ||
		current.IsDeleted ||
		len(original.Content) == 0 ||
		len(current.Content) == 0 ||
		!original.UpdatedAt.Equal(current.UpdatedAt) ||
		original.FileName != current.FileName ||
		original.Title != current.Title {
		return legacyFence("latest_source_snapshot_changed")
	}
	if original.Metadata["obj_type"] != current.Metadata["obj_type"] ||
		original.Metadata["obj_token"] != current.Metadata["obj_token"] ||
		original.Metadata["attachment"] == "true" ||
		current.Metadata["attachment"] == "true" {
		return legacyFence("latest_source_binding_changed")
	}
	if original.SourceResourceID != current.SourceResourceID || original.URL != current.URL {
		return legacyFence("latest_source_binding_changed")
	}
	for _, key := range []string{"node_token", "space_id"} {
		if original.Metadata[key] != current.Metadata[key] {
			return legacyFence("latest_source_binding_changed")
		}
	}
	if LegacySourceHash(
		original.Content,
		original.FileName,
		original.Title,
	) == LegacySourceHash(
		current.Content,
		current.FileName,
		current.Title,
	) {
		return nil
	}
	var first, second *types.FeishuLegacySourceIdentity
	var err error
	switch original.Metadata["obj_type"] {
	case "docx":
		first, err = LegacyDOCXSourceIdentity(original.Content, original.FileName, original.Title)
		if err == nil {
			second, err = LegacyDOCXSourceIdentity(current.Content, current.FileName, current.Title)
		}
	case "sheet", "bitable":
		first, err = LegacyXLSXSourceIdentity(original.Content, original.FileName, original.Title)
		if err == nil {
			second, err = LegacyXLSXSourceIdentity(current.Content, current.FileName, current.Title)
		}
	default:
		return legacyFence("latest_source_snapshot_changed")
	}
	if err != nil {
		return err
	}
	if !legacyIdentityEqual(first, second) {
		if kind := original.Metadata["obj_type"]; kind == "docx" || kind == "sheet" || kind == "bitable" {
			digest := feishudocx.CanonicalXLSXSnapshotDigest
			if kind == "docx" {
				digest = feishudocx.CanonicalDOCXSnapshotDigest
			}
			left, _, leftErr := digest(original.Content)
			right, _, rightErr := digest(current.Content)
			if leftErr != nil {
				return leftErr
			}
			if rightErr != nil {
				return rightErr
			}
			if left == right {
				return nil
			}
		}
		return legacyFence("latest_source_snapshot_changed")
	}
	return nil
}

// ProveCurrent validates fetched content against the captured source observation.
func (b *LegacyLatestSnapshotBaseline) ProveCurrent(
	original, current *types.FetchedItem,
) (*LegacyLatestSnapshotProof, error) {
	if b == nil || original == nil || b.resource.ExternalID != original.ExternalID ||
		time.Since(b.issued) > 5*time.Minute {
		return nil, legacyFence("latest_source_baseline_expired")
	}
	if err := MatchLatestFetchedSnapshot(original, current); err != nil {
		return nil, err
	}
	return &LegacyLatestSnapshotProof{
		baseline: *b,
		hash:     LegacySourceHash(original.Content, original.FileName, original.Title),
		revision: original.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (p *LegacyLatestSnapshotProof) check(
	ctx context.Context,
	r *types.FeishuLegacyResource,
	ds *types.DataSource,
	item *types.FetchedItem,
	hash string,
) error {
	if p == nil {
		return legacyFence("latest_source_proof_required")
	}
	execution, ok := types.SyncExecutionFromContext(ctx)
	b := p.baseline.resource
	if !ok || execution != p.baseline.execution || time.Since(p.baseline.issued) > 5*time.Minute ||
		r.ID != b.ID ||
		r.Generation != b.Generation ||
		r.CurrentAttempt != b.CurrentAttempt ||
		r.ActiveAttempt != b.ActiveAttempt ||
		r.SourceRevision != b.SourceRevision ||
		r.State != b.State ||
		r.DataSourceID != ds.ID ||
		r.TenantID != ds.TenantID ||
		r.KnowledgeBaseID != ds.KnowledgeBaseID ||
		r.ExternalID != item.ExternalID ||
		p.hash != hash ||
		p.revision != item.UpdatedAt.UTC().Format(time.RFC3339Nano) {
		return legacyFence("latest_source_baseline_changed")
	}
	return nil
}

// BeginLatestSourceSnapshot starts an attempt only after rechecking its source proof.
func (s *FeishuLegacyStore) BeginLatestSourceSnapshot(
	ctx context.Context,
	ds *types.DataSource,
	item *types.FetchedItem,
	raw string,
	identity *types.FeishuLegacySourceIdentity,
	latest *LegacyLatestSnapshotProof,
) (*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, error) {
	if latest == nil {
		return nil, nil, errors.New("latest source proof required")
	}
	return s.beginWithSourceIdentity(ctx, ds, item, raw, identity, nil, latest)
}
