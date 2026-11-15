package service

import (
	"errors"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
)

// Read this preference from the same configuration bound to the current SQL
// execution. The normal source edit API owns its authorization and change fence.
// Never mutate a published Knowledge or use the destructive reparse path.
func applyFeishuDocumentParserOverride(
	ds *types.DataSource, item *types.FetchedItem, engine string, overrides map[string]string,
) error {
	if ds == nil || item == nil {
		return errors.New("document parser preference requires a source item")
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return err
	}
	preferences, err := config.FeishuDocumentParserOverrides()
	if err != nil {
		return err
	}
	preference, exists := preferences[item.ExternalID]
	if !exists {
		return nil
	}
	if !legacyWikiDrive(ds.Type) || engine != "builtin" || item.IsDeleted || item.Metadata["attachment"] == "true" {
		return errors.New("document parser preference is unavailable for this source item")
	}
	if overrides == nil {
		return errors.New("document parser preference requires parser overrides")
	}
	switch getFileType(item.FileName) {
	case "xls", "xlsx", "xlsm":
		overrides["xlsx_context_column_count"] = strconv.Itoa(preference.XLSXContextColumnCount)
		return nil
	default:
		return errors.New("document parser preference requires an Excel source")
	}
}
