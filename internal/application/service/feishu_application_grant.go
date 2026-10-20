package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"html"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
)

var ErrFeishuApplicationRestricted = errors.New("feishu_application_test_only")
var ErrFeishuIncompleteContent = errors.New("feishu_application_content_not_verified")

// General availability keeps the original restricted rollout as an explicit fallback.
func applicationGrantPublicEnabled() bool {
	return os.Getenv("FEISHU_APPLICATION_GRANT_ENABLED") == "1"
}
func applicationGrantEnabled() bool {
	return applicationGrantPublicEnabled() || os.Getenv("FEISHU_APPLICATION_GRANT_TEST_ONLY") == "1"
}
func applicationUserAllowed(systemAdmin bool) bool {
	return applicationGrantPublicEnabled() || systemAdmin
}

func applicationKBAllowed(kb string) bool {
	if !applicationGrantEnabled() || kb == "" {
		return false
	}
	if applicationGrantPublicEnabled() {
		return true
	}
	for _, id := range strings.Split(os.Getenv("FEISHU_APPLICATION_GRANT_TEST_KB_IDS"), ",") {
		if strings.TrimSpace(id) == kb {
			return true
		}
	}
	return false
}

func validateFeishuModeConfig(cfg *FeishuAppConfig) error {
	switch types.FeishuAuthMode(cfg.AuthMode) {
	case types.FeishuUserOAuth:
		return validateFeishuApp(cfg)
	case types.FeishuApplicationGrant:
		if cfg.RedirectURI != "" {
			return errors.New("application mode does not use OAuth callback")
		}
		if !strings.HasPrefix(cfg.AppID, "cli_") || len(cfg.AppID) > 128 || cfg.Secret == "" || len(cfg.Secret) > 1024 {
			return errors.New("invalid application credentials")
		}
		return nil
	default:
		return errors.New("invalid feishu auth mode")
	}
}

func (p *PersonalRecovery) authorizeApplication(ctx, fresh context.Context, cfg *FeishuAppConfig, m *types.FeishuPersonalImport, a *types.FeishuPersonalAttempt) (context.Context, error) {
	if !p.firstCreateOnly || !applicationKBAllowed(m.KBID) || m.OpenID != "" || a.GrantEpoch != "" || m.ApplicationID != cfg.AppID || a.ApplicationID != cfg.AppID {
		return nil, ErrFeishuApplicationRestricted
	}
	var user types.User
	if err := p.service.db.WithContext(ctx).Select("id", "is_system_admin").Where("id = ?", m.UserID).First(&user).Error; err != nil {
		return nil, err
	}
	if !applicationUserAllowed(user.IsSystemAdmin) {
		return nil, access.ErrForbidden
	}
	client := p.service.client(&core.Config{AppID: cfg.AppID, AppSecret: cfg.Secret})
	validate := func(call context.Context) error {
		_, current, err := p.authorizeLocal(call, m, a)
		if err != nil {
			return err
		}
		if !applicationKBAllowed(m.KBID) || current.AppID != m.ApplicationID || !p.firstCreateOnly {
			return ErrFeishuApplicationRestricted
		}
		var u types.User
		if err = p.service.db.WithContext(call).Select("id", "is_system_admin").Where("id = ? AND is_active = ?", m.UserID, true).First(&u).Error; err != nil {
			return err
		}
		if !applicationUserAllowed(u.IsSystemAdmin) {
			return access.ErrForbidden
		}
		return nil
	}
	provider := func(call context.Context) (string, error) {
		if err := validate(call); err != nil {
			return "", err
		}
		return client.GetTenantAccessToken(call)
	}
	authOnly := core.WithApplicationIdentityValidation(fresh, provider, validate)
	remote := core.WithApplicationIdentityValidation(fresh, func(call context.Context) (string, error) {
		token, err := provider(call)
		if err != nil {
			return "", err
		}
		if m.DocumentToken != "" {
			probe := core.WithApplicationIdentityValidation(call, provider, validate)
			probe = applicationTableContext(probe)
			resolved, err := client.ResolveApplicationResource(probe, core.PersonalResource{Type: m.ResourceType, Token: m.DocumentToken, URL: m.URL})
			if err != nil {
				return "", err
			}
			if resolved.Modified != a.Revision || resolved.Type != m.ResourceType || resolved.Token != m.DocumentToken {
				return "", repository.ErrPersonalIdentity
			}
		}
		return token, nil
	}, validate)
	authOnly = applicationTableContext(authOnly)
	remote = applicationTableContext(remote)
	// Live source ACL/revision check also applies when resuming an already saved
	// manifest or public task. No local epoch substitutes for Feishu resource ACL.
	if m.DocumentToken != "" {
		resolved, err := client.ResolveApplicationResource(authOnly, core.PersonalResource{Type: m.ResourceType, Token: m.DocumentToken, URL: m.URL})
		if err != nil {
			return nil, err
		}
		if resolved.Type != m.ResourceType || resolved.Token != m.DocumentToken || resolved.Modified != a.Revision {
			return nil, repository.ErrPersonalIdentity
		}
	}
	return remote, nil
}

func (p *PersonalRecovery) resolveResource(ctx context.Context, m *types.FeishuPersonalImport, r core.PersonalResource) (core.PersonalResource, error) {
	client := p.service.client(&core.Config{AppID: m.AppID})
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		return client.ResolveApplicationResource(applicationTableContext(ctx), r)
	}
	return client.ResolvePersonalResource(ctx, r)
}

// Off by default until the full P4 business acceptance gate passes. This is
// independent from general application-grant access; Docx remains available.
func applicationTableContext(ctx context.Context) context.Context {
	if os.Getenv("FEISHU_APPLICATION_TABLES_ENABLED") == "1" {
		return core.WithApplicationTables(ctx)
	}
	return ctx
}

func manifestPayloadVersion(m *types.FeishuPersonalImport) int {
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		return 4
	}
	return 2
}
func publicPayloadVersion(m *types.FeishuPersonalImport) int {
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		return 5
	}
	return 3
}
func applicationPayloadMatches(m *types.FeishuPersonalImport, t types.DocumentProcessPayload) bool {
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		return t.FeishuAuthMode == types.FeishuApplicationGrant && t.FeishuApplicationID == m.ApplicationID
	}
	return t.FeishuAuthMode == "" && t.FeishuApplicationID == ""
}

// This callback participates in the existing short admission/fence transaction.
// Only local SQL occurs; network, file, parser and queue calls stay outside it.
func (p *PersonalRecovery) validateApplicationIdentityTx(tx *gorm.DB, m *types.FeishuPersonalImport, a *types.FeishuPersonalAttempt) error {
	if !p.firstCreateOnly || !applicationKBAllowed(m.KBID) {
		return ErrFeishuApplicationRestricted
	}
	var u types.User
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_active = ?", m.UserID, true).First(&u).Error; err != nil {
		return personalAccessError(err)
	}
	if !applicationUserAllowed(u.IsSystemAdmin) {
		return access.ErrForbidden
	}
	var member types.TenantMember
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("user_id = ? AND tenant_id = ? AND status = ?", m.UserID, a.CallerTenantID, types.TenantMemberStatusActive).First(&member).Error; err != nil {
		return personalAccessError(err)
	}
	var tenant types.Tenant
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = ?", a.CallerTenantID, "active").First(&tenant).Error; err != nil {
		return personalAccessError(err)
	}
	var kb types.KnowledgeBase
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", m.KBID).First(&kb).Error; err != nil {
		return personalAccessError(err)
	}
	var setting types.SystemSetting
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("key = ?", feishuAppSetting).First(&setting).Error; err != nil {
		return err
	}
	cipher, err := setting.AsString()
	if err != nil {
		return err
	}
	var cfg FeishuAppConfig
	if err = openFeishu(cipher, &cfg); err != nil {
		return err
	}
	if !cfg.Enabled || cfg.Version != a.AppConfigVersion || types.FeishuAuthMode(cfg.AuthMode) != types.FeishuApplicationGrant || cfg.AppID != m.AppID || m.ApplicationID != cfg.AppID || a.ApplicationID != cfg.AppID {
		return repository.ErrPersonalIdentity
	}
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		return access.ErrForbidden
	}
	if kb.TenantID == a.CallerTenantID {
		if !member.Role.HasPermission(types.TenantRoleAdmin) && kb.CreatorID != m.UserID {
			return access.ErrForbidden
		}
	} else {
		if p.shares == nil {
			return access.ErrForbidden
		}
		perm, shared, err := p.shares.CheckTenantKBPermission(tx.Statement.Context, kb.ID, a.CallerTenantID, member.Role)
		if err != nil {
			return err
		}
		if !shared || !perm.HasPermission(types.OrgRoleEditor) {
			return access.ErrForbidden
		}
	}
	return nil
}

func normalizeApplicationText(s string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsSpace(c) || strings.ContainsRune("\\`*|_#>[]", c) {
			return -1
		}
		return c
	}, html.UnescapeString(s))
}

func (p *PersonalRecovery) validateApplicationContent(ctx context.Context, client *core.Client, r core.PersonalResource, data []byte, name string) error {
	if r.Type == "sheet" || r.Type == "bitable" {
		if os.Getenv("FEISHU_APPLICATION_TABLES_ENABLED") != "1" || filepath.Ext(name) != ".md" {
			return ErrFeishuIncompleteContent
		}
		snapshot, err := client.ReadApplicationTableCandidate(ctx, r)
		if err != nil {
			return err
		}
		text, err := snapshot.IndexText()
		if err != nil {
			return err
		}
		if !bytes.Equal(data, []byte(text)) {
			return ErrFeishuIncompleteContent
		}
		return nil
	}
	raw, err := client.ApplicationDocxText(ctx, r)
	if err != nil {
		return err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ErrFeishuIncompleteContent
	}
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "word/media/") || strings.HasPrefix(f.Name, "word/embeddings/") {
			return ErrFeishuIncompleteContent
		}
	}
	parsed, err := docparser.NewAnydocReader(nil, nil).Read(ctx, &types.ReadRequest{FileType: "docx", FileName: name, FileContent: data})
	if err != nil {
		return ErrFeishuIncompleteContent
	}
	if parsed == nil || len(parsed.ImageRefs) > 0 || strings.TrimSpace(parsed.MarkdownContent) == "" || strings.TrimSpace(raw) == "" {
		return ErrFeishuIncompleteContent
	}
	normalized := normalizeApplicationText(parsed.MarkdownContent)
	for _, line := range strings.Split(raw, "\n") {
		s := normalizeApplicationText(line)
		if s != "" && !strings.Contains(normalized, s) {
			return ErrFeishuIncompleteContent
		}
	}
	return nil
}

func applicationGrantEngineAvailable() bool { return anydoc.Available() }
func applicationTextHash(text string) string {
	sum := sha256.Sum256([]byte(normalizeApplicationText(text)))
	return hex.EncodeToString(sum[:])
}
func applicationVerifiedTextHash(ctx context.Context, data []byte, name string) (string, error) {
	if filepath.Ext(name) == ".md" && bytes.HasPrefix(data, []byte("<!-- knowhub-feishu-table-snapshot-v1 -->\n")) {
		parsed, err := (&docparser.SimpleFormatReader{}).Read(ctx, &types.ReadRequest{FileType: "md", FileName: name, FileContent: data})
		if err != nil || parsed == nil || parsed.MarkdownContent == "" {
			return "", ErrFeishuIncompleteContent
		}
		return applicationTextHash(parsed.MarkdownContent), nil
	}
	parsed, err := docparser.NewAnydocReader(nil, nil).Read(ctx, &types.ReadRequest{FileType: "docx", FileName: name, FileContent: data})
	if err != nil || parsed == nil || parsed.MarkdownContent == "" || len(parsed.ImageRefs) > 0 {
		return "", ErrFeishuIncompleteContent
	}
	return applicationTextHash(parsed.MarkdownContent), nil
}
func verifyApplicationParse(k *types.Knowledge, result *types.ReadResult) error {
	var meta map[string]any
	if json.Unmarshal(k.Metadata, &meta) != nil {
		return nil
	}
	if meta["feishu_auth_mode"] != types.FeishuApplicationGrant {
		return nil
	}
	expected, _ := meta["feishu_verified_text_sha256"].(string)
	if expected == "" || result == nil || len(result.ImageRefs) > 0 || applicationTextHash(result.MarkdownContent) != expected {
		return ErrFeishuIncompleteContent
	}
	return nil
}

func applicationPrincipalFingerprint(id, version string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id + "\x00" + version))
	return hex.EncodeToString(sum[:])
}
