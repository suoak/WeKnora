package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrWikiMCPUnverified = errors.New("wiki provenance requires verified regeneration")

// ErrWikiMCPInputsChanged rejects an in-flight snapshot that changed. Retry
// from fresh inputs; this differs from a historical page with no valid proof.
var ErrWikiMCPInputsChanged = errors.New("wiki generation input snapshot changed")

// Bounded to accommodate existing production KBs and the GitLab Wiki backlog.
// Every historical page still needs its own verified proof.
const wikiMCPMaxPages = 20000

type (
	wikiGenerationProofKey struct{}
	wikiGenerationProof    struct {
		mu               sync.Mutex
		Tenant           uint64
		KB               string
		Sources          map[string]string
		EmptySources     map[string]bool
		NoWikiSources    map[string]int
		Epochs           map[string]string
		DependencySHA256 string
		WikiGeneration   int64
		WikiIncarnation  string
		InputStamps      map[string]string
		SelectedSources  map[string]bool
		Replacement      *wikiPreservingGeneration
	}
)

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

// WikiModelCheckpointScope identifies the server-verified generation inputs.
// It is a cache namespace, never a substitute for read or publication guards.
func WikiModelCheckpointScope(ctx context.Context) (uint64, string, string, bool) {
	p, ok := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if !ok || p == nil || p.Tenant == 0 || p.KB == "" || len(p.DependencySHA256) != 64 || len(p.Sources) == 0 {
		return 0, "", "", false
	}
	return p.Tenant, p.KB, p.DependencySHA256, true
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
		if e := db.Raw("SELECT count(*) FROM pg_trigger WHERE " +
			"tgrelid='wiki_pages'::regclass AND NOT tgisinternal AND " +
			"tgenabled='O' AND tgname='wiki_mcp_mutation'").Scan(&n).Error; e != nil {
			return e
		}
		if n == 1 {
			return nil
		}
	case "sqlite":
		if e := db.Raw("SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name IN " +
			"('wiki_mcp_insert','wiki_mcp_update','wiki_mcp_delete')").Scan(&n).Error; e != nil {
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
			return ErrWikiMCPInputsChanged
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
	fresh, e := wikiDependencies(db.WithContext(ctx), p.KB, p)
	if e != nil {
		return true, e
	}
	if fresh.DependencySHA256 != p.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(p.Epochs) {
		return true, ErrWikiMCPInputsChanged
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
	return wikiDigest(
		[]any{
			p.ID,
			p.TenantID,
			p.KnowledgeBaseID,
			p.Slug,
			p.Title,
			p.Content,
			p.Summary,
			p.Status,
			p.PageType,
			p.Aliases,
			p.SourceRefs,
			p.ChunkRefs,
			p.OutLinks,
			p.PageMetadata,
		},
	)
}

// The conservative closure is the ENTIRE input KB, not citations selected by
// an LLM. Existing epoch-backed versions fence generation against all writes,
// including ABA and writes preserving timestamps. Persisted read stamps bind
// actual model input fields, so finalizing -> completed alone is not content
// replacement. Current authorization/source state is always checked separately.
func wikiDependencies(
	db *gorm.DB,
	kb string,
	previous ...*wikiGenerationProof,
) (*wikiGenerationProof, error) {
	if len(previous) == 1 && previous[0] != nil && previous[0].Replacement != nil {
		return wikiPreservingDependencies(db, kb, previous...)
	}
	if len(previous) == 1 && previous[0] != nil && previous[0].SelectedSources != nil {
		return wikiDependenciesSelected(db, kb, nil, previous[0].SelectedSources, previous...)
	}
	return wikiDependenciesWithParsedRecovery(db, kb, nil, previous...)
}

// The operator recovery preview can admit only independently validated parsed
// stalls. Ordinary generation and serving always pass a nil recovery set.
func wikiDependenciesWithParsedRecovery(db *gorm.DB, kb string, parsed map[string]bool,
	previous ...*wikiGenerationProof,
) (*wikiGenerationProof, error) {
	return wikiDependenciesSelected(db, kb, parsed, nil, previous...)
}

// A non-nil selection is issued by the worker after document-level preflight.
// Every selected document still passes the original content and epoch fences.
func wikiDependenciesSelected(db *gorm.DB, kb string, parsed map[string]bool, selected map[string]bool,
	previous ...*wikiGenerationProof,
) (*wikiGenerationProof, error) {
	localPublishedRead, _ := db.Statement.Context.Value(wikiLocalPublishedReadKey{}).(bool)
	var anchor struct{ TenantID uint64 }
	if e := db.Table("knowledge_bases kb").Select("kb.tenant_id").
		Joins("JOIN tenants t ON t.id=kb.tenant_id AND t.deleted_at IS NULL AND "+
			"t.status='active'").Where("kb.id=? AND kb.deleted_at IS NULL AND kb.type='document' AND "+
		"kb.is_temporary=false", kb).Take(&anchor).Error; e != nil {
		return nil, e
	}
	var denied bool
	if e := db.Raw(`SELECT EXISTS(SELECT 1 FROM data_sources WHERE knowledge_base_id=? AND
 (legacy_access_state IN ('hold','revoked') OR
 (type IN ('feishu','feishu_drive','lark','lark_drive') AND deleted_at IS NULL
 AND legacy_access_state NOT IN ('active','unmanaged'))))
 OR EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_base_id=? AND state<>'active')`,
		kb, kb).Scan(&denied).Error; e != nil {
		return nil, e
	}

	if denied && selected == nil {
		return nil, types.ErrLegacySourceRestricted
	}
	var docs []struct {
		ID           string
		ParseStatus  string
		EnableStatus string
		Channel      string
		Metadata     types.JSON
	}
	documentQuery := db.Table("knowledges").Select("id,parse_status,enable_status,channel,metadata").
		Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb, anchor.TenantID)
	if selected != nil {
		documentQuery = documentQuery.Where("id IN ?", wikiSelectionIDs(selected))
	}
	if e := documentQuery.
		Order("id").Limit(2049).Find(&docs).Error; e != nil {
		return nil, e
	}
	if len(docs) == 0 || len(docs) > 2048 {
		return nil, ErrWikiMCPUnverified
	}
	stamps, err := wikiSelectedDocumentInputStamps(db, kb, anchor.TenantID, selected)
	if err != nil {
		return nil, err
	}
	p := &wikiGenerationProof{
		Tenant: anchor.TenantID, KB: kb, Sources: map[string]string{}, Epochs: map[string]string{},
		InputStamps:     map[string]string{},
		SelectedSources: selected,
	}
	versionIDs := make([]string, len(docs))
	for i, doc := range docs {
		versionIDs[i] = doc.ID
	}
	versions := newWikiEpochVersions(db, kb, anchor.TenantID, versionIDs)
	sourceInputs := newWikiSourceInputs(db, versionIDs)
	for _, d := range docs {
		if selected != nil && !selected[d.ID] {
			continue
		}
		var connectorProof string
		switch d.Channel {
		case "", types.ChannelWeb, types.ChannelAPI, types.ChannelBrowserExtension:
			// Local content published under KnowHub's existing KB authority.
		case types.ConnectorTypeGitLab, types.ConnectorTypeConfluence:
			// These connectors use KnowHub workspace authority, not a Feishu receipt.
			// Require the current connector binding in this exact tenant/KB. Its
			// configuration is part of the proof and is checked again before model I/O.
			var metadata map[string]string
			if json.Unmarshal(d.Metadata, &metadata) != nil ||
				metadata["datasource_id"] == "" || metadata["external_id"] == "" {
				return nil, ErrWikiMCPUnverified
			}
			var source map[string]interface{}
			if e := db.Table("data_sources").
				Select("id,tenant_id,knowledge_base_id,type,status,legacy_access_state,config").
				Where(`id=? AND tenant_id=? AND knowledge_base_id=? AND type=? AND status='active'
 AND legacy_access_state IN ('unmanaged','active') AND deleted_at IS NULL`,
					metadata["datasource_id"], p.Tenant, kb, d.Channel).Take(&source).
				Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return nil, types.ErrLegacySourceRestricted
				}
				return nil, e
			}
			connectorProof = wikiDigest(source)
		case "feishu_personal":
			// A personal import requires the current personal publication protocol.
			// A legacy active_attempt is never a substitute for that authority.
			if !localPublishedRead && (selected == nil || !WikiDocumentSelectionForKB(kb)) {
				return nil, types.ErrLegacySourceRestricted
			}
			var err error
			connectorProof, err = WikiPersonalPublication(db, p.Tenant, kb, d.ID)
			if err != nil {
				return nil, err
			}
		case types.ChannelFeishu, types.ChannelFeishuDrive, types.ChannelLarkDrive, "lark":
			// Local published reads still hash the actual document and chunks;
			// connector acquisition/hold state does not revoke platform access.
			if localPublishedRead {
				break
			}
			var verified int64
			query := db.Table("feishu_legacy_resources").
				Where("knowledge_id=? AND knowledge_base_id=? AND tenant_id=? AND "+
					"state='active' AND active_attempt<>''", d.ID, kb, p.Tenant)
			if selected != nil {
				query = query.Where(
					`EXISTS(SELECT 1 FROM data_sources ds WHERE ds.id=feishu_legacy_resources.data_source_id
 AND ds.tenant_id=feishu_legacy_resources.tenant_id AND ds.knowledge_base_id=feishu_legacy_resources.knowledge_base_id
 AND ds.deleted_at IS NULL AND ds.legacy_access_state IN ('active','unmanaged')
 AND ds.type IN ('feishu','feishu_drive','lark','lark_drive'))`,
				)
			}
			if e := query.Count(&verified).Error; e != nil {
				return nil, e
			}
			if verified != 1 {
				return nil, types.ErrLegacySourceRestricted
			}
		default:
			return nil, ErrWikiMCPUnverified
		}
		inputReady := d.ParseStatus == "completed" || d.ParseStatus == "finalizing" ||
			(d.ParseStatus == "failed" && parsed[d.ID])
		if !inputReady || d.EnableStatus != "enabled" {
			return nil, ErrWikiMCPUnverified
		}
		stamp, found := stamps[d.ID]
		if !found {
			return nil, ErrWikiMCPUnverified
		}
		stamp = wikiDigest([]any{stamp, connectorProof, d.Channel, d.Metadata})
		p.InputStamps[d.ID] = stamp
		if len(previous) == 1 && previous[0] != nil {
			old := previous[0]
			if old.Tenant == p.Tenant && old.KB == kb && old.InputStamps[d.ID] == stamp &&
				old.Sources[d.ID] != "" && old.Epochs[d.ID] != "" {
				// Reuse only this generation's exact verified input digest. The
				// trigger-backed epoch is read live, as are source state,
				// connector binding and the resource publication proof above.
				// No TTL or cached authorization is involved.
				p.Sources[d.ID], p.Epochs[d.ID] = old.Sources[d.ID], old.Epochs[d.ID]
				if n, noWiki := old.NoWikiSources[d.ID]; noWiki {
					if p.NoWikiSources == nil {
						p.NoWikiSources = map[string]int{}
					}
					p.NoWikiSources[d.ID] = n
				}
				if old.EmptySources[d.ID] {
					if p.EmptySources == nil {
						p.EmptySources = map[string]bool{}
					}
					p.EmptySources[d.ID] = true
				}
				continue
			}
		}
		version, tenant, e := versions.get(d.ID)
		if e != nil {
			return nil, e
		}
		if tenant != p.Tenant {
			return nil, ErrWikiMCPUnverified
		}
		p.Epochs[d.ID] = version
		identity, inputs, e := sourceInputs.get(d.ID, p.Tenant, kb)
		if e != nil {
			return nil, e
		}
		if connectorProof != "" {
			identity["connector_proof"] = connectorProof
			identity["connector_metadata"] = string(d.Metadata)
		}
		if len(inputs) == 0 {
			if connectorProof == "" {
				return nil, ErrWikiMCPUnverified
			}
			emptyVersion, err := emptyGitLabWikiInputVersion(db, p.Tenant, kb, d.ID)
			if err != nil {
				return nil, err
			}
			identity["verified_empty_input"] = emptyVersion
			if p.EmptySources == nil {
				p.EmptySources = map[string]bool{}
			}
			p.EmptySources[d.ID] = true
		}
		p.Sources[d.ID] = wikiDigest([]any{identity, inputs})
		if n, noWiki := wikiInsufficientPlainText(inputs); noWiki {
			if p.NoWikiSources == nil {
				p.NoWikiSources = map[string]int{}
			}
			p.NoWikiSources[d.ID] = n
		}
	}
	if selected != nil && len(p.Sources) != len(selected) {
		return nil, ErrWikiMCPInputsChanged
	}
	after, err := wikiSelectedDocumentInputStamps(db, kb, anchor.TenantID, selected)
	if err != nil {
		return nil, err
	}
	if wikiDigest(after) != wikiDigest(stamps) {
		return nil, ErrWikiMCPUnverified
	}
	p.DependencySHA256 = wikiDigest(p.Sources)
	return p, nil
}

// Only the server generation worker calls this before assembling ANY prompts.
// Unsealed historical pages may contain unknown sources: they are not certified
// by reading their current timestamps or by copying SourceRefs after generation.
func BeginWikiMCPGeneration(
	ctx context.Context,
	repo interfaces.WikiPageRepository,
	kb string,
) (context.Context, error) {
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
		if e = tx.Where("knowledge_base_id=?", kb).Limit(wikiMCPMaxPages + 1).Find(&old).Error; e != nil {
			return e
		}
		if len(old) > wikiMCPMaxPages {
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
		return ErrWikiMCPUnverified
	}
	for _, ref := range page.SourceRefs {
		id := types.WikiSourceKnowledgeID(ref)
		if id == "" || p.Sources[id] == "" {
			return ErrWikiMCPUnverified
		}
	}
	fresh, e := wikiDependencies(db, p.KB, p)
	if e != nil {
		return e
	}
	if fresh.DependencySHA256 != p.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(p.Epochs) {
		return ErrWikiMCPInputsChanged
	}
	b, e := json.Marshal(p.Sources)
	if e != nil {
		return e
	}
	row := WikiMCPProvenance{
		PageID:           page.ID,
		TenantID:         p.Tenant,
		KnowledgeBaseID:  p.KB,
		PageSHA256:       wikiPageDigest(page),
		DependencySHA256: p.DependencySHA256,
		SourceVersions:   string(b),
	}
	epoch, e := readWikiMCPEpoch(db, p.KB)
	if e != nil {
		return e
	}
	if epoch.Generation != p.WikiGeneration+1 || len(epoch.Incarnation) != 32 ||
		p.WikiIncarnation != "" && epoch.Incarnation != p.WikiIncarnation {
		return ErrWikiMCPUnverified
	}
	if e = db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "page_id"}}, DoUpdates: clause.
		AssignmentColumns([]string{
			"tenant_id", "knowledge_base_id", "page_sha256", "dependency_sha256",
			"source_versions",
		})}).Create(&row).Error; e != nil {
		return e
	}
	p.WikiGeneration, p.WikiIncarnation = epoch.Generation, epoch.Incarnation
	return nil
}

func verifyWikiProof(
	db *gorm.DB,
	page *types.WikiPage,
	p *wikiGenerationProof,
	published bool,
	allowed map[string]bool,
) error {
	if page == nil || page.DeletedAt.Valid || page.KnowledgeBaseID != p.KB || page.TenantID != p.Tenant ||
		(len(page.SourceRefs) == 0 && page.PageType != types.WikiPageTypeIndex) ||
		published && page.Status != types.WikiPageStatusPublished {
		return ErrWikiMCPUnverified
	}
	var seal WikiMCPProvenance
	if e := db.Where("page_id=? AND tenant_id=? AND knowledge_base_id=?", page.ID, p.Tenant, p.KB).
		Take(&seal).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return ErrWikiMCPUnverified
		}
		return e
	}
	return verifyWikiSeal(page, p, &seal, allowed, true)
}

// Verify the persisted page seal before loading any current source bodies.
func verifyWikiSeal(
	page *types.WikiPage, p *wikiGenerationProof, seal *WikiMCPProvenance, allowed map[string]bool, current bool,
) error {
	if seal.PageSHA256 != wikiPageDigest(page) {
		return ErrWikiMCPUnverified
	}
	var sources map[string]string
	if json.Unmarshal([]byte(seal.SourceVersions), &sources) != nil || len(sources) == 0 ||
		wikiDigest(sources) != seal.DependencySHA256 {
		return ErrWikiMCPUnverified
	}
	for id, version := range sources {
		if current && p.Sources[id] != version || allowed != nil && !allowed[id] {
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
func (s *PersonalWikiStore) RevalidatePages(
	ctx context.Context,
	tenant uint64,
	kb string,
	authorized []string,
	read []*types.WikiPage,
) error {
	defer perftrace.Measure(ctx, perftrace.WikiOutputProof)()
	ctx = access.WithoutPortalSnapshotReads(ctx)
	if len(read) > 200 {
		return ErrWikiMCPUnverified
	}
	allowed := map[string]bool{}
	for _, id := range authorized {
		allowed[id] = true
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		proof, e := wikiPageReadProof(tx, kb, nil)
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
			pageQuery := tx
			if types.LocalSourceContentRead(ctx) || WikiDocumentSelectionForKB(kb) {
				pageQuery = tx.Session(&gorm.Session{SkipHooks: true}).
					Table("wiki_pages AS page_validation_inputs")
			}
			if e = pageQuery.Where("id=? AND tenant_id=? AND knowledge_base_id=?", previous.ID, tenant,
				kb).First(&current).Error; e != nil {
				return e
			}
			if wikiPageDigest(&current) != wikiPageDigest(previous) {
				return ErrWikiMCPUnverified
			}
			if types.LocalSourceContentRead(ctx) || WikiDocumentSelectionForKB(kb) {
				proof, e = wikiPageReadProof(tx, kb, &current)
				if e != nil {
					return e
				}
			}
			if e = verifyWikiProof(tx, &current, proof, true, allowed); e != nil {
				return e
			}
		}
		return nil
	})
}

// VerifiedPages returns only pages with verified document provenance.
func (s *PersonalWikiStore) VerifiedPages(
	ctx context.Context,
	tenant uint64,
	kb string,
	authorized []string,
) ([]*types.WikiPage, bool, error) {
	return s.VerifiedPagesWindow(ctx, tenant, kb, authorized, "", 200)
}

// VerifiedPagesWindow limits verified output work, never the source closure.
// Exact page reads select their slug before the bounded candidate scan.
func (s *PersonalWikiStore) VerifiedPagesWindow(
	ctx context.Context, tenant uint64, kb string, authorized []string, slug string, limit int,
) ([]*types.WikiPage, bool, error) {
	return s.verifiedPagesWindow(ctx, tenant, kb, authorized, slug, "", limit)
}

// VerifiedSearchPages selects literal matches before the bounded proof scan.
// A page beyond the index window must not disappear merely because its slug
// sorts later. Matching is only a candidate filter: every returned page still
// passes the same seal, source closure and live output verification.
func (s *PersonalWikiStore) VerifiedSearchPages(
	ctx context.Context, tenant uint64, kb string, authorized []string, query string,
) ([]*types.WikiPage, bool, error) {
	if strings.TrimSpace(query) == "" || len(query) > 4096 {
		return nil, false, ErrWikiMCPUnverified
	}
	return s.verifiedPagesWindow(ctx, tenant, kb, authorized, "", query, 200)
}

func (s *PersonalWikiStore) verifiedPagesWindow(
	ctx context.Context, tenant uint64, kb string, authorized []string, slug, query string, limit int,
) ([]*types.WikiPage, bool, error) {
	return s.verifiedPagesSelection(ctx, tenant, kb, authorized, slug, query, nil, limit)
}

// VerifiedLinkedPages resolves exact same-KB targets through the ordinary sealed
// page and current-source proof. Link text is never authorized by its source page
// alone, by a matching title in another KB, or by an unverified historical page.
func (s *PersonalWikiStore) VerifiedLinkedPages(
	ctx context.Context, tenant uint64, kb string, authorized, slugs []string,
) ([]*types.WikiPage, bool, error) {
	if len(slugs) == 0 || len(slugs) > 200 {
		return nil, false, ErrWikiMCPUnverified
	}
	for _, slug := range slugs {
		if slug == "" || len(slug) > 255 {
			return nil, false, ErrWikiMCPUnverified
		}
	}
	return s.verifiedPagesSelection(ctx, tenant, kb, authorized, "", "", slugs, 200)
}

func (s *PersonalWikiStore) verifiedPagesSelection(
	ctx context.Context, tenant uint64, kb string, authorized []string,
	slug, query string, slugs []string, limit int,
) ([]*types.WikiPage, bool, error) {
	return s.verifiedPagesMatching(ctx, tenant, kb, authorized, slug, query, "", slugs, limit)
}

// VerifiedPatternSearchPages retains the native tool's PostgreSQL regex and
// field-priority ranking, with the same bounded seal/source proof as MCP.
func (s *PersonalWikiStore) VerifiedPatternSearchPages(
	ctx context.Context, tenant uint64, kb string, authorized []string, pattern string,
) ([]*types.WikiPage, bool, error) {
	if s.DB.Name() != "postgres" || pattern == "" || len(pattern) > 4096 {
		return nil, false, ErrWikiMCPUnverified
	}
	return s.verifiedPagesMatching(ctx, tenant, kb, authorized, "", "", pattern, nil, 200)
}

// SharedWikiDocumentIDs projects the currently enabled documents only after the
// ordinary whole-KB source/private gate. The service must independently authorize
// the caller's full shared-KB grant before and after this read. This is not a
// Personal MCP scope resolver and cannot mint document or workspace grants.
func (s *PersonalWikiStore) SharedWikiDocumentIDs(
	ctx context.Context, tenant uint64, kb string,
) ([]string, error) {
	if types.WorkspaceWebReadTenant(ctx) == 0 || tenant == 0 || kb == "" {
		return nil, types.ErrLegacySourceRestricted
	}
	db := s.DB.WithContext(ctx)
	if err := types.CheckLegacySourceRead(db, "", kb); err != nil {
		return nil, err
	}
	var ids []string
	if err := db.Model(&types.Knowledge{}).Select("id").Where(
		"tenant_id=? AND knowledge_base_id=? AND parse_status=? AND enable_status=?",
		tenant, kb, types.ParseStatusCompleted, "enabled").Order("id").Limit(wikiMCPMaxPages + 1).
		Scan(&ids).Error; err != nil {
		return nil, err
	}
	if len(ids) > wikiMCPMaxPages {
		return nil, ErrWikiMCPUnverified
	}
	return ids, types.CheckLegacySourceRead(db, "", kb)
}

func (s *PersonalWikiStore) verifiedPagesMatching(
	ctx context.Context, tenant uint64, kb string, authorized []string,
	slug, query, pattern string, slugs []string, limit int,
) ([]*types.WikiPage, bool, error) {
	defer perftrace.Measure(ctx, perftrace.WikiCandidates)()
	if limit < 1 || limit > 200 || len(slug) > 255 {
		return nil, false, ErrWikiMCPUnverified
	}
	allowed := map[string]bool{}
	for _, id := range authorized {
		allowed[id] = true
	}
	var result []*types.WikiPage
	partial := false
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		var total int64
		countQuery := tx.Model(&types.WikiPage{}).Where("tenant_id=? AND knowledge_base_id=? AND status=?",
			tenant, kb, types.WikiPageStatusPublished)
		if slug != "" {
			countQuery = countQuery.Where("slug=?", slug)
		}
		if len(slugs) > 0 {
			countQuery = countQuery.Where("slug IN ?", slugs)
		}
		if query != "" {
			countQuery = wikiLiteralSearchCandidates(countQuery, query)
		}
		if pattern != "" {
			countQuery = wikiPatternSearchCandidates(countQuery, pattern)
		}
		if e = countQuery.Count(&total).Error; e != nil {
			return e
		}
		var pages []*types.WikiPage
		pageQuery := tx
		if types.LocalSourceContentRead(ctx) || WikiDocumentSelectionForKB(kb) {
			pageQuery = tx.Session(&gorm.Session{SkipHooks: true}).
				Table("wiki_pages AS page_validation_inputs")
		}
		if slug != "" {
			pageQuery = pageQuery.Where("slug=?", slug)
		}
		if len(slugs) > 0 {
			pageQuery = pageQuery.Where("slug IN ?", slugs)
		}
		if query != "" {
			pageQuery = wikiLiteralSearchCandidates(pageQuery, query)
		}
		if pattern != "" {
			pageQuery = wikiPatternSearchCandidates(pageQuery, pattern).Order(clause.OrderBy{
				Expression: clause.Expr{
					SQL: "CASE WHEN title ~* ? THEN 4 WHEN slug ~* ? THEN 3 " +
						"WHEN summary ~* ? THEN 2 WHEN content ~* ? THEN 1 ELSE 0 END DESC, updated_at DESC, slug,id",
					Vars: []interface{}{pattern, pattern, pattern, pattern},
				},
			})
		}
		if e = pageQuery.Where("tenant_id=? AND knowledge_base_id=? AND status=? AND "+
			"length(content)<=262144 AND length(title)<=4096 AND "+
			"length(summary)<=32768 AND length(COALESCE(CAST(source_refs AS "+
			"TEXT),''))<=65536 AND length(COALESCE(CAST(page_metadata AS "+
			"TEXT),''))<=65536", tenant, kb, types.WikiPageStatusPublished).Order("slug,id").Limit(201).
			Find(&pages).Error; e != nil {
			return e
		}
		if total > int64(len(pages)) {
			partial = true
		}
		if len(pages) > 200 {
			pages = pages[:200]
			partial = true
		}
		// A bounded batch of persisted seals can reject unsealed historical,
		// edited, or out-of-scope pages before hashing the entire KB corpus.
		// This is only a negative prefilter: retained pages still pass the
		// original current source proof and final output revalidation.
		ids := make([]string, len(pages))
		for i, page := range pages {
			ids[i] = page.ID
		}
		var seals []WikiMCPProvenance
		if len(ids) > 0 {
			if err := tx.Where("page_id IN ? AND tenant_id=? AND knowledge_base_id=?", ids, tenant, kb).
				Find(&seals).Error; err != nil {
				return err
			}
		}
		sealed := make(map[string]*WikiMCPProvenance, len(seals))
		for i := range seals {
			sealed[seals[i].PageID] = &seals[i]
		}
		eligible := pages[:0]
		for _, page := range pages {
			seal := sealed[page.ID]
			if seal == nil ||
				verifyWikiSeal(page, &wikiGenerationProof{KB: kb, Tenant: tenant}, seal, allowed, false) != nil {
				partial = true
				continue
			}
			eligible = append(eligible, page)
		}
		pages = eligible
		if len(pages) == 0 {
			return nil
		}
		p, e := wikiPageReadProof(tx, kb, nil)
		if e != nil {
			return e
		}
		if p.Tenant != tenant {
			return ErrWikiMCPUnverified
		}
		for _, page := range pages {
			if types.LocalSourceContentRead(ctx) || WikiDocumentSelectionForKB(kb) {
				p, e = wikiPageReadProof(tx, kb, page)
				if e != nil {
					if errors.Is(e, ErrWikiMCPUnverified) || errors.Is(e, ErrWikiMCPInputsChanged) ||
						errors.Is(e, types.ErrLegacySourceRestricted) ||
						errors.Is(e, gorm.ErrRecordNotFound) {
						partial = true
						continue
					}
					return e
				}
			}
			if e = verifyWikiProof(tx, page, p, true, allowed); e != nil {
				if errors.Is(e, ErrWikiMCPUnverified) {
					partial = true
					continue
				}
				return e
			}
			result = append(result, page)
			if len(result) >= limit {
				break
			}
		}
		return nil
	})
	return result, partial, e
}

func wikiPatternSearchCandidates(tx *gorm.DB, pattern string) *gorm.DB {
	return tx.Where("(title ~* ? OR slug ~* ? OR summary ~* ? OR content ~* ?)",
		pattern, pattern, pattern, pattern)
}

func wikiLiteralSearchCandidates(tx *gorm.DB, query string) *gorm.DB {
	// Concatenation and parameter binding preserve %, _, backslashes and SQL
	// metacharacters as literal text. Use the same newline separators as the
	// service's final strings.Contains check, including cross-field matches.
	text := "COALESCE(title,'') || char(10) || COALESCE(summary,'') || char(10) || COALESCE(content,'')"
	if tx.Name() == "postgres" {
		text = "COALESCE(title,'') || chr(10) || COALESCE(summary,'') || chr(10) || COALESCE(content,'')"
		return tx.Where("strpos(lower("+text+"), lower(?)) > 0", query)
	}
	return tx.Where("instr(lower("+text+"), lower(?)) > 0", query)
}
