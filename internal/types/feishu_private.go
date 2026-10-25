package types

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrPrivateKnowledgeBase = errors.New("private_knowledge_base_forbidden")

type privateKBCreationKey struct{}

func WithPrivateKBCreation(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, privateKBCreationKey{}, owner)
}

func PrivateKBCreationOwner(ctx context.Context) string {
	owner, _ := ctx.Value(privateKBCreationKey{}).(string)
	return owner
}

const privateKBGuard = "knowhub.private_kb_guard"

type privateKBPlugin struct{}

func (privateKBPlugin) Name() string { return privateKBGuard }
func (privateKBPlugin) Initialize(db *gorm.DB) error {
	filter := func(tx *gorm.DB) {
		if tx.Statement == nil {
			return
		}
		table := tx.Statement.Table
		column := "knowledge_base_id"
		switch table {
		case "knowledge_bases":
			column = "id"
		case "knowledges", "chunks", "embeddings", "wiki_pages", "wiki_page_revisions", "wiki_folders", "wiki_page_issues":
		default:
			return
		}
		user, _ := UserIDFromContext(tx.Statement.Context)
		tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "NOT EXISTS (SELECT 1 FROM feishu_private_kbs fp WHERE fp.kb_id=" + table + "." + column + " AND fp.owner_id <> ?)", Vars: []any{user}}}})
	}
	if err := db.Callback().Query().Before("gorm:query").Register(privateKBGuard, filter); err != nil {
		return err
	}
	return db.Callback().Row().Before("gorm:row").Register(privateKBGuard, filter)
}

// Once private state exists these guards remain active even with V2 disabled.
func EnablePrivateKBGuards(db *gorm.DB) *gorm.DB {
	if _, ok := db.Config.Plugins[privateKBGuard]; !ok {
		if err := db.Use(privateKBPlugin{}); err != nil {
			panic("cannot install private KB guards")
		}
	}
	return db.Session(&gorm.Session{NewDB: true})
}
func PrivateKBGuardsEnabled(db *gorm.DB) bool { _, ok := db.Config.Plugins[privateKBGuard]; return ok }
func PrivateKBOwner(db *gorm.DB, kb string) (string, error) {
	if !PrivateKBGuardsEnabled(db) {
		return "", nil
	}
	var owner string
	err := db.Raw("SELECT owner_id FROM feishu_private_kbs WHERE kb_id=?", kb).Scan(&owner).Error
	return owner, err
}
func CheckPrivateKBRead(db *gorm.DB, knowledge, kb string) error {
	if _, ok := db.Config.Plugins[privateKBGuard]; !ok {
		return nil
	}
	if kb == "" && knowledge != "" {
		if err := db.Raw("SELECT knowledge_base_id FROM knowledges WHERE id=?", knowledge).Scan(&kb).Error; err != nil {
			return err
		}
	}
	if kb == "" {
		return nil
	}
	owner, err := PrivateKBOwner(db, kb)
	if err != nil {
		return err
	}
	if owner == "" {
		return nil
	}
	user, ok := UserIDFromContext(db.Statement.Context)
	if !ok || user != owner || IsSyntheticUserID(user) {
		return ErrPrivateKnowledgeBase
	}
	recordLegacyRead(db.Statement.Context, kb)
	return nil
}
