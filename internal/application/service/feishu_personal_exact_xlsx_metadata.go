package service

import "encoding/xml"

// The cell's cm is a one-based reference into cellMetadata. Its rc.t selects
// the metadata type and rc.v selects the matching futureMetadata record. Keep
// this mapping exact; an unknown or dangling entry cannot be discarded.
func standaloneDynamicMetadata(body []byte) (int, string, error) {
	if len(body) == 0 {
		return 0, "", nil
	}
	facts, err := standaloneXMLFacts(body, map[string]string{
		standaloneMainNS: "metadata metadataTypes metadataType futureMetadata bk extLst ext cellMetadata rc",
		"http://schemas.microsoft.com/office/spreadsheetml/2017/dynamicarray": "dynamicArrayProperties",
	}, false)
	if err != nil {
		return 0, "", err
	}
	var document struct {
		XMLName xml.Name `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main metadata"`
		Types   struct {
			Count int `xml:"count,attr"`
			Rows  []struct {
				Name string `xml:"name,attr"`
			} `xml:"metadataType"`
		} `xml:"metadataTypes"`
		Future []struct {
			Name  string `xml:"name,attr"`
			Count int    `xml:"count,attr"`
			Rows  []struct {
				Extensions []struct {
					Dynamic *struct {
						Dynamic   string `xml:"fDynamic,attr"`
						Collapsed string `xml:"fCollapsed,attr"`
					} `xml:"http://schemas.microsoft.com/office/spreadsheetml/2017/dynamicarray dynamicArrayProperties"`
				} `xml:"extLst>ext"`
			} `xml:"bk"`
		} `xml:"futureMetadata"`
		Cells struct {
			Count int `xml:"count,attr"`
			Rows  []struct {
				References []struct {
					Type  int `xml:"t,attr"`
					Value int `xml:"v,attr"`
				} `xml:"rc"`
			} `xml:"bk"`
		} `xml:"cellMetadata"`
	}
	if xml.Unmarshal(body, &document) != nil || document.Types.Count != 1 || len(document.Types.Rows) != 1 ||
		document.Types.Rows[0].Name != "XLDAPR" || len(document.Future) != 1 ||
		document.Future[0].Name != "XLDAPR" || document.Future[0].Count != len(document.Future[0].Rows) ||
		len(document.Future[0].Rows) == 0 || len(document.Future[0].Rows) > 1024 ||
		document.Cells.Count != len(document.Cells.Rows) || len(document.Cells.Rows) > 4096 {
		return 0, "", errExactWorkbookUnsupported
	}
	for _, record := range document.Future[0].Rows {
		if len(record.Extensions) != 1 || record.Extensions[0].Dynamic == nil {
			return 0, "", errExactWorkbookUnsupported
		}
		properties := record.Extensions[0].Dynamic
		if !standaloneXMLBoolean(properties.Dynamic) || !standaloneXMLBoolean(properties.Collapsed) {
			return 0, "", errExactWorkbookUnsupported
		}
	}
	for _, record := range document.Cells.Rows {
		if len(record.References) != 1 || record.References[0].Type != 1 ||
			record.References[0].Value < 0 || record.References[0].Value >= len(document.Future[0].Rows) {
			return 0, "", errExactWorkbookUnsupported
		}
	}
	return len(document.Cells.Rows), facts, nil
}

func standaloneXMLBoolean(value string) bool {
	return value == "" || value == "0" || value == "1" || value == "true" || value == "false"
}
