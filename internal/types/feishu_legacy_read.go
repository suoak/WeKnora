package types

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrLegacySourceRestricted = errors.New("Feishu source access requires verification")

const legacyReadGuardSetting = "knowhub.feishu_legacy_read_guard"

type legacyReadGuardPlugin struct{}

func (legacyReadGuardPlugin) Name() string { return legacyReadGuardSetting }
func (legacyReadGuardPlugin) Initialize(db *gorm.DB) error {
	// Scan projections (wiki title/summary/index/count endpoints) do not run
	// AfterFind. Apply the same KB restriction before executing their SQL.
	filter := func(tx *gorm.DB) {
		if tx.Statement == nil {
			return
		}
		if _, internal := SyncExecutionFromContext(tx.Statement.Context); internal {
			return
		}
		table := tx.Statement.Table
		switch table {
		case "wiki_pages", "wiki_page_revisions", "wiki_folders", "wiki_page_issues":
			tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: `NOT EXISTS (SELECT 1 FROM feishu_legacy_resources lr WHERE lr.knowledge_base_id=` + table + `.knowledge_base_id AND lr.state IN ('hold','revoked')) AND NOT EXISTS (SELECT 1 FROM data_sources ld WHERE ld.knowledge_base_id=` + table + `.knowledge_base_id AND (ld.legacy_access_state='revoked' OR (ld.legacy_access_state='hold' AND NOT EXISTS (SELECT 1 FROM knowledge_bases owner_kb WHERE owner_kb.id=ld.knowledge_base_id AND owner_kb.tenant_id=?))))`, Vars: []interface{}{WorkspaceWebReadTenant(tx.Statement.Context)}}}})
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register(legacyReadGuardSetting, filter); err != nil {
		return err
	}
	return db.Callback().Row().Before("gorm:row").Register(legacyReadGuardSetting, filter)
}

// Installed on the real APP database after formal migration. Tests that use
// deliberately partial schemas do not silently invent source policy tables.
func EnableLegacyReadGuards(db *gorm.DB) *gorm.DB {
	// Register once at startup. GORM intentionally discards statement Settings
	// in some NewDB sessions (including hooks), whereas database plugins survive
	// those clones. Never put this security switch on a mutable request statement.
	if _, enabled := db.Config.Plugins[legacyReadGuardSetting]; !enabled {
		if err := db.Use(legacyReadGuardPlugin{}); err != nil {
			panic("cannot install mandatory Feishu source read guards")
		}
	}
	return db.Session(&gorm.Session{NewDB: true})
}
func LegacyReadGuardsEnabled(db *gorm.DB) bool {
	if db == nil || db.Config == nil {
		return false
	}
	_, enabled := db.Config.Plugins[legacyReadGuardSetting]
	return enabled
}

// Until derived provenance and mixed document ACLs are reliable, an uncertain
// legacy source closes the whole affected KB. No body is released then filtered.
func CheckLegacySourceRead(db *gorm.DB, knowledgeID, kbID string) error {
	if !LegacyReadGuardsEnabled(db) {
		return nil
	}
	if _, internal := SyncExecutionFromContext(db.Statement.Context); internal {
		return nil
	}
	if kbID == "" && knowledgeID != "" {
		if err := db.Raw("SELECT knowledge_base_id FROM knowledges WHERE id=?", knowledgeID).Scan(&kbID).Error; err != nil {
			return err
		}
	}
	if kbID == "" {
		return nil
	}
	var managed bool
	if err := db.Raw("SELECT EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_base_id=?) OR EXISTS(SELECT 1 FROM data_sources WHERE knowledge_base_id=? AND (type IN ('feishu','feishu_drive','lark','lark_drive') OR legacy_access_state<>'unmanaged'))", kbID, kbID).Scan(&managed).Error; err != nil {
		return err
	}
	if !managed {
		return nil
	}
	var valid struct{ TenantID uint64 }
	if err := db.Raw("SELECT k.tenant_id FROM knowledge_bases k JOIN tenants t ON t.id=k.tenant_id WHERE k.id=? AND k.deleted_at IS NULL AND t.status='active' AND t.deleted_at IS NULL", kbID).Scan(&valid).Error; err != nil {
		return err
	}
	if valid.TenantID == 0 {
		return ErrLegacySourceRestricted
	}
	var denied bool
	sourceStates := []string{"hold", "revoked"}
	if WorkspaceWebReadTenant(db.Statement.Context) == valid.TenantID {
		sourceStates = []string{"revoked"}
	}
	err := db.Raw(`SELECT EXISTS(SELECT 1 FROM feishu_legacy_resources lr WHERE knowledge_base_id=? AND state IN ('hold','revoked')
 AND (?=0 OR EXISTS(SELECT 1 FROM knowledges existing_doc WHERE existing_doc.id=lr.knowledge_id AND existing_doc.deleted_at IS NULL)))
 OR EXISTS(SELECT 1 FROM data_sources WHERE knowledge_base_id=? AND legacy_access_state IN ?)`, kbID, WorkspaceWebReadTenant(db.Statement.Context), kbID, sourceStates).Scan(&denied).Error
	if err != nil {
		return err
	}
	if denied {
		return ErrLegacySourceRestricted
	}
	recordLegacyRead(db.Statement.Context, kbID)
	return nil
}

func (k *Knowledge) AfterFind(tx *gorm.DB) error {
	return CheckLegacySourceRead(tx, k.ID, k.KnowledgeBaseID)
}
func (c *Chunk) AfterFind(tx *gorm.DB) error {
	return CheckLegacySourceRead(tx, c.KnowledgeID, c.KnowledgeBaseID)
}

type legacyKBAdminMetadataKey struct{}

// Narrow server-side metadata lookup for datasource administration. This does
// not authorize Knowledge, Chunk, Wiki, file or model content and is never
// derived from an HTTP header. Existing tenant/key/role checks still apply.
func WithLegacyKBAdminMetadata(ctx context.Context, kb string) context.Context {
	return context.WithValue(ctx, legacyKBAdminMetadataKey{}, kb)
}
func (k *KnowledgeBase) AfterFind(tx *gorm.DB) error {
	if kb, ok := tx.Statement.Context.Value(legacyKBAdminMetadataKey{}).(string); ok && kb == k.ID {
		return nil
	}
	return CheckLegacySourceRead(tx, "", k.ID)
}
func (p *WikiPageRevision) AfterFind(tx *gorm.DB) error {
	return CheckLegacySourceRead(tx, "", p.KnowledgeBaseID)
}
func (m *Message) AfterFind(tx *gorm.DB) error {
	if !LegacyReadGuardsEnabled(tx) {
		return nil
	}
	for _, ref := range m.KnowledgeReferences {
		if ref != nil {
			if err := CheckLegacySourceRead(tx, ref.KnowledgeID, ""); err != nil {
				return err
			}
		}
	}
	return nil
}
