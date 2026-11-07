package service

import (
	"context"
	"reflect"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type publishedReadDocument struct {
	ctx    context.Context
	tenant uint64
	kb     string
	check  func() error
}

// A request-local adapter composes independent document reads, never their
// capabilities. No union KB grant or new shared authorization is introduced.
type publishedReadBundle struct {
	recovery  *PersonalRecovery
	original  context.Context
	documents map[string]publishedReadDocument
}

func (p *PersonalRecovery) preparePublishedReadBundle(
	ctx context.Context,
	results []*types.SearchResult,
) (*publishedReadBundle, error) {
	ids := make([]string, 0, len(results))
	seen := make(map[string]string)
	for _, result := range results {
		if result == nil || result.KnowledgeID == "" || result.KnowledgeBaseID == "" {
			return nil, errPersonalExactDocument
		}
		if kb, exists := seen[result.KnowledgeID]; exists {
			if kb != result.KnowledgeBaseID {
				return nil, errPersonalExactDocument
			}
			continue
		}
		seen[result.KnowledgeID] = result.KnowledgeBaseID
		ids = append(ids, result.KnowledgeID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 256 {
		return nil, errPersonalExactDocument
	}
	var count int64
	if err := p.service.db.WithContext(ctx).Table("task_pending_ops").
		Where("scope=? AND scope_id IN ?", types.PersonalExactReceiptScope, ids).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	var metadata []struct {
		ID, KnowledgeBaseID, Channel string
		TenantID                     uint64
		HasReceipt                   bool
	}
	if err := p.service.db.WithContext(ctx).Table("knowledges k").
		Select(`k.id,k.tenant_id,k.knowledge_base_id,k.channel,
EXISTS(SELECT 1 FROM task_pending_ops o WHERE o.scope=? AND o.scope_id=k.id) AS has_receipt`, types.PersonalExactReceiptScope).
		Where("k.id IN ? AND k.deleted_at IS NULL", ids).Scan(&metadata).Error; err != nil {
		return nil, err
	}
	if len(metadata) != len(ids) {
		return nil, errPersonalExactDocument
	}
	b := &publishedReadBundle{recovery: p, original: ctx, documents: make(map[string]publishedReadDocument)}
	for _, row := range metadata {
		if seen[row.ID] != row.KnowledgeBaseID {
			return nil, errPersonalExactDocument
		}
		doc := publishedReadDocument{tenant: row.TenantID, kb: row.KnowledgeBaseID}
		if row.HasReceipt {
			read, err := p.exactPublishedReadContext(ctx, row.ID)
			if err != nil {
				return nil, err
			}
			doc.ctx = read
			doc.check = func() error { _, err := types.CheckExactPublishedDocumentRead(read); return err }
		} else {
			// This adapter may combine local uploads with exact publications;
			// it cannot attest unreceipted personal or external-source history.
			if row.Channel != "web" && row.Channel != "api" && row.Channel != "manual" {
				return nil, errPersonalExactDocument
			}
			receipt := types.PersonalExactReceipt{TenantID: row.TenantID, KnowledgeBaseID: row.KnowledgeBaseID}
			kb, err := p.checkExactPublishedViewer(ctx, receipt)
			if err != nil {
				return nil, err
			}
			grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
				kb, types.OrgRoleViewer, p.shares, nil)
			if err != nil {
				return nil, err
			}
			read := types.WithExecutionTenant(grant.Context(ctx), row.TenantID)
			knowledge := row.ID
			doc.ctx = read
			doc.check = func() error {
				if _, err := p.checkExactPublishedViewer(read, receipt); err != nil {
					return err
				}
				return types.CheckLegacySourceRead(p.service.db.WithContext(read), knowledge, kb.ID)
			}
		}
		if err := doc.check(); err != nil {
			return nil, err
		}
		b.documents[row.ID] = doc
	}
	return b, nil
}

func (b *publishedReadBundle) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, ok := types.PrincipalFromContext(ctx)
	original, valid := types.PrincipalFromContext(b.original)
	k, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	okey, okeyed := types.TenantAPIKeyScopeFromContext(b.original)
	if !ok || !valid || p != original || types.CallerFromContext(ctx) != types.CallerFromContext(b.original) ||
		keyed != okeyed ||
		!reflect.DeepEqual(k, okey) {
		return access.ErrUnauthorized
	}
	for _, doc := range b.documents {
		if err := doc.check(); err != nil {
			return err
		}
	}
	return nil
}

type publishedBundleChunks struct {
	interfaces.ChunkRepository
	bundle *publishedReadBundle
}

func (r *publishedBundleChunks) ListChunksByIDOnly(ctx context.Context, ids []string) ([]*types.Chunk, error) {
	if err := r.bundle.validate(ctx); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 4096 {
		return nil, errPersonalExactDocument
	}
	unique := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			return nil, errPersonalExactDocument
		}
		unique[id] = true
	}
	var metadata []struct {
		ID, KnowledgeID, KnowledgeBaseID string
		TenantID                         uint64
	}
	if err := r.bundle.recovery.service.db.WithContext(ctx).Table("chunks").
		Select("id,knowledge_id,knowledge_base_id,tenant_id").
		Where("id IN ? AND deleted_at IS NULL", ids).Scan(&metadata).Error; err != nil {
		return nil, err
	}
	if len(metadata) != len(unique) {
		return nil, errPersonalExactDocument
	}
	groups := make(map[string][]string)
	for _, row := range metadata {
		doc, ok := r.bundle.documents[row.KnowledgeID]
		if !ok || doc.tenant != row.TenantID || doc.kb != row.KnowledgeBaseID {
			return nil, errPersonalExactDocument
		}
		groups[row.KnowledgeID] = append(groups[row.KnowledgeID], row.ID)
	}
	var out []*types.Chunk
	for knowledge, group := range groups {
		doc := r.bundle.documents[knowledge]
		rows, err := r.ChunkRepository.ListChunksByID(doc.ctx, doc.tenant, group)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.KnowledgeID != knowledge || row.TenantID != doc.tenant || row.KnowledgeBaseID != doc.kb ||
				!row.IsEnabled ||
				row.DeletedAt.Valid {
				return nil, errPersonalExactDocument
			}
		}
		out = append(out, rows...)
	}
	if err := r.bundle.validate(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *modelService) AuthorizeExactPersonalReadRepository(
	ctx context.Context,
	results []*types.SearchResult,
	chunks interfaces.ChunkRepository,
) (context.Context, interfaces.ChunkRepository, error) {
	if s.personalRecovery == nil || len(results) == 0 {
		return ctx, chunks, nil
	}
	bundle, err := s.personalRecovery.preparePublishedReadBundle(ctx, results)
	if err != nil || bundle == nil {
		return ctx, chunks, err
	}
	return ctx, &publishedBundleChunks{ChunkRepository: chunks, bundle: bundle}, nil
}

func (b *publishedReadBundle) canonicalResults(
	ctx context.Context,
	inputs []*types.SearchResult,
) ([]*types.SearchResult, error) {
	ks := b.recovery.service.knowledge.(*knowledgeService)
	groups := make(map[string][]string)
	for _, input := range inputs {
		if input == nil {
			return nil, errPersonalExactDocument
		}
		groups[input.KnowledgeID] = append(groups[input.KnowledgeID], input.ID)
	}
	owners := make(map[string]*types.Knowledge)
	byID := make(map[string]*types.Chunk)
	for knowledge, ids := range groups {
		doc, ok := b.documents[knowledge]
		if !ok {
			return nil, errPersonalExactDocument
		}
		owner, err := ks.repo.GetKnowledgeByID(doc.ctx, doc.tenant, knowledge)
		if err != nil {
			return nil, err
		}
		owners[knowledge] = owner
		rows, err := ks.chunkRepo.ListChunksByID(doc.ctx, doc.tenant, ids)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.KnowledgeID != knowledge || row.KnowledgeBaseID != doc.kb || row.TenantID != doc.tenant ||
				!row.IsEnabled ||
				row.DeletedAt.Valid {
				return nil, errPersonalExactDocument
			}
			byID[row.ID] = row
		}
	}
	builder := &knowledgeBaseService{}
	canonical := make([]*types.SearchResult, 0, len(inputs))
	for _, input := range inputs {
		row := byID[input.ID]
		if row == nil || row.KnowledgeID != input.KnowledgeID || row.KnowledgeBaseID != input.KnowledgeBaseID {
			return nil, errPersonalExactDocument
		}
		canonical = append(
			canonical,
			builder.buildSearchResult(row, owners[input.KnowledgeID], input.Score, input.MatchType, ""),
		)
	}
	if err := b.validate(ctx); err != nil {
		return nil, err
	}
	return canonical, nil
}
