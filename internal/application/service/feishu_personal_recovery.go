package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type personalDispatchRunner interface {
	Process(context.Context, types.DocumentProcessPayload) error
	Reconcile(context.Context) error
	Dispatchable(context.Context, types.KnowledgeDispatch) (bool, error)
}

// PersonalRecovery defines personal recovery state.
// Explicit opt-in: no default replacement of the legacy Knowledge lifecycle.
// This protocol finishes at a durable private manifest, not a searchable index.
type PersonalRecovery struct {
	service         *FeishuPersonalService
	repo            *repository.PersonalRecoveryRepository
	store           *personalManifestStore
	shares          interfaces.KBShareService
	lease           time.Duration
	firstCreateOnly bool
}

// EnablePersonalRecovery provides the enable personal recovery operation.
func EnablePersonalRecovery(
	s *FeishuPersonalService,
	knowledge interfaces.KnowledgeService,
	shares interfaces.KBShareService,
	root string,
) error {
	if s.db.Name() != "postgres" {
		return errors.New("personal fencing protocol requires PostgreSQL")
	}
	for _, model := range []any{
		&types.FeishuPersonalResource{},
		&types.FeishuPersonalAttempt{},
		&types.FeishuPersonalGrantEpoch{},
	} {
		if !s.db.Migrator().HasTable(model) {
			return errors.New("personal recovery requires migration 3031")
		}
	}
	if !s.db.Migrator().HasColumn(&types.FeishuPersonalImport{}, "auth_mode") ||
		!s.db.Migrator().HasColumn(&types.FeishuPersonalAttempt{}, "application_id") {
		return errors.New("feishu identity requires migration 3033")
	}
	store, err := newPersonalManifestStore(root)
	if err != nil {
		return err
	}
	ks, ok := knowledge.(*knowledgeService)
	if !ok {
		return errors.New("personal recovery requires durable knowledge dispatcher")
	}
	p := &PersonalRecovery{
		service: s,
		repo:    repository.NewPersonalRecoveryRepository(s.db),
		store:   store,
		shares:  shares,
		lease:   30 * time.Second,
	}
	p.firstCreateOnly = os.Getenv("FEISHU_PERSONAL_MVP_FIRST_CREATE") == "1"
	if applicationGrantEnabled() && (!p.firstCreateOnly || !applicationGrantEngineAvailable()) {
		return errors.New("application grant requires first-create and native anydoc engine")
	}
	p.repo.FirstCreateOnly = p.firstCreateOnly
	p.repo.ApplicationGrantValidator = p.validateApplicationIdentityTx
	s.recovery = p
	ks.personalRecovery = p
	if models, ok := ks.modelService.(*modelService); ok {
		models.personalRecovery = p
	}
	if kbs, ok := ks.kbService.(*knowledgeBaseService); ok {
		kbs.personalRecovery = p
	}
	return nil
}

// Restore a new context from the original user; no task/admin grant is inherited.
// Tenant membership and KB ownership are live checks, not payload assertions.
func (p *PersonalRecovery) authorizeLocal(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) (context.Context, *FeishuAppConfig, error) {
	db := p.service.db.WithContext(ctx)
	var user types.User
	if err := db.Where("id = ? AND is_active = ?", m.UserID, true).First(&user).Error; err != nil {
		return nil, nil, personalAccessError(err)
	}
	var member types.TenantMember
	if err := db.Where(
		"user_id = ? AND tenant_id = ? AND status = ?",
		m.UserID,
		a.CallerTenantID,
		types.TenantMemberStatusActive,
	).
		First(
			&member,
		).
		Error; err != nil {
		return nil, nil, personalAccessError(err)
	}
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		return nil, nil, access.ErrForbidden
	}
	var tenant types.Tenant
	if err := db.Where("id = ? AND status = ?", a.CallerTenantID, "active").First(&tenant).Error; err != nil {
		return nil, nil, personalAccessError(err)
	}
	ownerContext := context.WithValue(ctx, types.UserIDContextKey, user.ID)
	ownerContext = types.WithCaller(ownerContext, types.Caller{
		TenantID: member.TenantID, UserID: user.ID, Role: member.Role,
	})
	ownerContext = types.WithPrincipal(
		ownerContext,
		types.Principal{Type: types.PrincipalWebUser, ID: user.ID},
	)
	db = db.WithContext(ownerContext)
	kb, err := p.loadAuthorizationKB(ownerContext, m.KBID)
	if err != nil {
		return nil, nil, personalAccessError(err)
	}
	// The canonical KB resolver below is the write authority, just as at the
	// product route. Requiring creator/admin here would reject an otherwise
	// authorized contributor before duplicate detection or background recovery.
	caller := types.Caller{TenantID: member.TenantID, UserID: user.ID, Role: member.Role}
	// Replace inherited caller/grants, then let the canonical resolver issue a
	// fresh grant. Incoming task contexts have no user authority whatsoever.
	fresh := context.WithValue(ownerContext, types.KBGrantsContextKey, nil)
	fresh = types.WithCaller(fresh, caller)
	fresh = context.WithValue(fresh, types.UserIDContextKey, user.ID)
	grant, err := access.ResolveKB(
		fresh,
		access.KBRequest{Caller: caller},
		kb,
		types.OrgRoleEditor,
		p.shares,
		nil,
	)
	if err != nil {
		return nil, nil, err
	}
	fresh = grant.Context(fresh)
	if err = access.RequireKBWrite(fresh, kb); err != nil {
		return nil, nil, err
	}
	cfg, err := p.service.app(fresh)
	if err != nil {
		return nil, nil, err
	}
	if !cfg.Enabled || cfg.identityVersion(types.FeishuAuthMode(m.AuthMode)) != a.AppConfigVersion ||
		cfg.AppID != m.AppID ||
		!cfg.modeEnabled(types.FeishuAuthMode(m.AuthMode)) ||
		types.FeishuAuthMode(a.AuthMode) != types.FeishuAuthMode(m.AuthMode) {
		return nil, nil, repository.ErrPersonalIdentity
	}
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuUserOAuth {
		if err := p.service.assertConnectionHealth(db, m.UserID); err != nil {
			return nil, nil, err
		}
	}
	if err := p.service.checkSharingApproval(db, cfg, m, a.Revision); err != nil {
		return nil, nil, err
	}
	return fresh, cfg, nil
}

func (p *PersonalRecovery) authorize(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) (context.Context, error) {
	fresh, cfg, err := p.authorizeLocal(ctx, m, a)
	if err != nil {
		return nil, err
	}
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		return p.authorizeApplication(ctx, fresh, cfg, m, a)
	}
	if m.ApplicationID != "" || a.ApplicationID != "" {
		return nil, repository.ErrPersonalIdentity
	}
	db := p.service.db.WithContext(ctx)
	var epoch types.FeishuPersonalGrantEpoch
	if err = db.Where("user_id = ? AND epoch = ?", m.UserID, a.GrantEpoch).First(&epoch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFeishuReconnect
		}
		return nil, err
	}
	var connection types.FeishuConnection
	if err = db.Where(
		"user_id = ? AND app_id = ? AND open_id = ?",
		m.UserID,
		m.AppID,
		m.OpenID,
	).
		First(
			&connection,
		).
		Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFeishuReconnect
		}
		return nil, err
	}

	var tokens feishuTokens
	if err := openFeishu(connection.Tokens, &tokens); err != nil || tokens.UserID != m.UserID ||
		tokens.Access == "" {
		return nil, ErrFeishuReconnect
	}
	if !connection.ExpiresAt.After(time.Now()) && !connection.RefreshExpiresAt.After(time.Now()) {
		return nil, ErrFeishuRefreshExpired
	}
	return core.WithUserIdentity(fresh, func(call context.Context) (string, error) {
		// This check runs for every API request, including image/export polling.
		if _, err := p.authorize(call, m, a); err != nil {
			return "", err
		}

		var current types.FeishuPersonalGrantEpoch
		if err := p.service.db.WithContext(
			call,
		).
			Where(
				"user_id = ? AND epoch = ?",
				m.UserID,
				a.GrantEpoch,
			).
			First(
				&current,
			).
			Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", ErrFeishuReconnect
			}
			return "", err
		}
		return p.service.UserToken(call, m.UserID, m.AppID, m.OpenID)
	}), nil
}

func (p *PersonalRecovery) prepareWithIdentity(
	ctx context.Context,
	kbID, rawURL, requested string,
	expected ...types.FeishuCredentialRef,
) (*types.FeishuPersonalImport, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	kb, err := p.service.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	cfg, err := p.service.app(ctx)
	if err != nil {
		return nil, err
	}
	mode, err := cfg.importMode(requested)
	if err != nil {
		return nil, err
	}
	m := types.FeishuPersonalImport{
		ID:        uuid.NewString(),
		AuthMode:  mode,
		UserID:    user,
		AppID:     cfg.AppID,
		KBID:      kbID,
		URL:       rawURL,
		Status:    "accepted",
		UpdatedAt: time.Now(),
	}
	a := types.FeishuPersonalAttempt{
		AuthMode:         mode,
		CallerTenantID:   types.CallerFromContext(ctx).TenantID,
		AppConfigVersion: cfg.identityVersion(mode),
	}
	switch mode {
	case types.FeishuApplicationGrant:
		if !applicationUserAllowed(types.IsSystemAdminFromContext(ctx)) || !applicationKBAllowed(kbID) ||
			!p.firstCreateOnly {
			return nil, ErrFeishuApplicationRestricted
		}
		m.ApplicationID = cfg.AppID
		a.ApplicationID = cfg.AppID
	case types.FeishuUserOAuth:
		var connection types.FeishuConnection
		if err = p.service.db.WithContext(
			ctx,
		).
			Where(
				"user_id = ? AND app_id = ?",
				user,
				cfg.AppID,
			).
			First(
				&connection,
			).
			Error; err != nil {
			return nil, ErrFeishuReconnect
		}
		var epoch types.FeishuPersonalGrantEpoch
		if err = p.service.db.WithContext(ctx).Where("user_id = ?", user).First(&epoch).Error; err != nil {
			return nil, ErrFeishuReconnect
		}
		m.OpenID = connection.OpenID
		a.GrantEpoch = epoch.Epoch
	default:
		return nil, repository.ErrPersonalIdentity
	}
	if len(expected) > 0 {
		ref := expected[0]
		connection := connectionID(mode, cfg.AppID, a.AppConfigVersion)
		if mode == types.FeishuUserOAuth {
			connection = connectionID(mode, user, cfg.AppID, m.OpenID, a.GrantEpoch)
		}
		if ref.ConnectionID != connection || ref.ConfigVersion != a.AppConfigVersion ||
			ref.AppID != cfg.AppID {
			return nil, ErrFeishuReconnect
		}
	}

	remote, err := p.authorize(ctx, &m, &a)
	if err != nil {
		return nil, err
	}
	remote, cancel := context.WithTimeout(remote, 2*time.Minute)
	defer cancel()
	resource, err := core.ParsePersonalResourceURL(rawURL)
	if err != nil {
		return nil, err
	}
	resource, err = p.resolveResource(remote, &m, resource)
	if err != nil {
		return nil, err
	}
	if resource.Modified == "" {
		return nil, errors.New("personal_resource_revision_unavailable")
	}
	m.ResourceType, m.DocumentToken, m.URL = resource.Type, resource.Token, resource.URL
	a.Revision = resource.Modified
	if cfg.Capabilities != nil {
		var protected int64
		if e := p.service.db.
			WithContext(ctx).
			Model(&types.FeishuPersonalImport{}).
			Where("app_id = ? AND kb_id = ? AND resource_type = ? AND document_token = ? AND status IN ?",
				m.AppID,
				m.KBID,
				m.ResourceType,
				m.DocumentToken,
				[]string{
					"paused",
					"hold",
					"revoked",
				}).
			Count(&protected).Error; e != nil {
			return nil, e
		}
		if protected > 0 {
			return nil, repository.ErrPersonalIdentity
		}
	}
	if err := p.service.checkSharingApproval(p.service.db.WithContext(ctx), cfg, &m, a.Revision); err != nil {
		return nil, err
	}
	if cfg.Capabilities != nil {
		var managed types.FeishuPersonalImport
		err := p.service.db.WithContext(ctx).
			Where("user_id = ? AND app_id = ? AND kb_id = ? AND auth_mode = ? AND "+
				"resource_type = ? AND document_token = ? AND status = ?",
				user, m.AppID, kbID, mode, m.ResourceType, m.DocumentToken, "source_managed").
			First(&managed).Error
		if err == nil {
			// Discovery observes existing knowledge; its independent datasource
			// owns all updates, pauses, holds and cursor/version transitions.
			return &managed, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	resourceState, accepted, err := p.repo.Prepare(ctx, &m, &a)
	if err != nil {
		return nil, err
	}
	m.LogicalKnowledgeID = resourceState.LogicalKnowledgeID
	m.AttemptID = accepted.ID
	m.Status = accepted.Status
	// Acceptance is the DB commit. Redis failure never changes this fact.

	if ks, ok := p.service.knowledge.(*knowledgeService); ok {
		if dispatch, ok := ks.repo.(interfaces.KnowledgeDispatchRepository); ok {
			if op, loadErr := dispatch.GetKnowledgeDispatch(
				ctx,
				accepted.CallerTenantID,
				accepted.CandidateVersionID,
			); loadErr == nil {
				_ = ks.publishKnowledgeDispatch(ctx, op)
			}
		}
	}

	return &m, nil
}

// Dispatchable provides the dispatchable operation.
func (p *PersonalRecovery) Dispatchable(ctx context.Context, intent types.KnowledgeDispatch) (bool, error) {
	m, r, a, err := p.repo.Load(ctx, intent.AttemptID)
	if err != nil {
		return false, err
	}
	if intent.PayloadVersion != manifestPayloadVersion(m) || !applicationPayloadMatches(m, intent.Task) ||
		intent.Task.PersonalAttemptID != a.ID ||
		intent.Task.CandidateVersionID != a.CandidateVersionID ||
		intent.Task.KnowledgeID != a.CandidateVersionID ||
		intent.Task.TenantID != a.CallerTenantID ||
		intent.Task.KnowledgeBaseID != m.KBID ||
		intent.SourceID != m.ID ||
		intent.Revision != a.Revision {
		return false, repository.ErrPersonalIdentity
	}
	if r.CurrentAttemptID != a.ID || a.Status == "awaiting_index_isolation" || a.Status == "obsolete" ||
		a.Status == "needs_attention" {
		return false, nil
	}
	if a.RetryAfter != nil && a.RetryAfter.After(time.Now()) {
		return false, nil
	}
	if a.LeaseUntil != nil && a.LeaseUntil.After(time.Now()) {
		return false, nil
	}
	_, err = p.authorize(ctx, m, a)
	if err != nil {
		if personalAuthorizationLost(err) {
			return false, errors.Join(
				err,
				p.repo.Invalidate(ctx, a.ID, "authorization_revalidation_failed"),
			)
		}
		return false, err
	}
	return true, nil
}

// Process provides the process operation.
func (p *PersonalRecovery) Process(ctx context.Context, payload types.DocumentProcessPayload) error {
	m, _, a, err := p.repo.Load(ctx, payload.PersonalAttemptID)
	if err != nil {
		return err
	}
	if !applicationPayloadMatches(m, payload) {
		return repository.ErrPersonalIdentity
	}
	if m.Status == "source_managed" {
		return nil
	}
	ctx, err = p.privateWorkerContext(ctx, m, a)
	if err != nil {
		return err
	}
	if p.firstCreateOnly && payload.FirstCreate {
		return p.processFirstCreateDocument(ctx, payload)
	}
	if p.firstCreateOnly {
		return p.processFirstCreateManifest(ctx, payload)
	}
	return p.processPrivateManifest(ctx, payload)
}

func (p *PersonalRecovery) processPrivateManifest(
	ctx context.Context,
	payload types.DocumentProcessPayload,
) error {
	m, resource, a, err := p.repo.Load(ctx, payload.PersonalAttemptID)
	if err != nil {
		return err
	}
	if payload.CandidateVersionID != a.CandidateVersionID || payload.KnowledgeID != a.CandidateVersionID ||
		payload.TenantID != a.CallerTenantID ||
		payload.KnowledgeBaseID != m.KBID ||
		payload.DispatchID != "personal-"+a.ID {
		return repository.ErrPersonalIdentity
	}
	if resource.CurrentAttemptID != a.ID || a.Status == "awaiting_index_isolation" ||
		a.Status == "obsolete" ||
		a.Status == "needs_attention" {
		return nil
	}
	remote, err := p.authorize(ctx, m, a)
	if err != nil {
		if personalAuthorizationLost(err) {
			return errors.Join(err, p.repo.Invalidate(ctx, a.ID, "authorization_revalidation_failed"))
		}
		return err
	}
	owner, err := p.repo.Claim(ctx, a.ID, p.lease)
	if errors.Is(err, repository.ErrPersonalBusy) || errors.Is(err, repository.ErrPersonalFence) {
		return nil
	}
	if err != nil {
		return err
	}
	fetchTimeout := 2 * time.Minute
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant &&
		(m.ResourceType == "sheet" || m.ResourceType == "bitable") {
		fetchTimeout = 15 * time.Minute
	}
	work, cancel := context.WithTimeout(remote, fetchTimeout)
	defer cancel()
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(p.lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-work.Done():
				return
			case <-ticker.C:
				if _, e := p.authorize(work, m, a); e != nil {
					cancel()
					return
				}
				if e := p.mutate(work, owner, p.lease, nil); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(done); cancel(); <-stopped }()
	observedWork := core.WithAPIObserver(work, func(event core.APIObservation) {
		p.service.recordFeishuAPIError(work, m, owner, event)
	})
	finishErr := p.fetchCandidate(observedWork, m, owner)
	if finishErr != nil {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if _, authErr := p.authorize(cleanup, m, a); personalAuthorizationLost(authErr) ||
			personalAuthorizationLost(finishErr) {
			category := "authorization_revalidation_failed"
			if precise := feishuRemoteFailureCategory(finishErr); precise != "" {
				category = precise
			}
			if errors.Is(finishErr, core.ErrApplicationTableMedia) {
				category = "candidate_media_not_supported"
			} else if errors.Is(
				finishErr,
				core.ErrApplicationTableIncomplete,
			) ||
				errors.Is(
					finishErr,
					ErrFeishuIncompleteContent,
				) {
				category = "candidate_content_incomplete"
			}
			return errors.Join(finishErr, p.repo.Invalidate(cleanup, a.ID, category))
		}
		persistErr := p.mutate(
			cleanup,
			owner,
			0,
			func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
				category, retryable := personalCandidateFailure(finishErr)
				// A committed manifest can be adopted after an ambiguous commit;
				// this does not repeat the external read.
				if _, readErr := p.store.read(current); readErr == nil {
					retryable = true
				}
				current.Status = "recovery"
				current.RetryCount++
				delay := time.Duration(1<<min(current.RetryCount, 8)) * time.Second
				next := time.Now().Add(delay)
				current.RetryAfter = &next
				current.LeaseUntil = nil
				current.OwnerNonce = ""
				current.LastErrorCategory = category
				if !retryable || current.RetryCount > 3 {
					current.Status = "needs_attention"
					current.RetryAfter = nil
					if retryable {
						current.LastErrorCategory = "retry_exhausted"
					}
					if err := tx.Model(&types.FeishuPersonalImport{}).Where("id = ?", current.ImportID).
						Update("status", "needs_attention").Error; err != nil {
						return err
					}
				}
				op := "recovery"
				if current.Status == "needs_attention" {
					op = "attention"
				}
				return tx.Model(&types.TaskPendingOp{}).
					Where("scope = ? AND scope_id = ?", types.KnowledgeDispatchScope, current.CandidateVersionID).
					Update("op", op).
					Error
			},
		)
		return errors.Join(finishErr, persistErr)
	}
	return nil
}

func personalCandidateFailure(err error) (string, bool) {
	if os.IsPermission(err) {
		return "storage_permission_denied", false
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return "storage_unavailable", false
	}
	if errors.Is(err, core.ErrRateLimited) || errors.Is(err, core.ErrTransient) ||
		errors.Is(err, context.DeadlineExceeded) {
		return "temporary_failure", true
	}
	if errors.Is(err, context.Canceled) {
		return "worker_interrupted", true
	}
	if err != nil && err.Error() == "personal_resource_revision_changed" {
		return "source_version_changed", false
	}
	return "candidate_requires_review", false
}

func (p *PersonalRecovery) fetchCandidate(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	owner *types.FeishuPersonalAttempt,
) error {
	// A planned path is committed before any external write. If a prior owner
	// published it, ownership+hash validation adopts it without downloading again.
	var manifest *personalManifest
	var err error
	if owner.ManifestKey != "" {
		manifest, err = p.store.read(owner)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if manifest == nil {
		key := fmt.Sprintf("%s/%s/f%d", owner.ID, owner.CandidateVersionID, owner.Fence)
		if err = p.mutate(
			ctx,
			owner,
			p.lease,
			func(_ *gorm.DB, current *types.FeishuPersonalAttempt) error { current.ManifestKey = key; return nil },
		); err != nil {
			return err
		}
		owner.ManifestKey = key
		client := p.service.client(&core.Config{AppID: m.AppID})
		r := core.PersonalResource{Type: m.ResourceType, Token: m.DocumentToken, URL: m.URL}
		r, err = p.resolveResource(ctx, m, r)
		if err != nil {
			return err
		}
		if r.Type != m.ResourceType || r.Token != m.DocumentToken || r.Modified != owner.Revision {
			return errors.New("personal_resource_revision_changed")
		}
		var data []byte
		var name string
		var assets []core.ApplicationAttachment
		if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
			if r.Type == "sheet" || r.Type == "bitable" {
				ctx = p.applicationAttachmentContext(ctx, m, owner)
				var snapshot *core.ApplicationTableSnapshot
				snapshot, err = client.ReadApplicationTableCandidate(ctx, r)
				if err == nil {
					var text string
					text, err = snapshot.IndexText()
					data = []byte(text)
					name = core.SanitizeFileName(r.Title) + ".md"
					assets = snapshot.Attachments
				}
			} else {
				data, name, err = client.DownloadApplicationResource(ctx, r)
			}
		} else {
			data, name, err = client.DownloadPersonalResource(ctx, r)
		}
		if err != nil {
			return err
		}
		if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
			if err = p.validateApplicationContent(ctx, client, r, data, name); err != nil {
				return err
			}
		} else if _, err = client.ValidateWorkbookCoverage(ctx, r, data); err != nil {
			return err
		}
		verified, verifyErr := p.resolveResource(ctx, m, r)
		if verifyErr != nil {
			return verifyErr
		}
		if verified.Modified != owner.Revision || verified.Type != m.ResourceType ||
			verified.Token != m.DocumentToken {
			return errors.New("personal_resource_revision_changed_during_fetch")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		manifest, err = p.store.put(owner, data, name, assets...)
		if err != nil {
			return err
		}
	}
	if _, err = p.authorize(ctx, m, owner); err != nil {
		return err
	}
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuUserOAuth {
		if _, err = p.service.UserToken(ctx, m.UserID, m.AppID, m.OpenID); err != nil {
			return err
		}
	}
	return p.mutate(ctx, owner, 0, func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
		current.ManifestKey = owner.ManifestKey
		current.ManifestHash = manifest.Hash
		current.ManifestSize = manifest.Size
		current.ManifestName = manifest.Name
		current.Status = "awaiting_index_isolation"
		current.LeaseUntil = nil
		current.OwnerNonce = ""
		current.LastErrorCategory = ""
		if err := tx.Model(
			&types.FeishuPersonalImport{},
		).
			Where(
				"id = ?",
				m.ID,
			).
			Update(
				"status",
				current.Status,
			).
			Error; err != nil {
			return err
		}
		return tx.Model(&types.TaskPendingOp{}).
			Where("scope = ? AND scope_id = ?", types.KnowledgeDispatchScope, current.CandidateVersionID).
			Update("op", "completed").
			Error
	})
}

// Reconcile provides the reconcile operation.
func (p *PersonalRecovery) Reconcile(ctx context.Context) error {
	var after string
	var failures []error
	if err := p.service.reconcileConnectionHealth(ctx); err != nil {
		failures = append(failures, err)
	}
	if err := p.service.ReconcileCollections(ctx); err != nil {
		failures = append(failures, err)
	}
	if err := p.service.ReconcileBatches(ctx); err != nil {
		failures = append(failures, err)
	}
	for {
		rows, err := p.repo.List(ctx, after, 100)
		if err != nil {
			return errors.Join(errors.Join(failures...), err)
		}
		if len(rows) == 0 {
			return errors.Join(failures...)
		}
		for _, a := range rows {
			after = a.ID
			if a.Status == "needs_attention" {
				continue
			}
			m, _, current, err := p.repo.Load(ctx, a.ID)
			if err == nil && m.Status == "source_managed" {
				continue
			}
			if err == nil {
				_, err = p.authorize(ctx, m, current)
			}

			if err != nil {
				if current != nil && exactPersonalRecoveryAllows(current.LogicalKnowledgeID) &&
					current.AuthMode == types.FeishuUserOAuth {
					// Revocation is not permission to cancel a concurrently
					// renewed generation or rewrite a published execution.
					failures = append(failures, err, p.closeExactReconcileExecution(ctx, current))
					continue
				}
				if personalAuthorizationLost(err) || errors.Is(err, gorm.ErrRecordNotFound) {
					failures = append(
						failures,
						p.repo.Invalidate(ctx, a.ID, "authorization_or_resource_missing"),
					)
				} else {
					failures = append(failures, err)
				}
				continue
			}

			if a.ManifestHash != "" {
				if _, err = p.store.read(&a); err != nil {
					if exactPersonalRecoveryAllows(current.LogicalKnowledgeID) &&
						current.AuthMode == types.FeishuUserOAuth {
						failures = append(failures, err, p.closeExactReconcileExecution(ctx, current))
						continue
					}
					failures = append(failures, p.repo.Invalidate(ctx, a.ID, "manifest_missing_or_invalid"))
					continue
				}
			}
			if p.firstCreateOnly && current.Status == "awaiting_index_isolation" {
				if err = p.processFirstCreateManifest(ctx, firstCreateRecoveryPayload(m, current)); err != nil {
					failures = append(failures, err)
				}
			}
			if p.firstCreateOnly && m.KnowledgeID != "" {
				if err = p.reconcileFirstCreatePublic(ctx, m, current); err != nil {
					failures = append(failures, err)
				}
			}
			if err = p.repo.ReconcileIntent(ctx, a.ID); err != nil {
				failures = append(failures, err)
			}
		}
	}
}

func personalAccessError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return access.ErrForbidden
	}
	return err
}

func personalAuthorizationLost(err error) bool {
	return errors.Is(err, core.ErrApplicationTableIncomplete) ||
		errors.Is(err, core.ErrApplicationTableMedia) ||
		errors.Is(err, ErrFeishuApplicationRestricted) ||
		errors.Is(err, ErrFeishuIncompleteContent) ||
		errors.Is(err, core.ErrApplicationIncompleteContent) ||
		errors.Is(err, access.ErrForbidden) ||
		errors.Is(err, access.ErrNotFound) ||
		errors.Is(err, ErrFeishuReconnect) ||
		errors.Is(err, repository.ErrPersonalIdentity) ||
		errors.Is(err, core.ErrAuth) ||
		errors.Is(err, core.ErrAppScope) || errors.Is(err, core.ErrUserScope) ||
		errors.Is(err, ErrFeishuOAuthConfiguration) ||
		errors.Is(err, core.ErrPermission) ||
		errors.Is(err, core.ErrNotFound)
}

// Recheck local authorization at every fenced DB commit, including heartbeat.
// SHARE locks keep a concurrent local revocation from committing between this
// check and the state write. No Feishu/Redis/model request occurs in this tx.
func (p *PersonalRecovery) mutate(
	ctx context.Context,
	owner *types.FeishuPersonalAttempt,
	lease time.Duration,
	fn func(*gorm.DB, *types.FeishuPersonalAttempt) error,
) error {
	return p.repo.Mutate(ctx, owner, lease, func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error {
		var m types.FeishuPersonalImport
		if err := tx.Where("id = ?", a.ImportID).First(&m).Error; err != nil {
			return personalAccessError(err)
		}
		var user types.User
		if err := tx.Clauses(
			clause.Locking{
				Strength: "SHARE",
			},
		).
			Where(
				"id = ? AND is_active = ?",

				m.UserID,

				true,
			).
			First(
				&user,
			).
			Error; err != nil {
			return personalAccessError(err)
		}
		var member types.TenantMember
		if err := tx.Clauses(
			clause.Locking{
				Strength: "SHARE",
			},
		).
			Where(
				"user_id = ? AND tenant_id = ? AND status = ?",

				m.UserID,

				a.CallerTenantID,

				types.TenantMemberStatusActive,
			).
			First(
				&member,
			).
			Error; err != nil {
			return personalAccessError(err)
		}
		if !member.Role.HasPermission(types.TenantRoleContributor) {
			return access.ErrForbidden
		}
		var tenant types.Tenant
		if err := tx.Clauses(
			clause.Locking{
				Strength: "SHARE",
			},
		).
			Where(
				"id = ? AND status = ?",

				a.CallerTenantID,

				"active",
			).
			First(
				&tenant,
			).
			Error; err != nil {
			return personalAccessError(err)
		}
		ownerContext := context.WithValue(tx.Statement.Context, types.UserIDContextKey, user.ID)
		ownerContext = types.WithCaller(ownerContext, types.Caller{
			TenantID: member.TenantID, UserID: user.ID, Role: member.Role,
		})
		ownerContext = types.WithPrincipal(
			ownerContext,
			types.Principal{Type: types.PrincipalWebUser, ID: user.ID},
		)
		tx = tx.WithContext(ownerContext)
		if _, err := personalAuthorizationKB(tx, m.KBID, true); err != nil {
			return personalAccessError(err)
		}
		if err := p.service.requireImportWriter(tx, m.UserID, m.KBID, a.CallerTenantID); err != nil {
			return err
		}
		var setting types.SystemSetting
		if err := tx.Clauses(
			clause.Locking{
				Strength: "SHARE",
			},
		).
			Where(
				"key = ?",

				feishuAppSetting,
			).
			First(
				&setting,
			).
			Error; err != nil {
			return personalAccessError(err)
		}
		cipher, err := setting.AsString()
		if err != nil {
			return err
		}
		var cfg FeishuAppConfig
		if err = openFeishu(cipher, &cfg); err != nil {
			return err
		}
		if !cfg.Enabled || cfg.identityVersion(types.FeishuAuthMode(m.AuthMode)) != a.AppConfigVersion ||
			cfg.AppID != m.AppID ||
			!cfg.modeEnabled(types.FeishuAuthMode(m.AuthMode)) ||
			types.FeishuAuthMode(a.AuthMode) != types.FeishuAuthMode(m.AuthMode) {
			return repository.ErrPersonalIdentity
		}
		if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant &&
			(!applicationUserAllowed(user.IsSystemAdmin) || !applicationKBAllowed(m.KBID)) {
			return ErrFeishuApplicationRestricted
		}
		if err := p.service.checkSharingApproval(tx, &cfg, &m, a.Revision); err != nil {
			return err
		}
		if types.FeishuAuthMode(m.AuthMode) == types.FeishuUserOAuth {
			if err := p.service.assertConnectionHealth(tx, m.UserID); err != nil {
				return err
			}
		}
		if fn != nil {
			return fn(tx, a)
		}
		return nil
	})
}
