package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var resourceTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

type PersonalResource struct {
	Type     string `json:"type"`
	Token    string `json:"token"`
	Title    string `json:"title"`
	Modified string `json:"modified"`
	URL      string `json:"url"`
}

var ErrUnsupportedPersonalResource = errors.New("feishu_unsupported_resource_export_manually")

// Parsing follows PR #3312's URL types, with strict host/path validation.
func ParsePersonalResourceURL(raw string) (PersonalResource, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return PersonalResource{}, errors.New("invalid_feishu_url")
	}
	host := strings.ToLower(u.Hostname())
	if host != "feishu.cn" && !strings.HasSuffix(host, ".feishu.cn") {
		return PersonalResource{}, errors.New("invalid_feishu_host")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !resourceTokenPattern.MatchString(parts[1]) {
		return PersonalResource{}, ErrUnsupportedPersonalResource
	}
	kind := map[string]string{"docx": "docx", "docs": "doc", "doc": "doc", "sheets": "sheet", "base": "bitable", "wiki": "wiki", "file": "file", "slides": "slides", "mindnotes": "mindnote", "mindnote": "mindnote"}[parts[0]]
	if kind == "" {
		return PersonalResource{}, ErrUnsupportedPersonalResource
	}
	u.RawQuery, u.Fragment = "", ""
	return PersonalResource{Type: kind, Token: parts[1], URL: u.String()}, nil
}

// ResolvePersonalResource unwraps a Wiki link and validates metadata under the
// user identity. Identity is {real type}:{real token}, independent of URL form.
func (c *Client) ResolvePersonalResource(ctx context.Context, r PersonalResource) (PersonalResource, error) {
	if _, app := ctx.Value(applicationIdentityKey{}).(applicationIdentity); app {
		return r, ErrAuth
	}
	if _, ok := ctx.Value(userIdentityKey{}).(userIdentity); !ok {
		return r, ErrAuth
	}
	return c.resolveResource(ctx, r)
}

func (c *Client) resolveResource(ctx context.Context, r PersonalResource) (PersonalResource, error) {
	if r.Type == "wiki" {
		node, err := c.GetWikiNode(ctx, "", r.Token)
		if err != nil {
			return r, err
		}
		r.Type, r.Token, r.Title, r.Modified = node.ObjType, node.ObjToken, node.Title, node.ObjEditTime
	}
	if !resourceTokenPattern.MatchString(r.Token) {
		return r, errors.New("invalid_resource_token")
	}
	switch r.Type {
	case "doc", "docx", "sheet", "bitable", "file", "slides":
	default:
		return r, ErrUnsupportedPersonalResource
	}
	// Minimal reuse of PR #3312's metadata contract. No folder or drive listing.
	var response struct {
		Data struct {
			Metas []struct {
				Token    string `json:"doc_token"`
				Type     string `json:"doc_type"`
				Title    string `json:"title"`
				Modified string `json:"latest_modify_time"`
			} `json:"metas"`
			Failed []struct {
				Token string `json:"token"`
				Code  int    `json:"code"`
			} `json:"failed_list"`
		} `json:"data"`
	}
	body := map[string]any{"request_docs": []map[string]string{{"doc_token": r.Token, "doc_type": r.Type}}, "with_url": true}
	if err := c.doRequest(ctx, "resolve_personal_resource", http.MethodPost, "/open-apis/drive/v1/metas/batch_query", body, &response); err != nil {
		return r, err
	}
	if len(response.Data.Failed) > 0 || len(response.Data.Metas) != 1 || response.Data.Metas[0].Token != r.Token || response.Data.Metas[0].Type != r.Type {
		return r, ErrPermission
	}
	r.Title, r.Modified = response.Data.Metas[0].Title, response.Data.Metas[0].Modified
	return r, nil
}

func (c *Client) DownloadPersonalResource(ctx context.Context, r PersonalResource) ([]byte, string, error) {
	if _, app := ctx.Value(applicationIdentityKey{}).(applicationIdentity); app {
		return nil, "", ErrAuth
	}
	if _, ok := ctx.Value(userIdentityKey{}).(userIdentity); !ok {
		return nil, "", ErrAuth
	}
	return c.downloadResource(ctx, r)
}

func (c *Client) downloadResource(ctx context.Context, r PersonalResource) ([]byte, string, error) {
	if r.Type == "file" {
		data, err := c.DownloadDriveFile(ctx, r.Token)
		if err == nil {
			err = validatePersonalBytes(data, r.Title)
		}
		return data, SanitizeFileName(r.Title), err
	}
	var data []byte
	var err error
	ext := ""
	if r.Type == "slides" {
		data, _, err = c.exportAndDownload(ctx, r.Token, "slides", "pptx")
		ext = ".pptx"
	} else {
		data, _, err = c.ExportAndDownload(ctx, r.Token, r.Type)
		ext = ExportFileExtToSuffix[ObjTypeToExportFileExtension[r.Type]]
	}
	if err != nil {
		return nil, "", err
	}
	name := SanitizeFileName(r.Title) + ext
	if err := validatePersonalBytes(data, name); err != nil {
		return nil, "", err
	}
	return data, name, nil
}

// Reject HTML login pages, empty bodies, and invalid OOXML before ingestion.
// A valid download means fetched, not that all embedded objects were parsed.
func validatePersonalBytes(data []byte, name string) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return errors.New("feishu_empty_download")
	}
	lower := strings.ToLower(string(trimmed[:min(len(trimmed), 512)]))
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return errors.New("feishu_html_download_rejected")
	}
	var required string
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".docx"):
		required = "word/document.xml"
	case strings.HasSuffix(strings.ToLower(name), ".xlsx"):
		required = "xl/workbook.xml"
	case strings.HasSuffix(strings.ToLower(name), ".pptx"):
		required = "ppt/presentation.xml"
	}
	if required != "" {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return errors.New("feishu_invalid_office_download")
		}
		for _, f := range z.File {
			if f.Name == required {
				return nil
			}
		}
		return errors.New("feishu_invalid_office_download")
	}
	if strings.HasSuffix(strings.ToLower(name), ".pdf") && !bytes.HasPrefix(trimmed, []byte("%PDF-")) {
		return errors.New("feishu_invalid_pdf_download")
	}
	return nil
}

// ValidateWorkbookCoverage verifies all visible tables/sheets are represented
// in the exported workbook. Field/record semantics and attachment contents
// still require the documented live fixture checks; they are not inferred.
func (c *Client) ValidateWorkbookCoverage(ctx context.Context, r PersonalResource, data []byte) (int, error) {
	if _, app := ctx.Value(applicationIdentityKey{}).(applicationIdentity); app {
		return 0, ErrAuth
	}
	if _, ok := ctx.Value(userIdentityKey{}).(userIdentity); !ok {
		return 0, ErrAuth
	}
	return c.validateWorkbookCoverage(ctx, r, data)
}

func (c *Client) validateWorkbookCoverage(ctx context.Context, r PersonalResource, data []byte) (int, error) {
	if r.Type != "sheet" && r.Type != "bitable" {
		return 0, nil
	}
	names := []string{}
	if r.Type == "sheet" {
		var resp struct {
			Data struct {
				Sheets []struct {
					Title string `json:"title"`
				} `json:"sheets"`
			} `json:"data"`
		}
		if err := c.doRequest(ctx, "verify_personal_sheets", http.MethodGet, "/open-apis/sheets/v3/spreadsheets/"+r.Token+"/sheets/query", nil, &resp); err != nil {
			return 0, err
		}
		for _, sheet := range resp.Data.Sheets {
			names = append(names, sheet.Title)
		}
	} else {
		page := ""
		seen := map[string]bool{}
		for {
			var resp struct {
				Data struct {
					Items []struct {
						Name string `json:"name"`
					} `json:"items"`
					HasMore   bool   `json:"has_more"`
					PageToken string `json:"page_token"`
				} `json:"data"`
			}
			path := "/open-apis/bitable/v1/apps/" + r.Token + "/tables?page_size=100"
			if page != "" {
				path += "&page_token=" + url.QueryEscape(page)
			}
			if err := c.doRequest(ctx, "verify_personal_bitable_tables", http.MethodGet, path, nil, &resp); err != nil {
				return 0, err
			}
			for _, table := range resp.Data.Items {
				names = append(names, table.Name)
			}
			if !resp.Data.HasMore {
				break
			}
			if resp.Data.PageToken == "" || seen[resp.Data.PageToken] || len(names) > 10000 {
				return 0, errors.New("feishu_incomplete_table_listing")
			}
			page = resp.Data.PageToken
			seen[page] = true
		}
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, err
	}
	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
		} `xml:"sheets>sheet"`
	}
	for _, f := range z.File {
		if f.Name == "xl/workbook.xml" {
			if f.UncompressedSize64 > 4*1024*1024 {
				return 0, errors.New("feishu_workbook_manifest_too_large")
			}
			reader, err := f.Open()
			if err != nil {
				return 0, err
			}
			err = xml.NewDecoder(io.LimitReader(reader, 4*1024*1024)).Decode(&workbook)
			reader.Close()
			if err != nil {
				return 0, err
			}
			break
		}
	}
	if len(names) == 0 || len(workbook.Sheets) != len(names) {
		return 0, errors.New("feishu_export_missing_tables")
	}
	// Office exports may sanitize/truncate titles. Counts are a hard check;
	// names are left to the live semantic validation rather than guessing.
	return len(names), nil
}

func PersonalResourceID(r PersonalResource) string { return fmt.Sprintf("%s:%s", r.Type, r.Token) }
