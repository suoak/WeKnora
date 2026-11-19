package repository

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const (
	wikiSourceInputBytes = 2 << 20
	wikiSourceInputRows  = 2048
)

type wikiSourceInputs struct {
	db         *gorm.DB
	ids        []string
	positions  map[string]int
	identities map[string]map[string]interface{}
	chunks     map[string][]map[string]interface{}
}

const wikiIdentityColumns = "id,tenant_id,knowledge_base_id,type,title,description,source,channel,enable_status," +
	"file_name,file_type,file_hash,file_path,embedding_model_id"

const wikiInputColumns = "id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_index,chunk_type,context_header," +
	"is_enabled,parent_chunk_id,image_info,deleted_at"

func newWikiSourceInputs(db *gorm.DB, ids []string) *wikiSourceInputs {
	positions := make(map[string]int, len(ids))
	for i, id := range ids {
		positions[id] = i
	}
	return &wikiSourceInputs{db: db, ids: ids, positions: positions}
}

// The scalar aggregate and rows are read in the same SQL statement snapshot.
// Oversized groups use the complete original per-document path. This cache
// lives only inside one proof, covered by its existing before/after epochs.
func (v *wikiSourceInputs) boundedRows(
	table, columns, predicate string,
	ids []string,
) ([]map[string]interface{}, error) {
	terms := []string{}
	for _, column := range strings.Split(columns, ",") {
		text := "COALESCE(CAST(" + column + " AS TEXT),'')"
		if v.db.Dialector.Name() == "postgres" {
			text = "octet_length(" + text + ")"
		} else {
			text = "length(CAST(" + text + " AS BLOB))"
		}
		terms = append(terms, text)
	}
	guard := fmt.Sprintf("(SELECT count(*)<=%d AND COALESCE(sum(%s),0)<=%d FROM %s",
		wikiSourceInputRows, strings.Join(terms, "+"), wikiSourceInputBytes, table) +
		" WHERE " + predicate + ")"
	var rows []map[string]interface{}
	err := v.db.Table(table).Select(columns).Where(predicate, ids).Where(guard, ids).
		Order("id").Limit(wikiSourceInputRows + 1).Find(&rows).Error
	return rows, err
}

func (v *wikiSourceInputs) get(
	id string,
	tenant uint64,
	kb string,
) (map[string]interface{}, []map[string]interface{}, error) {
	if err := v.db.Statement.Context.Err(); err != nil {
		return nil, nil, err
	}
	if identity := v.identities[id]; identity != nil {
		return identity, v.chunks[id], nil
	}
	position, ok := v.positions[id]
	if !ok {
		return nil, nil, ErrWikiMCPUnverified
	}
	start := position / 8 * 8
	ids := v.ids[start:min(start+8, len(v.ids))]
	identities, err := v.boundedRows("knowledges", wikiIdentityColumns, "id IN ? AND deleted_at IS NULL", ids)
	if err != nil {
		return nil, nil, err
	}
	chunks, err := v.boundedRows(
		"chunks",
		wikiInputColumns,
		"knowledge_id IN ? AND chunk_type IN ('','text','parent_text','image_ocr','image_caption')",
		ids,
	)
	if err != nil {
		return nil, nil, err
	}
	// Empty or oversized batches cannot prove completeness. Use original reads.
	if len(identities) != len(ids) || len(chunks) == 0 || len(chunks) > wikiSourceInputRows {
		return v.single(id, tenant, kb)
	}
	v.identities = make(map[string]map[string]interface{}, len(ids))
	v.chunks = make(map[string][]map[string]interface{}, len(ids))
	for _, row := range identities {
		owner, ok := row["id"].(string)
		if !ok {
			return nil, nil, ErrWikiMCPUnverified
		}
		if fmt.Sprint(row["tenant_id"]) != fmt.Sprint(tenant) || row["knowledge_base_id"] != kb {
			return nil, nil, ErrWikiMCPInputsChanged
		}
		v.identities[owner] = row
	}
	for _, row := range chunks {
		owner, ok := row["knowledge_id"].(string)
		if !ok {
			return nil, nil, ErrWikiMCPUnverified
		}
		v.chunks[owner] = append(v.chunks[owner], row)
	}
	if v.identities[id] == nil {
		return nil, nil, ErrWikiMCPUnverified
	}
	return v.identities[id], v.chunks[id], nil
}

func (v *wikiSourceInputs) single(
	id string,
	tenant uint64,
	kb string,
) (map[string]interface{}, []map[string]interface{}, error) {
	var identity map[string]interface{}
	if err := v.db.Table("knowledges").Select(wikiIdentityColumns).
		Where("id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", id, tenant, kb).
		Take(&identity).Error; err != nil {
		return nil, nil, err
	}
	var inputs []map[string]interface{}
	err := v.db.Table("chunks").Select(wikiInputColumns).
		Where("knowledge_id=? AND chunk_type IN ('','text','parent_text','image_ocr','image_caption')", id).
		Order("id").Find(&inputs).Error
	return identity, inputs, err
}
