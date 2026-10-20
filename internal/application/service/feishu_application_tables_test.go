package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestP4ApplicationSheetUsesOriginalFirstCreateProtocol(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	t.Setenv("FEISHU_APPLICATION_TABLES_ENABLED", "1")
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	for name, content := range map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sales" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData/></worksheet>`,
	} {
		entry, err := z.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal") {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"sheet-app-token","expire":7200}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sheet-app-token" {
			t.Error("wrong principal")
			w.WriteHeader(403)
			return
		}
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/batch_query"):
			data = map[string]any{"metas": []any{map[string]any{"doc_type": "sheet", "doc_token": "sheet1", "title": "Revenue", "latest_modify_time": "1"}}}
		case strings.HasSuffix(r.URL.Path, "/sheets/query"):
			data = map[string]any{"sheets": []any{map[string]any{"sheet_id": "s1", "title": "Sales", "grid_properties": map[string]any{"row_count": 2, "column_count": 2}}}}
		case strings.Contains(r.URL.Path, "/values/"):
			data = map[string]any{"revision": 1, "valueRange": map[string]any{"range": "s1!A1:B2", "values": [][]any{{"Customer", "Revenue"}, {"UniqueCustomer", 123}}}}
		case strings.HasSuffix(r.URL.Path, "/export_tasks"):
			data = map[string]any{"ticket": "ticket"}
		case strings.HasSuffix(r.URL.Path, "/export_tasks/ticket"):
			data = map[string]any{"result": map[string]any{"job_status": 0, "file_token": "download", "file_name": "sheet.xlsx"}}
		case strings.HasSuffix(r.URL.Path, "/file/download/download"):
			w.Write(buffer.Bytes())
			return
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer s.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = s.URL; return core.NewClient(cfg) }
	m := f.prepare("https://test.feishu.cn/sheets/sheet1")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, _, _ := mvpPublicTask(t, f, m)
	require.Equal(t, m.LogicalKnowledgeID, k.ID)
	require.Equal(t, "md", k.FileType)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	var n int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestP4ApplicationBitableUsesOriginalFirstCreateProtocol(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	t.Setenv("FEISHU_APPLICATION_TABLES_ENABLED", "1")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal") {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"table-app-token","expire":7200}`)
			return
		}
		require.Equal(t, "Bearer table-app-token", r.Header.Get("Authorization"))
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/batch_query"):
			data = map[string]any{"metas": []any{map[string]any{"doc_type": "bitable", "doc_token": "base1", "title": "CRM", "latest_modify_time": "1"}}}
		case strings.HasSuffix(r.URL.Path, "/tables"):
			data = map[string]any{"items": []any{map[string]any{"table_id": "t1", "name": "Sales"}}}
		case strings.HasSuffix(r.URL.Path, "/fields"):
			data = map[string]any{"items": []any{map[string]any{"field_id": "f1", "field_name": "Customer", "type": 1}}}
		case strings.HasSuffix(r.URL.Path, "/records"):
			data = map[string]any{"items": []any{map[string]any{"record_id": "r1", "fields": map[string]any{"Customer": "Unique Customer"}}}, "total": 1}
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer s.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = s.URL; return core.NewClient(cfg) }
	m := f.prepare("https://test.feishu.cn/base/base1")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, op, _ := mvpPublicTask(t, f, m)
	require.Equal(t, m.LogicalKnowledgeID, k.ID)
	require.Equal(t, "md", k.FileType)
	overrides, err := k.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, overrides)
	kb, err := f.ks.kbService.GetKnowledgeBaseByID(f.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, "builtin", ResolveProcessConfig(kb, overrides).ChunkingConfig.ResolveParserEngine("md"))
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
	require.NotEmpty(t, metadata["feishu_verified_text_sha256"])
	require.ErrorIs(t, verifyApplicationParse(k, &types.ReadResult{MarkdownContent: "lost record"}), ErrFeishuIncompleteContent)
	var envelope types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &envelope))
	require.Equal(t, m.AttemptID, envelope.AttemptID)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	again := f.prepare("https://test.feishu.cn/base/base1")
	require.Equal(t, m.LogicalKnowledgeID, again.LogicalKnowledgeID)
	require.Equal(t, m.AttemptID, again.AttemptID)
	var n int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestP4ApplicationTableParserReceiptRejectsChangedContent(t *testing.T) {
	data := []byte("<!-- knowhub-feishu-table-snapshot-v1 -->\n# CRM\nRecord: r1 Customer: A")
	hash, err := applicationVerifiedTextHash(context.Background(), data, "CRM.md")
	require.NoError(t, err)
	meta, _ := json.Marshal(map[string]string{"feishu_auth_mode": types.FeishuApplicationGrant, "feishu_verified_text_sha256": hash})
	k := &types.Knowledge{Metadata: types.JSON(meta)}
	require.NoError(t, verifyApplicationParse(k, &types.ReadResult{MarkdownContent: string(data)}))
	require.ErrorIs(t, verifyApplicationParse(k, &types.ReadResult{MarkdownContent: string(data) + " changed"}), ErrFeishuIncompleteContent)
}
