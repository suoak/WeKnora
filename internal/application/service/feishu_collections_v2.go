package service

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type feishuCollectionEvidence struct {
	Links         []types.FeishuDiscoveredLink
	BlockedPath   []string
	ErrorCategory string
}

// CreateCollection admits one explicit tree. Personal discovery is restricted to
// the owner's private KB; publishing a changing tree cannot infer sharing grants.
func (s *FeishuPersonalService) CreateCollection(
	ctx context.Context, kbID string, q FeishuResourceQuery,
) (*types.FeishuCollection, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if s.db.Name() != "postgres" || s.recovery == nil || !s.recovery.firstCreateOnly {
		return nil, ErrFeishuStagingRequired
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	if q.AuthMode == types.FeishuUserOAuth {
		owner, e := types.PrivateKBOwner(s.db.WithContext(ctx), kbID)
		if e != nil {
			return nil, e
		}
		if owner != user {
			return nil, ErrFeishuSharingApproval
		}
	}
	frontier, err := core.NewDiscoveryFrontier(q.Kind, q.Parent, q.Space)
	if err != nil {
		return nil, err
	}
	_, _, ref, err := s.ResolveCredentialReference(ctx, q.AuthMode, q.ConnectionID)
	if err != nil {
		return nil, err
	}
	encoded, err := sealFeishu(frontier)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	row := &types.FeishuCollection{
		ID: uuid.NewString(), OwnerID: user, TenantID: kb.TenantID, KBID: kbID,
		AuthMode: q.AuthMode, ConnectionID: ref.ConnectionID, ConfigVersion: ref.ConfigVersion,
		Kind: q.Kind, RootID: q.Parent, SpaceID: q.Space, State: "active", Frontier: encoded,
		Cycle: 1, NextRunAt: now, CreatedAt: now, UpdatedAt: now,
	}
	write := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if write.Error != nil {
		return nil, write.Error
	}
	if write.RowsAffected == 0 {
		row = &types.FeishuCollection{}
		err = s.db.WithContext(ctx).Where("owner_id = ? AND kb_id = ? AND auth_mode = ? AND "+
			"connection_id = ? AND kind = ? AND root_id = ? AND space_id = ?",
			user, kbID, q.AuthMode, ref.ConnectionID, q.Kind, q.Parent, q.Space).First(row).Error
	}
	return row, err // An existing paused/held collection is never resumed here.
}

// ListCollections returns only the owner's bounded discovery history.
func (s *FeishuPersonalService) ListCollections(
	ctx context.Context, kbID string, offset int,
) ([]types.FeishuCollection, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if offset < 0 || offset > 10000 {
		return nil, errors.New("invalid_collection_history_offset")
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	rows := []types.FeishuCollection{}
	err = s.db.WithContext(ctx).Where("owner_id = ? AND kb_id = ?", user, kbID).
		Order("created_at DESC, id DESC").Offset(offset).Limit(20).Find(&rows).Error
	return rows, err
}

// CollectionAction pauses discovery or explicitly schedules a permission-checked
// continuation. Already admitted batches and document sources remain separate.
func (s *FeishuPersonalService) CollectionAction(ctx context.Context, kbID, id, action string) error {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return access.ErrUnauthorized
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return err
	}
	var row types.FeishuCollection
	if err = s.db.WithContext(ctx).Where("id = ? AND owner_id = ? AND kb_id = ?", id, user, kbID).
		First(&row).Error; err != nil {
		return access.ErrNotFound
	}
	values := map[string]any{"updated_at": time.Now()}
	switch action {
	case "pause":
		// Pausing never replaces a restrictive hold or revocation.
		if row.State != "active" && row.State != "paused" {
			return access.ErrForbidden
		}
		values["state"] = "paused"
	case "resume", "discover":
		if row.State == "hold" || row.State == "revoked" || action == "discover" && row.State != "active" {
			return access.ErrForbidden
		}
		_, _, ref, e := s.ResolveCredentialReference(ctx, row.AuthMode, row.ConnectionID)
		if e != nil {
			return e
		}
		if ref.ConfigVersion != row.ConfigVersion {
			return ErrFeishuReconnect
		}
		values["state"], values["next_run_at"] = "active", time.Now()
	default:
		return errors.New("invalid_collection_action")
	}
	change := s.db.WithContext(ctx).Model(&types.FeishuCollection{}).
		Where("id = ? AND owner_id = ? AND state = ?", id, user, row.State).Updates(values)
	if change.Error != nil {
		return change.Error
	}
	if change.RowsAffected != 1 {
		return access.ErrForbidden
	}
	return nil
}

// CollectionPages exposes retained provenance only to the collection owner.
func (s *FeishuPersonalService) CollectionPages(
	ctx context.Context, kbID, id string, offset int,
) ([]types.FeishuCollectionPage, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if offset < 0 || offset > 10000 {
		return nil, errors.New("invalid_collection_history_offset")
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	var row types.FeishuCollection
	if err = s.db.WithContext(ctx).Where("id = ? AND owner_id = ? AND kb_id = ?", id, user, kbID).
		First(&row).Error; err != nil {
		return nil, access.ErrNotFound
	}
	pages := []types.FeishuCollectionPage{}
	err = s.db.WithContext(ctx).Where("collection_id = ?", id).
		Order("cycle DESC, ordinal DESC").Offset(offset).Limit(20).Find(&pages).Error
	if err != nil {
		return nil, err
	}
	for i := range pages {
		var evidence feishuCollectionEvidence
		if err = openFeishu(pages[i].Provenance, &evidence); err != nil {
			return nil, err
		}
		pages[i].Links, pages[i].BlockedPath, pages[i].ErrorCategory = evidence.Links, evidence.BlockedPath,
			evidence.ErrorCategory
	}
	return pages, nil
}

// ReconcileCollections advances at most one leased provider page per pass. Batch
// admission and the encrypted frontier are committed together, never indexing.
func (s *FeishuPersonalService) ReconcileCollections(ctx context.Context) error {
	if os.Getenv("FEISHU_ACCESS_V2_ENABLED") != "1" || s.recovery == nil {
		return nil
	}
	var row types.FeishuCollection
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state = ? AND next_run_at <= ? AND (lease_until IS NULL OR lease_until < ?)",
				"active", time.Now(), time.Now()).Order("next_run_at,id").First(&row).Error; e != nil {
			return e
		}
		row.LeaseOwner = uuid.NewString()
		until := time.Now().Add(2 * time.Minute)
		row.LeaseUntil = &until
		return tx.Save(&row).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.advanceCollection(ctx, &row)
	if err == nil {
		return nil
	}
	// Preserve the unacknowledged frontier on all faults. Known temporary
	// faults retry later; uncertain permission/identity/capacity results HOLD.
	values := map[string]any{
		"lease_owner": "", "lease_until": nil, "updated_at": time.Now(),
		"error_category": feishuV2Error(err), "next_run_at": time.Now().Add(time.Minute),
	}
	if values["error_category"] != "temporary_failure" {
		values["state"] = "hold"
	} else {
		var frontier core.DiscoveryFrontier
		if e := openFeishu(row.Frontier, &frontier); e != nil {
			return e
		}
		frontier.RetryCount++
		encoded, e := sealFeishu(frontier)
		if e != nil {
			return e
		}
		values["frontier"] = encoded
		if frontier.RetryCount >= 3 {
			values["state"] = "hold"
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.db.WithContext(cleanup).Model(&types.FeishuCollection{}).
		Where("id = ? AND state = ? AND lease_owner = ?", row.ID, "active", row.LeaseOwner).Updates(values).Error
}

func (s *FeishuPersonalService) advanceCollection(ctx context.Context, row *types.FeishuCollection) error {
	fresh, err := s.batchContext(ctx, &types.FeishuBatch{OwnerID: row.OwnerID, TenantID: row.TenantID, KBID: row.KBID})
	if err != nil {
		return err
	}
	remote, client, ref, err := s.ResolveCredentialReference(fresh, row.AuthMode, row.ConnectionID)
	if err != nil {
		return err
	}
	if ref.ConfigVersion != row.ConfigVersion {
		return ErrFeishuReconnect
	}
	var frontier core.DiscoveryFrontier
	if err = openFeishu(row.Frontier, &frontier); err != nil {
		return err
	}
	cycle := row.Cycle
	if len(frontier.Queue) == 0 {
		if frontier.Truncated || len(frontier.Deferred) > 0 {
			return core.ErrDiscoveryReview
		}
		frontier, err = core.NewDiscoveryFrontier(row.Kind, row.RootID, row.SpaceID)
		if err != nil {
			return err
		}
		cycle++ // A new observation cycle; existing document cursors are untouched.
	}
	remote, cancel := context.WithTimeout(remote, 30*time.Second)
	defer cancel()
	next, links, err := core.AdvanceDiscovery(remote, client, frontier)
	evidence := feishuCollectionEvidence{Links: []types.FeishuDiscoveredLink{}}
	if err != nil {
		// Preserve denied child directories for review and continue healthy
		// siblings. Root or identity failures still hold the entire discovery.
		if len(frontier.Queue) == 0 || frontier.Queue[0].Depth == 0 ||
			!errors.Is(err, core.ErrPermission) && !errors.Is(err, core.ErrNotFound) {
			return err
		}
		next = frontier
		next.Deferred = append(append([]core.DiscoveryPosition(nil), frontier.Deferred...), frontier.Queue[0])
		next.Queue = append([]core.DiscoveryPosition(nil), frontier.Queue[1:]...)
		next.Pages++
		next.Truncated = true
		links = nil
		evidence.BlockedPath = frontier.Queue[0].Path
		evidence.ErrorCategory = "source_permission_required"
	}
	next.RetryCount = 0
	encoded, err := sealFeishu(next)
	if err != nil {
		return err
	}
	for _, link := range links {
		evidence.Links = append(evidence.Links, types.FeishuDiscoveredLink{URL: link.URL, Path: link.Path})
	}
	provenance, err := sealFeishu(evidence)
	if err != nil {
		return err
	}
	return s.db.WithContext(fresh).Transaction(func(tx *gorm.DB) error {
		var locked types.FeishuCollection
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND state = ? AND lease_owner = ? AND lease_until > ?",
				row.ID, "active", row.LeaseOwner, time.Now()).First(&locked).Error; e != nil {
			return e
		}
		var setting types.SystemSetting
		if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("key = ?", feishuAppSetting).
			First(&setting).Error; e != nil {
			return e
		}
		cipher, e := setting.AsString()
		if e != nil {
			return e
		}
		var cfg FeishuAppConfig
		if e = openFeishu(cipher, &cfg); e != nil {
			return e
		}
		if !cfg.modeEnabled(row.AuthMode) || cfg.identityVersion(row.AuthMode) != row.ConfigVersion {
			return ErrFeishuReconnect
		}
		page := types.FeishuCollectionPage{
			ID: uuid.NewString(), CollectionID: row.ID, Cycle: cycle,
			Ordinal: next.Pages, Provenance: provenance, CreatedAt: time.Now(),
		}
		if len(links) > 0 {
			urls := make([]string, 0, len(links))
			for _, link := range links {
				urls = append(urls, link.URL)
			}
			service := *s
			service.db = tx
			batch, e := service.CreateBatch(fresh, row.KBID, row.AuthMode, row.ConnectionID,
				"collection:"+page.ID, urls)
			if e != nil {
				return e
			}
			page.BatchID = &batch.Batch.ID
		}
		if e := tx.Create(&page).Error; e != nil {
			return e
		}
		due := time.Now()
		if len(next.Queue) == 0 {
			due = due.Add(15 * time.Minute)
		}
		category := ""
		if len(next.Deferred) > 0 {
			category = "source_permission_required"
		}
		return tx.Model(&locked).Updates(map[string]any{
			"frontier": encoded, "cycle": cycle,
			"pages": next.Pages, "discovered": next.Discovered, "truncated": next.Truncated,
			"lease_owner": "", "lease_until": nil, "next_run_at": due,
			"error_category": category, "updated_at": time.Now(),
		}).Error
	})
}
