package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestP4SheetReadsBeyond500WithFormulaAndHeaderContext(t *testing.T) {
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sheets/query") {
			_, _ = fmt.Fprint(
				w,
				"{\"code\":0,\"data\":{\"sheets\":[{\"sheet_id\":\"s1\","+
					"\"title\":\"Sales\",\"grid_properties\":{\"row_count\":601,"+
					"\"column_count\":2},\"merges\":[]}]}}",
			)
			return
		}
		requests++
		rg := strings.Split(r.URL.Path, "/values/")[1]
		var start, end int
		if _, err := fmt.Sscanf(rg, "s1!A%d:B%d", &start, &end); err != nil {
			t.Error(err)
		}
		rows := [][]any{}
		for n := start; n <= end; n++ {
			v := any(n * 2)
			if r.URL.Query().Get("valueRenderOption") == "Formula" {
				v = "=A" + strconv.Itoa(n) + "*2"
			}
			rows = append(rows, []any{"row" + strconv.Itoa(n), v})
		}
		_ = json.NewEncoder(w).
			Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"revision": 42,
					"valueRange": map[string]any{
						"range":  rg,
						"values": rows,
					},
				},
			})
	}))
	defer s.Close()
	c := NewClient(&Config{AppID: "p4sheet601", BaseURL: s.URL})
	ctx := WithApplicationIdentity(
		context.Background(),
		func(context.Context) (string, error) { return "app", nil },
	)
	got, err := c.CollectApplicationTableSnapshot(
		ctx,
		PersonalResource{Type: "sheet", Token: "sheet", Title: "Revenue"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 8 || len(got.Tables) != 1 || len(got.Tables[0].Cells) != 601 {
		t.Fatalf("truncated range: requests=%d rows=%d", requests, len(got.Tables[0].Cells))
	}
	text, err := got.IndexText()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"row601", "=A601*2", "First rows / header context:", "Sales", "Revenue"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestP4SheetRejectsChangedRevisionAndMedia(t *testing.T) {
	for _, mode := range []string{"revision", "media", "range"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/sheets/query") {
					_, _ = fmt.Fprint(
						w,
						"{\"code\":0,\"data\":{\"sheets\":[{\"sheet_id\":\"s\",\"title\":\"T\","+
							"\"grid_properties\":{\"row_count\":1,\"column_count\":1}}]}}",
					)
					return
				}
				requests++
				revision := 1
				if mode == "revision" && requests > 1 {
					revision = 2
				}
				rg := "s!A1:A1"
				if mode == "range" {
					rg = "wrong"
				}
				v := any("text")
				if mode == "media" {
					v = map[string]any{"type": "image", "fileToken": "secret-token"}
				}
				_ = json.NewEncoder(w).
					Encode(map[string]any{
						"code": 0,
						"data": map[string]any{
							"revision": revision,
							"valueRange": map[string]any{
								"range": rg,
								"values": [][]any{
									{
										v,
									},
								},
							},
						},
					})
			}))
			defer s.Close()
			c := NewClient(&Config{AppID: "p4sheetreject" + mode, BaseURL: s.URL})
			ctx := WithApplicationIdentity(
				context.Background(),
				func(context.Context) (string, error) { return "app", nil },
			)
			if _, err := c.CollectApplicationTableSnapshot(
				ctx,
				PersonalResource{
					Type:  "sheet",
					Token: "sheet",
				},
			); err == nil {
				t.Fatal("incomplete sheet accepted")
			}
		})
	}
}

// A default view must never limit the formal snapshot. These are protocol
// contracts, not evidence that the live application has the corresponding scopes.
func TestP4BitableAllPagesAndRecordContext(t *testing.T) {
	pages := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer application" {
			t.Error("wrong principal")
		}
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/tables"):
			data = map[string]any{
				"items":    []any{map[string]any{"table_id": "t1", "name": "Sales"}},
				"has_more": false,
			}
		case strings.HasSuffix(r.URL.Path, "/fields"):
			data = map[string]any{
				"items":    []any{map[string]any{"field_id": "f1", "field_name": "Customer", "type": 1}},
				"has_more": false,
			}
		case strings.HasSuffix(r.URL.Path, "/records"):
			if r.URL.Query().Get("view_id") != "" || r.Method != "GET" {
				t.Error("view filtering is forbidden")
			}
			pages++
			items := []any{}
			start := 0
			if pages == 2 {
				start = 500
			}
			for i := start; i < min(start+500, 601); i++ {
				items = append(
					items,
					map[string]any{
						"record_id": fmt.Sprintf("r%d", i),
						"fields":    map[string]any{"Customer": fmt.Sprintf("customer%d", i)},
					},
				)
			}
			data = map[string]any{
				"items":      items,
				"has_more":   pages == 1,
				"page_token": "next",
				"total":      601,
			}
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer s.Close()
	c := NewClient(&Config{AppID: "p4pages", BaseURL: s.URL})
	ctx := WithApplicationIdentity(
		context.Background(),
		func(context.Context) (string, error) { return "application", nil },
	)
	got, err := c.CollectApplicationTableSnapshot(
		ctx,
		PersonalResource{Type: "bitable", Token: "app", Title: "CRM"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tables) != 1 || len(got.Tables[0].Records) != 601 || pages != 2 {
		t.Fatalf("lost records: pages=%d snapshot=%+v", pages, got)
	}
	text, err := got.IndexText()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CRM", "Sales", "Customer", "r600", "customer600"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing context %s", want)
		}
	}
}

func TestP4BitableBrokenPaginationNeverSucceeds(t *testing.T) {
	for _, mode := range []string{
		"repeated",
		"missing",
		"duplicate_record",
		"wrong_total",
		"unknown_field",
		"attachment",
	} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data any
				switch {
				case strings.HasSuffix(r.URL.Path, "/tables"):
					data = map[string]any{"items": []any{map[string]any{"table_id": "t", "name": "T"}}}
				case strings.HasSuffix(r.URL.Path, "/fields"):
					typ := 1
					if mode == "attachment" {
						typ = 17
					}
					data = map[string]any{
						"items": []any{map[string]any{"field_id": "f", "field_name": "F", "type": typ}},
					}
				default:
					field := "F"
					if mode == "unknown_field" {
						field = "missing"
					}
					data = map[string]any{
						"items": []any{
							map[string]any{"record_id": "r", "fields": map[string]any{field: "value"}},
						},
						"total": 1,
					}
					d := data.(map[string]any)
					if mode == "repeated" || mode == "missing" || mode == "duplicate_record" {
						d["has_more"] = true
						if mode != "missing" {
							d["page_token"] = "same"
						}
					}
					if mode == "wrong_total" {
						d["total"] = 2
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer s.Close()
			c := NewClient(&Config{AppID: "p4broken" + mode, BaseURL: s.URL})
			ctx := WithApplicationIdentity(
				context.Background(),
				func(context.Context) (string, error) { return "app", nil },
			)
			if _,
				err := c.CollectApplicationTableSnapshot(
				ctx,

				PersonalResource{
					Type: "bitable",

					Token: "app",
				},
			); err == nil {
				t.Fatal("incomplete snapshot accepted")
			}
		})
	}
}

func TestP4SnapshotRequiresExclusiveApplicationIdentity(t *testing.T) {
	c := NewClient(&Config{AppID: "p4identity"})
	for _, ctx := range []context.Context{
		context.Background(),

		WithUserIdentity(
			context.Background(),

			func(
				context.Context,
			) (
				string,

				error,
			) {
				return "user",
					nil
			},
		),

		WithUserIdentity(
			WithApplicationIdentity(
				context.Background(),

				nil,
			),

			nil,
		),
	} {
		if _,
			err := c.CollectApplicationTableSnapshot(
			ctx,

			PersonalResource{
				Type: "sheet",

				Token: "s",
			},
		); err != ErrAuth {
			t.Fatalf("identity bypass: %v", err)
		}
	}
}
