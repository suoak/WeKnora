package service

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func standaloneThreadFacts(parts map[string][]byte, sheets []standaloneWorkbookSheet) (map[string]string, error) {
	notes := map[string]string{}
	people := map[string]string{}
	for _, part := range standaloneSortedKeys(parts) {
		if !strings.HasPrefix(part, "xl/persons/") {
			continue
		}
		facts, err := standaloneXMLFacts(parts[part], map[string]string{
			standaloneThreadNS: "personList person",
		}, false)
		if err != nil {
			return nil, err
		}
		var list struct {
			XMLName xml.Name `xml:"http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments personList"`
			People  []struct {
				ID   string `xml:"id,attr"`
				Name string `xml:"displayName,attr"`
			} `xml:"person"`
		}
		if xml.Unmarshal(parts[part], &list) != nil || len(list.People) > 1024 {
			return nil, errExactWorkbookUnsupported
		}
		for _, person := range list.People {
			id, err := uuid.Parse(person.ID)
			if err != nil || id == uuid.Nil || people[id.String()] != "" || person.Name == "" {
				return nil, errExactWorkbookUnsupported
			}
			people[id.String()] = person.Name
		}
		notes[sheets[0].Part] += facts
	}
	type comment struct {
		ID     string `xml:"id,attr"`
		Parent string `xml:"parentId,attr"`
		Person string `xml:"personId,attr"`
		Ref    string `xml:"ref,attr"`
		Date   string `xml:"dT,attr"`
		Text   string `xml:"text"`
	}
	used := map[string]bool{}
	commentIDs := map[string]bool{}
	for _, sheet := range sheets {
		rels, err := standaloneWorkbookRelations(parts, sheet.Part)
		if err != nil {
			return nil, err
		}
		for _, rel := range rels {
			if rel.Type != standaloneThreadRelation {
				continue
			}
			part := standaloneRelationshipTarget(sheet.Part, rel)
			if used[part] {
				return nil, errExactWorkbookUnsupported
			}
			used[part] = true
			facts, err := standaloneXMLFacts(parts[part], map[string]string{
				standaloneThreadNS: "ThreadedComments threadedComment text",
			}, false)
			if err != nil {
				return nil, err
			}
			var list struct {
				XMLName xml.Name
				Rows    []comment `xml:"threadedComment"`
			}
			if xml.Unmarshal(parts[part], &list) != nil ||
				list.XMLName != (xml.Name{Space: standaloneThreadNS, Local: "ThreadedComments"}) ||
				len(list.Rows) == 0 || len(list.Rows) > 1024 {
				return nil, errExactWorkbookUnsupported
			}
			byID := map[string]comment{}
			for _, row := range list.Rows {
				id, err := uuid.Parse(row.ID)
				person, personErr := uuid.Parse(row.Person)
				col, line, cellErr := excelize.CellNameToCoordinates(row.Ref)
				canonical, canonicalErr := excelize.CoordinatesToCellName(col, line)
				if err != nil || id == uuid.Nil || personErr != nil || people[person.String()] == "" ||
					cellErr != nil || canonicalErr != nil || canonical != row.Ref || col > 64 || line > 1024 ||
					commentIDs[id.String()] {
					return nil, errExactWorkbookUnsupported
				}
				byID[id.String()] = row
				commentIDs[id.String()] = true
				fmtText := "\nThreaded comment cell %s: author=%s; date=%s; text=%s\n"
				notes[sheet.Part] += fmt.Sprintf(fmtText, row.Ref, standaloneFactText(people[person.String()]),
					standaloneFactText(row.Date), standaloneFactText(row.Text))
			}
			for id, row := range byID {
				visited := map[string]bool{id: true}
				for row.Parent != "" && row.Parent != "{}" {
					parent, err := uuid.Parse(row.Parent)
					if err != nil {
						return nil, errExactWorkbookUnsupported
					}
					if parent == uuid.Nil {
						break // Missing, empty-brace and all-zero parents are explicit root sentinels.
					}
					next, exists := byID[parent.String()]
					if !exists || visited[parent.String()] || next.Ref != row.Ref {
						return nil, errExactWorkbookUnsupported
					}
					visited[parent.String()] = true
					row = next
				}
			}
			notes[sheet.Part] += facts
		}
	}
	for name := range parts {
		if strings.HasPrefix(name, "xl/threadedcomments/") && !used[name] {
			return nil, errExactWorkbookUnsupported
		}
	}
	return notes, nil
}
