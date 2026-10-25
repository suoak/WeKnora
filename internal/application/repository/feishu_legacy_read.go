package repository

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func legacyTenantModelGate(ctx context.Context, db *gorm.DB, tenant uint64) error {
	if err := types.CheckLegacyReadScope(db.WithContext(ctx)); err != nil {
		return err
	}
	if !types.LegacyReadGuardsEnabled(db) {
		return nil
	}
	var denied bool
	sourceStates := []string{"hold", "revoked"}
	if types.WorkspaceWebReadTenant(ctx) == tenant {
		sourceStates = []string{"revoked"}
	}
	err := db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE tenant_id=? AND state IN ('hold','revoked'))
 OR EXISTS(SELECT 1 FROM data_sources WHERE tenant_id=? AND legacy_access_state IN ?)`,
			tenant, tenant, sourceStates).
		Scan(&denied).
		Error
	if err != nil {
		return err
	}
	if denied {
		return types.ErrLegacySourceRestricted
	}
	return nil
}

func (r *tenantRepository) CheckLegacyReadScope(ctx context.Context) error {
	return types.CheckLegacyReadScope(r.db.WithContext(ctx))
}

func (r *modelRepository) CheckLegacyModelInputs(ctx context.Context, tenant uint64) error {
	if err := types.CheckFeishuSourceRead(ctx); err != nil {
		return err
	}
	if scoped, err := checkWikiMCPModelInputs(r.db, ctx, tenant); scoped {
		return err
	}
	return legacyTenantModelGate(ctx, r.db, tenant)
}

func (r *knowledgeRepository) CheckLegacyKBRead(ctx context.Context, kb string) error {
	return types.CheckLegacySourceRead(r.db.WithContext(ctx), "", kb)
}

func (r *resourceRepository) CheckLegacyFileRead(
	ctx context.Context,
	tenant uint64,
	path string,
) (bool, error) {
	return (&tenantRepository{db: r.db}).CheckLegacyFileRead(ctx, tenant, path)
}

// File paths of enrolled versions are a revocable reference, including links
// signed before an update/withdrawal. It never grants access to a new path.
func (r *tenantRepository) CheckLegacyFileRead(
	ctx context.Context,
	tenant uint64,
	path string,
) (bool, error) {
	legacy := types.LegacyReadGuardsEnabled(r.db)
	private := types.PrivateKBGuardsEnabled(r.db)
	if !legacy && !private {
		return false, nil
	}
	if legacy && r.db.Name() != "postgres" {
		// SQLite has schema parity but cannot publish this feature. Preserve
		// ordinary file compatibility without attempting PostgreSQL jsonb SQL;
		// any managed state on an unsupported backend remains closed.
		var managed bool
		err := r.db.WithContext(ctx).
			Raw(`SELECT EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE tenant_id=?) OR EX`+
				`ISTS(SELECT 1 FROM data_sources WHERE tenant_id=? AND legacy_access_state<>'`+
				`unmanaged')`,
				tenant,
				tenant).
			Scan(&managed).
			Error
		if err != nil {
			return false, err
		}
		if managed {
			return true, types.ErrLegacySourceRestricted
		}
		if !private {
			return false, nil
		}
	}
	type binding struct{ State, ActiveAttempt, Attempt, Path, KBID string }
	var bindings []binding
	bindingSQL := ""
	bindingArgs := []any{}
	if legacy && r.db.Name() == "postgres" {
		bindingSQL = `SELECT state,active_attempt,'' AS attempt,original_file_path AS path,knowledge_base_id AS kb_id
 FROM feishu_legacy_resources WHERE tenant_id=?
 UNION ALL SELECT r.state,
r.active_attempt,
a.id AS attempt,
a.manifest->'knowledge'->>'file_path' AS path,
r.knowledge_base_id AS kb_id
 FROM feishu_legacy_resources r JOIN feishu_legacy_attempts a ON a.resource_id=r.id WHERE r.tenant_id=?`
		bindingArgs = []any{tenant, tenant}
	}
	if private {
		if bindingSQL != "" {
			bindingSQL += " UNION ALL "
		}
		bindingSQL += `SELECT 'active' AS state,'' AS active_attempt,'' AS attempt,k.file_path AS p` +
			`ath,k.knowledge_base_id AS kb_id FROM knowledges k JOIN feishu_private_kbs p` +
			` ON p.kb_id=k.knowledge_base_id WHERE k.tenant_id=? AND k.deleted_at IS NULL`
		bindingArgs = append(bindingArgs, tenant)
	}
	err := r.db.WithContext(ctx).Raw(bindingSQL, bindingArgs...).Scan(&bindings).Error
	if err != nil {
		return false, err
	}
	normalize := func(p string) (string, error) {
		if handle, ok := types.ParseResourcePath(p); ok {
			var resource struct {
				PhysicalPath, Provider string
				TenantID               uint64
			}
			if e := r.db.
				WithContext(ctx).
				Raw("SELECT physical_path,provider,tenant_id FROM resources WHERE handle=? AND st"+
					"ate='active' AND deleted_at IS NULL",
					handle).
				Scan(&resource).Error; e != nil {
				return "", e
			}
			if resource.TenantID != tenant || resource.Provider != "local" || resource.PhysicalPath == "" {
				return "", types.ErrLegacySourceRestricted
			}
			p = resource.PhysicalPath
		}
		if _, inner, ok := types.ParseStorageBackendPath(p); ok {
			p = inner
		}
		p = strings.TrimPrefix(p, "local://")
		for i := 0; i < 4; i++ {
			decoded, e := url.PathUnescape(p)
			if e != nil {
				return "", types.ErrLegacySourceRestricted
			}
			if decoded == p {
				break
			}
			p = decoded
			if i == 3 {
				return "", types.ErrLegacySourceRestricted
			}
		}
		p = filepath.Clean(p)
		if resolved, e := filepath.EvalSymlinks(p); e == nil {
			p = resolved
		}
		return strings.ReplaceAll(p, "\\", "/"), nil
	}
	path, err = normalize(path)
	if err != nil {
		return false, err
	}
	for _, b := range bindings {
		if b.Path == "" {
			continue
		}
		p, e := normalize(b.Path)
		if e != nil {
			return false, e
		}
		end := strings.LastIndex(p, "/")
		if end < 1 {
			continue
		}
		dir := p[:end+1]
		if !strings.HasPrefix(path, dir) {
			continue
		}
		if b.State != "active" || b.Attempt != b.ActiveAttempt {
			return true, types.ErrLegacySourceRestricted
		}
		if e := types.CheckLegacySourceRead(r.db.WithContext(ctx), "", b.KBID); e != nil {
			return true, e
		}
		return true, nil
	}
	if !legacy || r.db.Name() != "postgres" {
		return false, nil
	}
	// Even a not-yet-prepared attempt is private. SaveFile namespaces all its
	// artifacts under the attempt ID; never authorize an unpublished directory.
	var attempts []struct{ ID, ActiveAttempt, State string }
	err = r.db.WithContext(ctx).
		Raw(`SELECT a.id,r.active_attempt,r.state FROM feishu_legacy_attempts a JOIN feis`+
			`hu_legacy_resources r ON r.id=a.resource_id WHERE r.tenant_id=?`,
			tenant).
		Scan(&attempts).
		Error
	if err != nil {
		return false, err
	}
	for _, a := range attempts {
		if strings.Contains("/"+strings.Trim(path, "/")+"/", "/"+a.ID+"/") {
			if a.State != "active" || a.ID != a.ActiveAttempt {
				return true, types.ErrLegacySourceRestricted
			}
		}
	}
	// Source guards also cover files before enrollment and subsequent holds
	// during an already-running download. Do not cache these anonymous paths.
	var sourcePaths []struct{ Path, KBID string }
	err = r.db.WithContext(ctx).
		Raw(`SELECT k.file_path AS path,
k.knowledge_base_id AS kb_id FROM knowledges k JOIN data_sources d ON k.metadata->>'datasource_id'=d.id
 WHERE k.tenant_id=? AND (d.type IN ('feishu',
'feishu_drive',
'lark',
'lark_drive') OR d.legacy_access_state IN ('hold',
'revoked'))`,
			tenant).
		Scan(&sourcePaths).
		Error
	if err != nil {
		return false, err
	}
	for _, binding := range sourcePaths {
		p := binding.Path
		if p == "" {
			continue
		}
		p, e := normalize(p)
		if e != nil {
			return false, e
		}
		i := strings.LastIndex(p, "/")
		if i > 0 && strings.HasPrefix(path, p[:i+1]) {
			return true, types.CheckLegacySourceRead(r.db.WithContext(ctx), "", binding.KBID)
		}
	}
	return false, nil
}
