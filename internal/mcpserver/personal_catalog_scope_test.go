package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type catalogScopeDirectory struct{ calls atomic.Int64 }

func (d *catalogScopeDirectory) ListPortalMCPCandidates(context.Context) ([]access.PortalKBDescriptor, error) {
	d.calls.Add(1)
	return []access.PortalKBDescriptor{{ID: "selected", Name: "catalog-selected", TenantID: 99}}, nil
}

type catalogScopeStore struct {
	*selectedMCPStore
	loads             atomic.Int64
	before            func(int64)
	permissionRevoked atomic.Bool
}

func (s *catalogScopeStore) CheckDocumentReadBase(
	ctx context.Context, tenant uint64, kb string, op types.DocumentOperation, public bool,
) error {
	if s.permissionRevoked.Load() {
		return access.ErrForbidden
	}
	return (selectedMCPBase{}).CheckDocumentReadBase(ctx, tenant, kb, op, public)
}

func (s *catalogScopeStore) LoadDocumentReadSnapshot(
	ctx context.Context, user string, key uint64, kb string,
) (*types.EnterpriseReadSnapshot, error) {
	n := s.loads.Add(1)
	if s.before != nil {
		s.before(n)
	}
	return s.selectedMCPStore.LoadDocumentReadSnapshot(ctx, user, key, kb)
}

func catalogScopeFixture() (*PersonalServer, *catalogScopeStore, *catalogScopeDirectory, context.Context) {
	s, base, ctx := selectedMCPFixture()
	store := &catalogScopeStore{selectedMCPStore: base}
	directory := &catalogScopeDirectory{}
	s.knowledge.Resolver.Store = store
	s.knowledge.Resolver.Directory = directory
	s.knowledge.Resolver.Documents = access.NewDocumentReader(true, store, store, selectedMCPEmployee{})
	return s, store, directory, ctx
}

func catalogScopeRequest(handler http.Handler, ctx context.Context, path string) *httptest.ResponseRecorder {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
		`"params":{"name":"list_knowledge_bases","arguments":{}}}`
	r := httptest.NewRequest("POST", path, bytes.NewBufferString(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestPersonalCatalogUsesOneResolutionWithLiveFences(t *testing.T) {
	for _, entry := range []string{"internal", "external", "isolated"} {
		t.Run(entry, func(t *testing.T) {
			s, store, directory, ctx := catalogScopeFixture()
			if entry == "external" {
				s = ConfigurePersonalContentServer(s, &service.PersonalContent{})
			}
			sink := &captureUsage{}
			s.SetUsageRecorder(sink)
			handler := s.Handler()
			path := personalPath
			if entry == "isolated" {
				handler = s.IsolatedHandler()
				path = isolatedPersonalPath
			}
			w := catalogScopeRequest(handler, ctx, path)
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), "catalog-selected")
			require.EqualValues(t, 1, directory.calls.Load(),
				"one HTTP invocation must not enumerate the same scope three times")
			require.EqualValues(t, 4, store.loads.Load(), "admission plus live pre-execution and final fences must remain")
			require.Len(t, sink.reports, 1)
			require.True(t, sink.reports[0].Success)
		})
	}
}

func TestPersonalCatalogDiscardsRevokedOrChangedOutput(t *testing.T) {
	for _, phase := range []int64{3, 4} {
		for _, mutation := range []string{"revoke", "permission", "version", "hold"} {
			t.Run(fmt.Sprintf("%s/%d", mutation, phase), func(t *testing.T) {
				s, store, directory, ctx := catalogScopeFixture()
				store.before = func(n int64) {
					if n != phase {
						return
					}
					switch mutation {
					case "revoke":
						store.revoked = true
					case "permission":
						store.permissionRevoked.Store(true)
					case "version":
						store.version = "v2"
					case "hold":
						store.held = true
					}
				}
				w := catalogScopeRequest(s.Handler(), ctx, personalPath)
				if mutation == "version" && phase == 3 {
					require.Contains(t, w.Body.String(), "catalog-selected", "fresh versions remain readable")
					require.EqualValues(t, 2, directory.calls.Load(), "changed admission facts require fresh resolution")
					return
				}
				require.NotContains(t, w.Body.String(), "catalog-selected")
			})
		}
	}
}

func TestPersonalCatalogConcurrentScopesDoNotMixKeys(t *testing.T) {
	s, _, _, ctx := catalogScopeFixture()
	s.admission = newPersonalAdmission(config.PersonalMCPConcurrencyConfig{GlobalLimit: 24, UserLimit: 24}, false)
	var wg sync.WaitGroup
	var failures atomic.Int64
	for i := uint64(1); i <= 24; i++ {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			allowed := id%2 == 0
			kb := "other"
			if allowed {
				kb = "selected"
			}
			requestCtx := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
				KeyID: id, ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve"},
				KnowledgeBaseRestricted: true, KnowledgeBaseIDs: types.StringArray{kb},
			})
			w := catalogScopeRequest(s.Handler(), requestCtx, personalPath)
			if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("catalog-selected")) != allowed {
				failures.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.Zero(t, failures.Load(), "catalog metadata may only use the current request's Key scope")
}

func BenchmarkPersonalCatalogHTTPResolution(b *testing.B) {
	s, store, directory, ctx := catalogScopeFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if w := catalogScopeRequest(s.Handler(), ctx, personalPath); w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(directory.calls.Load())/float64(b.N), "directories/op")
	b.ReportMetric(float64(store.loads.Load())/float64(b.N), "snapshots/op")
}
