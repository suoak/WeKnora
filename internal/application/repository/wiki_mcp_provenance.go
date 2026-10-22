package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrWikiMCPUnverified = errors.New("wiki provenance requires verified regeneration")

type wikiGenerationProofKey struct{}
type wikiGenerationProof struct {
	mu               sync.Mutex
	Tenant           uint64
	KB               string
	Sources          map[string]string
	Epochs           map[string]string
	DependencySHA256 string
	WikiGeneration   int64
	WikiIncarnation  string
}
type wikiMCPEpoch struct {
	Generation  int64
	Incarnation string
}

func WikiMCPGenerationTenant(ctx context.Context) (uint64, bool) {
	p, ok := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if !ok {
		return 0, false
	}
	return p.Tenant, true
}

func readWikiMCPEpoch(db *gorm.DB, kb string) (wikiMCPEpoch, error) {
	var e wikiMCPEpoch
	err := db.Table("wiki_mcp_kb_epochs").Where("knowledge_base_id=?", kb).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return e, nil
	}
	return e, err
}
func checkWikiMCPEpochCoverage(db *gorm.DB) error {
	var n int64
	switch db.Name() {
	case "postgres":
		if e := db.Raw("SELECT count(*) FROM pg_trigger WHERE tgrelid='wiki_pages'::regclass AND NOT tgisinternal AND tgenabled='O' AND tgname='wiki_mcp_mutation'").Scan(&n).Error; e != nil {
			return e
		}
		if n == 1 {
			return nil
		}
	case "sqlite":
		if e := db.Raw("SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name IN ('wiki_mcp_insert','wiki_mcp_update','wiki_mcp_delete')").Scan(&n).Error; e != nil {
			return e
		}
		if n == 3 {
			return nil
		}
	}
	return ErrWikiMCPUnverified
}
func withWikiMCPProofWrite(ctx context.Context, write func() error) error {
	p, ok := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if !ok {
		return write()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	oldGeneration, oldIncarnation := p.WikiGeneration, p.WikiIncarnation
	err := write()
	if err != nil {
		p.WikiGeneration, p.WikiIncarnation = oldGeneration, oldIncarnation
	}
	return err
}
func beforeWikiMCPMutation(db *gorm.DB, ctx context.Context) error {
	if p, ok := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof); ok {
		if err := checkWikiMCPEpochCoverage(db); err != nil {
			return err
		}
		e, err := readWikiMCPEpoch(db, p.KB)
		if err != nil {
			return err
		}
		if e.Generation != p.WikiGeneration || e.Incarnation != p.WikiIncarnation {
			return ErrWikiMCPUnverified
		}
	}
	return nil
}

// This proof exists only on server-generated Wiki jobs. It cannot be created by
// a caller header. It is the only case that can replace the tenant-wide fallback
// with an exact, fully known input closure at the model network boundary.
func checkWikiMCPModelInputs(db *gorm.DB, ctx context.Context, tenant uint64) (bool, error) {
	p, ok := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if !ok {
		return false, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if tenant != p.Tenant {
		return true, ErrWikiMCPUnverified
	}
	if e := beforeWikiMCPMutation(db.WithContext(ctx), ctx); e != nil {
		return true, e
	}
	fresh, e := wikiDependencies(db.WithContext(ctx), p.KB)
	if e != nil {
		return true, e
	}
	if fresh.DependencySHA256 != p.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(p.Epochs) {
		return true, ErrWikiMCPUnverified
	}
	return true, nil
}

type WikiMCPProvenance struct {
	PageID           string `gorm:"primaryKey"`
	TenantID         uint64
	KnowledgeBaseID  string
	PageSHA256       string
	DependencySHA256 string
	SourceVersions   string
}

func (WikiMCPProvenance) TableName() string { return "wiki_mcp_provenance" }
func wikiDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func wikiPageDigest(p *types.WikiPage) string {
	return wikiDigest([]any{p.ID, p.TenantID, p.KnowledgeBaseID, p.Slug, p.Title, p.Content, p.Summary, p.Status, p.PageType, p.Aliases, p.SourceRefs, p.ChunkRefs, p.OutLinks, p.PageMetadata})
}

// The conservative closure is the ENTIRE input KB, not citations selected by
// an LLM. Existing epoch-backed versions fence generation against all writes,
// including ABA and writes preserving timestamps. Persisted read stamps bind
// actual model input fields, so finalizing -> completed alone is not content
// replacement. Current authorization/source state is always checked separately.
func wikiDependencies(db *gorm.DB, kb string) (*wikiGenerationProof, error) {
	var anchor struct{ TenantID uint64 }
	if e := db.Table("knowledge_bases kb").Select("kb.tenant_id").Joins("JOIN tenants t ON t.id=kb.tenant_id AND t.deleted_at IS NULL AND t.status='active'").Where("kb.id=? AND kb.deleted_at IS NULL AND kb.type='document' AND kb.is_temporary=false", kb).Take(&anchor).Error; e != nil {
		return nil, e
	}
	var denied bool
	if e := db.Raw(`SELECT EXISTS(SELECT 1 FROM data_sources WHERE knowledge_base_id=? AND legacy_access_state<>'active') OR EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_base_id=? AND state<>'active')`, kb, kb).Scan(&denied).Error; e != nil {
		return nil, e
	}
	if denied {
		return nil, types.ErrLegacySourceRestricted
	}
	var docs []struct {
		ID           string
		ParseStatus  string
		EnableStatus string
		Channel      string
	}
	if e := db.Table("knowledges").Select("id,parse_status,enable_status,channel").Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb, anchor.TenantID).Order("id").Limit(2049).Find(&docs).Error; e != nil {
		return nil, e
	}
	if len(docs) == 0 || len(docs) > 2048 {
		return nil, ErrWikiMCPUnverified
	}
	p := &wikiGenerationProof{Tenant: anchor.TenantID, KB: kb, Sources: map[string]string{}, Epochs: map[string]string{}}
	for _, d := range docs {
		switch d.Channel {
		case "", types.ChannelWeb, types.ChannelAPI, types.ChannelBrowserExtension:
			// Local content published under KnowHub's existing KB authority.
		default:
			var verified int64
			if e := db.Table("feishu_legacy_resources").Where("knowledge_id=? AND knowledge_base_id=? AND tenant_id=? AND state='active' AND active_attempt<>''", d.ID, kb, p.Tenant).Count(&verified).Error; e != nil {
				return nil, e
			}
			if verified != 1 {
				return nil, types.ErrLegacySourceRestricted
			}
		}
		if (d.ParseStatus != "completed" && d.ParseStatus != "finalizing") || d.EnableStatus != "enabled" {
			return nil, ErrWikiMCPUnverified
		}
		version, tenant, e := resourceVersion(db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: d.ID})
		if e != nil {
			return nil, e
		}
		if tenant != p.Tenant {
			return nil, ErrWikiMCPUnverified
		}
		p.Epochs[d.ID] = version
		var identity map[string]interface{}
		if e = db.Table("knowledges").Select("id,tenant_id,knowledge_base_id,type,title,description,source,channel,enable_status,file_name,file_type,file_hash,file_path,embedding_model_id").Where("id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", d.ID, p.Tenant, kb).Take(&identity).Error; e != nil {
			return nil, e
		}
		var inputs []map[string]interface{}
		if e = db.Table("chunks").Select("id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_index,chunk_type,context_header,is_enabled,parent_chunk_id,image_info,deleted_at").Where("knowledge_id=? AND chunk_type IN ('','text','parent_text','image_ocr','image_caption')", d.ID).Order("id").Find(&inputs).Error; e != nil {
			return nil, e
		}
		if len(inputs) == 0 {
			return nil, ErrWikiMCPUnverified
		}
		p.Sources[d.ID] = wikiDigest([]any{identity, inputs})
	}
	p.DependencySHA256 = wikiDigest(p.Sources)
	return p, nil
}

// Only the server generation worker calls this before assembling ANY prompts.
// Unsealed historical pages may contain unknown sources: they are not certified
// by reading their current timestamps or by copying SourceRefs after generation.
func BeginWikiMCPGeneration(ctx context.Context, repo interfaces.WikiPageRepository, kb string) (context.Context, error) {
	r, ok := repo.(*wikiPageRepository)
	if !ok {
		return ctx, ErrWikiMCPUnverified
	}
	var proof *wikiGenerationProof
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := checkWikiMCPEpochCoverage(tx); e != nil {
			return e
		}
		epoch, e := readWikiMCPEpoch(tx, kb)
		if e != nil {
			return e
		}
		proof, e = wikiDependencies(tx, kb)
		if e != nil {
			return e
		}
		var old []*types.WikiPage
		if e = tx.Where("knowledge_base_id=?", kb).Limit(1001).Find(&old).Error; e != nil {
			return e
		}
		if len(old) > 1000 {
			return ErrWikiMCPUnverified
		}
		for _, page := range old {
			if e = verifyWikiProof(tx, page, proof, false, nil); e != nil {
				return e
			}
		}
		after, e := readWikiMCPEpoch(tx, kb)
		if e != nil {
			return e
		}
		if after != epoch {
			return ErrWikiMCPUnverified
		}
		proof.WikiGeneration, proof.WikiIncarnation = epoch.Generation, epoch.Incarnation
		return nil
	})
	if e != nil {
		return ctx, e
	}
	return context.WithValue(ctx, wikiGenerationProofKey{}, proof), nil
}
func sealWikiMCPMutation(db *gorm.DB, ctx context.Context, page *types.WikiPage) error {
	p, ok := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if !ok {
		return nil
	}
	if page.KnowledgeBaseID != p.KB || page.TenantID != p.Tenant {
		return ErrWikiMCPUnverified
	}
	if len(page.SourceRefs) == 0 && page.PageType != types.WikiPageTypeIndex {
		return nil
	} // an unknown index introduction stays hidden
	for _, ref := range page.SourceRefs {
		id := types.WikiSourceKnowledgeID(ref)
		if id == "" || p.Sources[id] == "" {
			return ErrWikiMCPUnverified
		}
	}
	fresh, e := wikiDependencies(db, p.KB)
	if e != nil {
		return e
	}
	if fresh.DependencySHA256 != p.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(p.Epochs) {
		return ErrWikiMCPUnverified
	}
	b, e := json.Marshal(p.Sources)
	if e != nil {
		return e
	}
	row := WikiMCPProvenance{PageID: page.ID, TenantID: p.Tenant, KnowledgeBaseID: p.KB, PageSHA256: wikiPageDigest(page), DependencySHA256: p.DependencySHA256, SourceVersions: string(b)}
	epoch, e := readWikiMCPEpoch(db, p.KB)
	if e != nil {
		return e
	}
	if epoch.Generation != p.WikiGeneration+1 || len(epoch.Incarnation) != 32 || p.WikiIncarnation != "" && epoch.Incarnation != p.WikiIncarnation {
		return ErrWikiMCPUnverified
	}
	if e = db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "page_id"}}, DoUpdates: clause.AssignmentColumns([]string{"tenant_id", "knowledge_base_id", "page_sha256", "dependency_sha256", "source_versions"})}).Create(&row).Error; e != nil {
		return e
	}
	p.WikiGeneration, p.WikiIncarnation = epoch.Generation, epoch.Incarnation
	return nil
}
func verifyWikiProof(db *gorm.DB, page *types.WikiPage, p *wikiGenerationProof, published bool, allowed map[string]bool) error {
	if page == nil || page.DeletedAt.Valid || page.KnowledgeBaseID != p.KB || page.TenantID != p.Tenant || (len(page.SourceRefs) == 0 && page.PageType != types.WikiPageTypeIndex) || published && page.Status != types.WikiPageStatusPublished {
		return ErrWikiMCPUnverified
	}
	var seal WikiMCPProvenance
	if e := db.Where("page_id=? AND tenant_id=? AND knowledge_base_id=?", page.ID, p.Tenant, p.KB).Take(&seal).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return ErrWikiMCPUnverified
		}
		return e
	}
	if seal.PageSHA256 != wikiPageDigest(page) {
		return ErrWikiMCPUnverified
	}
	var sources map[string]string
	if json.Unmarshal([]byte(seal.SourceVersions), &sources) != nil || len(sources) == 0 || wikiDigest(sources) != seal.DependencySHA256 {
		return ErrWikiMCPUnverified
	}
	for id, version := range sources {
		if p.Sources[id] != version || allowed != nil && !allowed[id] {
			return ErrWikiMCPUnverified
		}
	}
	for _, ref := range page.SourceRefs {
		if sources[types.WikiSourceKnowledgeID(ref)] == "" {
			return ErrWikiMCPUnverified
		}
	}
	return nil
}

// Read-only adapter over the existing repository. No GetIndex/CreatePage path.
type PersonalWikiStore struct{ DB *gorm.DB }

// Recheck page status, exact body proof and every source at the output boundary.
// A document-only grant cannot detect a concurrent Wiki archive or raw edit.
func (s *PersonalWikiStore) RevalidatePages(ctx context.Context, tenant uint64, kb string, authorized []string, read []*types.WikiPage) error {
	if len(read) > 200 {
		return ErrWikiMCPUnverified
	}
	allowed := map[string]bool{}
	for _, id := range authorized {
		allowed[id] = true
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		proof, e := wikiDependencies(tx, kb)
		if e != nil {
			return e
		}
		if proof.Tenant != tenant {
			return ErrWikiMCPUnverified
		}
		for _, previous := range read {
			if previous == nil {
				return ErrWikiMCPUnverified
			}
			var current types.WikiPage
			if e = tx.Where("id=? AND tenant_id=? AND knowledge_base_id=?", previous.ID, tenant, kb).First(&current).Error; e != nil {
				return e
			}
			if wikiPageDigest(&current) != wikiPageDigest(previous) {
				return ErrWikiMCPUnverified
			}
			if e = verifyWikiProof(tx, &current, proof, true, allowed); e != nil {
				return e
			}
		}
		return nil
	})
}

func (s *PersonalWikiStore) VerifiedPages(ctx context.Context, tenant uint64, kb string, authorized []string) ([]*types.WikiPage, bool, error) {
	allowed := map[string]bool{}
	for _, id := range authorized {
		allowed[id] = true
	}
	var result []*types.WikiPage
	partial := false
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, e := wikiDependencies(tx, kb)
		if e != nil {
			return e
		}
		if p.Tenant != tenant {
			return ErrWikiMCPUnverified
		}

		var total int64
		if e = tx.Model(&types.WikiPage{}).Where("tenant_id=? AND knowledge_base_id=? AND status=?", tenant, kb, types.WikiPageStatusPublished).Count(&total).Error; e != nil {
			return e
		}
		var pages []*types.WikiPage
		if e = tx.Where("tenant_id=? AND knowledge_base_id=? AND status=? AND length(content)<=262144 AND length(title)<=4096 AND length(summary)<=32768 AND length(COALESCE(CAST(source_refs AS TEXT),''))<=65536 AND length(COALESCE(CAST(page_metadata AS TEXT),''))<=65536", tenant, kb, types.WikiPageStatusPublished).Order("slug,id").Limit(201).Find(&pages).Error; e != nil {
			return e
		}
		if total > int64(len(pages)) {
			partial = true
		}
		if len(pages) > 200 {
			pages = pages[:200]
			partial = true
		}
		for _, page := range pages {
			if e = verifyWikiProof(tx, page, p, true, allowed); e != nil {
				if errors.Is(e, ErrWikiMCPUnverified) {
					partial = true
					continue
				}
				return e
			}
			result = append(result, page)
		}
		return nil
	})
	return result, partial, e
}
