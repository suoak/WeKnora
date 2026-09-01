package spreadsheet

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const SchemasMetadataKey = "spreadsheet.schemas"

type Schema struct {
	SheetName     string   `json:"sheet_name"`
	Headers       []string `json:"headers"`
	CommonColumns []string `json:"common_columns"`
	EntityAxis    string   `json:"entity_axis"`
	EntityHeaders []string `json:"entity_headers"`
	EntityCount   int      `json:"entity_count"`
	Confidence    float64  `json:"confidence"`
	Records       []Record `json:"records,omitempty"`
}

type Record struct {
	Common   map[string]string `json:"common"`
	Entities map[string]string `json:"entities"`
}

type QueryResult struct {
	Content      string
	Entities     []string
	Count        int
	Completeness types.RetrievalCompleteness
	EntityAxis   string
}

func ParseSchemas(raw string) ([]Schema, error) {
	var schemas []Schema
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(raw), &schemas); err != nil {
		return nil, fmt.Errorf("parse spreadsheet schemas: %w", err)
	}
	return schemas, nil
}

func EncodeSchema(schema Schema) (string, error) {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// Execute runs a deterministic collection query against complete parser-owned
// schemas. It never derives an entity set from retrieved row chunks.
func Execute(queryType types.RetrievalQueryType, query, sourceFile string,
	schemas []Schema,
) QueryResult {
	result := QueryResult{Completeness: types.PartialRetrievalCompleteness()}
	trusted := make([]Schema, 0, len(schemas))
	for _, schema := range schemas {
		if (schema.EntityAxis == "columns" || schema.EntityAxis == "rows") &&
			schema.Confidence > 0 && len(schema.EntityHeaders) > 0 {
			trusted = append(trusted, schema)
		}
	}
	if len(trusted) == 0 {
		return result
	}
	if queryType == types.RetrievalQueryExhaustiveList || queryType == types.RetrievalQueryCount {
		trusted = selectRelevantSchemas(query, trusted)
	}

	entities := uniqueEntities(trusted)
	sheets := make([]string, 0, len(trusted))
	for _, schema := range trusted {
		sheets = append(sheets, schema.SheetName)
	}
	result.Entities = entities
	result.Count = len(entities)
	result.EntityAxis = commonAxis(trusted)
	result.Completeness = types.RetrievalCompleteness{
		Scope:               types.RetrievalScopeEntityIndex,
		IsExhaustive:        len(entities) > 0,
		ExpectedEntityCount: len(entities),
		ObservedEntityCount: len(entities),
		SourceFile:          sourceFile,
		SourceSheets:        sheets,
		EntityAxis:          result.EntityAxis,
	}

	switch queryType {
	case types.RetrievalQueryExhaustiveList:
		result.Content = formatEntityList(entities, sourceFile, sheets)
	case types.RetrievalQueryCount:
		result.Content = fmt.Sprintf("Spreadsheet entity count: %d\nSource file: %s\nSource sheets: %s",
			len(entities), sourceFile, strings.Join(sheets, ", "))
	case types.RetrievalQueryFilter:
		return executeFilter(query, sourceFile, trusted, entities)
	default:
		return QueryResult{Completeness: types.PartialRetrievalCompleteness()}
	}
	return result
}

// selectRelevantSchemas narrows a multi-sheet workbook only when the query
// structurally names a sheet or a common-field value. With no positive signal,
// all trusted sheets remain in scope.
func selectRelevantSchemas(query string, schemas []Schema) []Schema {
	normalized := strings.ToLower(query)
	best := 0
	scores := make([]int, len(schemas))
	for index, schema := range schemas {
		sheet := strings.ToLower(strings.TrimSpace(schema.SheetName))
		if len([]rune(sheet)) >= 2 && strings.Contains(normalized, sheet) {
			scores[index] = len([]rune(sheet))
		}
		for _, record := range schema.Records {
			if score := recordQueryScore(query, record.Common); score > scores[index] {
				scores[index] = score
			}
		}
		if scores[index] > best {
			best = scores[index]
		}
	}
	if best == 0 {
		return schemas
	}
	selected := make([]Schema, 0, len(schemas))
	for index, schema := range schemas {
		if scores[index] == best {
			selected = append(selected, schema)
		}
	}
	return selected
}

func executeFilter(query, sourceFile string, schemas []Schema, _ []string) QueryResult {
	result := QueryResult{
		Completeness: types.PartialRetrievalCompleteness(),
		EntityAxis:   commonAxis(schemas),
	}
	negated := containsFold(query, "不支持", "not supported", "unsupported")
	bestScore := 0
	var selected *Record
	var selectedEntities []string
	selectedSheet := ""
	for schemaIndex := range schemas {
		if schemas[schemaIndex].EntityAxis != "columns" {
			continue
		}
		for recordIndex := range schemas[schemaIndex].Records {
			record := &schemas[schemaIndex].Records[recordIndex]
			score := recordQueryScore(query, record.Common)
			if score > bestScore {
				bestScore = score
				selected = record
				selectedSheet = schemas[schemaIndex].SheetName
				selectedEntities = schemas[schemaIndex].EntityHeaders
			}
		}
	}
	if selected == nil || bestScore == 0 {
		return result
	}

	matched := make([]string, 0)
	observed := 0
	for _, entity := range selectedEntities {
		value, ok := selected.Entities[entity]
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		observed++
		supported, known := supportState(value)
		if known && supported != negated {
			matched = append(matched, entity)
		}
	}
	result.Entities = matched
	result.Count = len(matched)
	result.Completeness = types.RetrievalCompleteness{
		Scope:               types.RetrievalScopeFullSheet,
		IsExhaustive:        observed == len(selectedEntities) && len(selectedEntities) > 0,
		ExpectedEntityCount: len(selectedEntities),
		ObservedEntityCount: observed,
		SourceFile:          sourceFile,
		SourceSheets:        []string{selectedSheet},
		EntityAxis:          result.EntityAxis,
	}
	if result.Completeness.IsExhaustive {
		result.Content = formatEntityList(matched, sourceFile, []string{selectedSheet})
	}
	return result
}

func supportState(value string) (bool, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "not supported", "unsupported", "no", "false", "n", "不支持", "否":
		return false, true
	case "supported", "support", "yes", "true", "y", "支持", "是":
		return true, true
	default:
		return false, false
	}
}

func recordQueryScore(query string, common map[string]string) int {
	normalized := strings.ToLower(query)
	best := 0
	for _, value := range common {
		candidate := strings.ToLower(strings.TrimSpace(value))
		if len([]rune(candidate)) >= 2 && strings.Contains(normalized, candidate) {
			if length := len([]rune(candidate)); length > best {
				best = length
			}
		}
	}
	return best
}

func uniqueEntities(schemas []Schema) []string {
	seen := make(map[string]struct{})
	entities := make([]string, 0)
	for _, schema := range schemas {
		for _, entity := range schema.EntityHeaders {
			entity = strings.TrimSpace(entity)
			if entity == "" {
				continue
			}
			key := strings.ToLower(entity)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			entities = append(entities, entity)
		}
	}
	return entities
}

func commonAxis(schemas []Schema) string {
	axis := schemas[0].EntityAxis
	for _, schema := range schemas[1:] {
		if schema.EntityAxis != axis {
			return "mixed"
		}
	}
	return axis
}

func formatEntityList(entities []string, sourceFile string, sheets []string) string {
	var builder strings.Builder
	builder.WriteString("Spreadsheet entity collection (complete parser index):\n")
	for _, entity := range entities {
		builder.WriteString("- " + entity + "\n")
	}
	builder.WriteString("Source file: " + sourceFile + "\n")
	builder.WriteString("Source sheets: " + strings.Join(sheets, ", "))
	return builder.String()
}

func containsFold(value string, candidates ...string) bool {
	value = strings.ToLower(value)
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func SortSchemas(schemas []Schema) {
	sort.SliceStable(schemas, func(i, j int) bool { return schemas[i].SheetName < schemas[j].SheetName })
}
