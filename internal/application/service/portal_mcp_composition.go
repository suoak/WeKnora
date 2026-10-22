package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// NewPortalPersonalKnowledge composes the existing application repositories,
// user Key lifecycle, shares, model and retrieval registries. It does not start
// a listener, approve content or create membership. Missing config stays off.
func NewPortalPersonalKnowledge(cfg *config.Config, db *gorm.DB, members interfaces.TenantMemberService, keys interfaces.TenantAPIKeyRepository, kbs interfaces.KnowledgeBaseRepository, tenants interfaces.TenantRepository, knowledge interfaces.KnowledgeRepository, chunks interfaces.ChunkRepository, models interfaces.ModelService, registry interfaces.RetrieveEngineRegistry, ownership retriever.TenantStoreOwnership, shares interfaces.KBShareService, agents interfaces.AgentShareService) *PortalMCPKnowledge {
	if cfg == nil || cfg.PersonalMCP == nil || !cfg.PersonalMCP.Enabled || cfg.PersonalMCP.PolicyReference == "" {
		return nil
	}
	liveKeys, ok := keys.(interfaces.UserMCPAPIKeyRepository)
	if !ok || db == nil || members == nil || kbs == nil || tenants == nil || knowledge == nil || chunks == nil || models == nil || registry == nil {
		return nil
	}
	store := repository.NewEnterpriseAccessRepository(db)
	accounts := &repository.KnowHubPersonalUserVerifier{DB: db}
	public := access.NewEnterprisePublicReader(access.EnterprisePublicOptions{Enabled: true, PortalPublishedDefault: true}, store, accounts, chunks)
	base := &access.LiveDocumentBaseVerifier{Members: members, Keys: liveKeys, KBs: kbs, Shares: shares, Agents: agents, EnterpriseScope: func(ctx context.Context, tenant uint64, kb string) error {
		grant, err := public.Authorize(ctx, kb)
		if err != nil {
			return err
		}
		if !grant.ResourceMatches(tenant, kb) {
			return access.ErrForbidden
		}
		return nil
	}}
	documents := access.NewDocumentReader(true, store, base, accounts)
	resolver := &access.PortalMCPResolver{Enabled: true, PublishedDefaults: true, Directory: store, Store: store, Public: public, Documents: documents, Employees: accounts}
	s := &PortalMCPKnowledge{Resolver: resolver, Knowledge: knowledge, Chunks: chunks, Wiki: &repository.PersonalWikiStore{DB: db}}
	BindPortalMCPAdapters(s, kbs, tenants, models, registry, ownership)
	return s
}

// Existing callers can keep NewPortalService. Application DI adds only this
// explicit, default-disabled policy configuration; no SystemAdmin auto-grant.
func NewConfiguredPortalService(cfg *config.Config, db *gorm.DB, repo interfaces.PortalRepository, tenants interfaces.TenantService, members interfaces.TenantMemberService, orgs interfaces.OrganizationRepository, audit interfaces.AuditLogService) interfaces.PortalService {
	s := NewPortalService(db, repo, tenants, members, orgs, audit)
	if cfg != nil && cfg.PersonalMCP != nil {
		_ = ConfigurePortalPublication(s, PortalPolicyAuthorityConfig{Enabled: cfg.PersonalMCP.ReviewEnabled, AllowSystemAdminReview: cfg.PersonalMCP.SystemAdminReview, ContractRef: cfg.PersonalMCP.PolicyReference})
		s.(*portalService).publicationExceptionConfig = PortalPolicyAuthorityConfig{Enabled: cfg.PersonalMCP.ExceptionsEnabled, AllowSystemAdminReview: cfg.PersonalMCP.SystemAdminReview, ContractRef: cfg.PersonalMCP.PolicyReference}
	}
	return s
}
