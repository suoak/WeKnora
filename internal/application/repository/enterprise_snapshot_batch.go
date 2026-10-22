package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Same version wire representation as resourceVersion, fetched in a bounded
// number of SQL statements rather than per-document queries. No content read.
// Missing epochs/trigger coverage deny; never approximate with updated_at.
func populateDocumentVersions(tx *gorm.DB, docs []types.EnterpriseDocumentState) error {
	if err := checkDocumentEpochCoverage(tx); err != nil {
		return err
	}
	if len(docs) == 0 {
		return nil
	}
	if len(docs) > 2048 {
		return errors.New("document snapshot resource budget exceeded: HOLD")
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	type policyRow struct {
		KnowledgeID string
		types.EnterprisePolicy
	}
	var policies []policyRow
	if err := tx.Table("knowledge_access_policies").Where("knowledge_id IN ?", ids).Find(&policies).Error; err != nil {
		return err
	}
	pm := map[string]types.EnterprisePolicy{}
	for _, p := range policies {
		pm[p.KnowledgeID] = p.EnterprisePolicy
	}
	type versionRow struct {
		TenantID  uint64
		KBID      string
		UpdatedAt time.Time
	}
	type documentRow struct {
		ID        string
		TenantID  uint64
		KBID      string
		UpdatedAt time.Time
	}
	var rows []documentRow
	if err := tx.Table("knowledges").Select("id,tenant_id,knowledge_base_id AS kb_id,updated_at").Where("id IN ? AND deleted_at IS NULL", ids).Find(&rows).Error; err != nil {
		return err
	}
	rm := map[string]versionRow{}
	for _, r := range rows {
		rm[r.ID] = versionRow{r.TenantID, r.KBID, r.UpdatedAt}
	}
	type epochRow struct {
		KnowledgeID string
		Generation  int64
		Incarnation string
	}
	var epochs []epochRow
	if err := tx.Table("document_access_epochs").Where("knowledge_id IN ?", ids).Find(&epochs).Error; err != nil {
		return err
	}
	em := map[string]epochRow{}
	for _, e := range epochs {
		em[e.KnowledgeID] = e
	}
	var chunks []map[string]interface{}
	if err := tx.Table("chunks").Select("knowledge_id,id,content_revision,updated_at,is_enabled,deleted_at").Where("knowledge_id IN ?", ids).Order("knowledge_id,id").Find(&chunks).Error; err != nil {
		return err
	}
	cm := map[string][]map[string]interface{}{}
	for _, c := range chunks {
		id, ok := c["knowledge_id"].(string)
		if !ok {
			return errors.New("invalid canonical chunk owner")
		}
		delete(c, "knowledge_id")
		cm[id] = append(cm[id], c)
	}
	for i := range docs {
		d := &docs[i]
		r, ok := rm[d.ID]
		e := em[d.ID]
		if !ok || r.TenantID == 0 || r.UpdatedAt.IsZero() || e.Generation <= 0 || len(e.Incarnation) != 32 {
			return errors.New("document version unavailable: HOLD")
		}
		// Exported field names/order deliberately match resourceVersion.
		row := struct {
			TenantID  uint64
			KBID      string
			UpdatedAt time.Time
		}{r.TenantID, r.KBID, r.UpdatedAt}
		epoch := struct {
			Generation  int64
			Incarnation string
		}{e.Generation, e.Incarnation}
		encoded, err := json.Marshal(struct {
			Target   types.EnterprisePolicyTarget
			Row      interface{}
			Children interface{}
			Epoch    interface{}
		}{types.EnterprisePolicyTarget{Kind: "knowledge", ID: d.ID}, row, cm[d.ID], epoch})
		if err != nil {
			return err
		}
		sum := sha256.Sum256(encoded)
		d.CurrentVersion = hex.EncodeToString(sum[:])
		d.EnterprisePolicy = pm[d.ID]
	}
	return nil
}
