package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type retryProofVersionKey struct{}

type retryReadKB struct {
	interfaces.KnowledgeBaseService
	calls int
	get   func(int) (uint64, error)
}

func (s *retryReadKB) GetKnowledgeBaseByID(ctx context.Context, _ string) (*types.KnowledgeBase, error) {
	s.calls++
	if ctx.Value(retryProofVersionKey{}) != nil {
		return nil, fmt.Errorf("stale proof survived KB lookup")
	}
	tenant, err := s.get(s.calls)
	return &types.KnowledgeBase{
		ID: "reviewed", TenantID: tenant, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
	}, err
}

type retryReadWiki struct {
	interfaces.WikiPageService
	proofCalls, readCalls int
	proof                 func(int) error
	read                  func(context.Context, int) error
}

func (s *retryReadWiki) VerifiedWikiBrowserReadContext(ctx context.Context, _ string) (context.Context, error) {
	s.proofCalls++
	if ctx.Value(retryProofVersionKey{}) != nil {
		return ctx, fmt.Errorf("stale proof survived recapture")
	}
	if err := s.proof(s.proofCalls); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, retryProofVersionKey{}, s.proofCalls), nil
}

func (s *retryReadWiki) ListPages(ctx context.Context, _ *types.WikiPageListRequest) (
	*types.WikiPageListResponse, error,
) {
	s.readCalls++
	// Returning a non-nil partial response with the error catches accidental
	// serialization of stale content when a proof or a later attempt fails.
	title := "discarded-old-result"
	if s.readCalls == 2 {
		title = "fresh-verified-result"
	}
	return &types.WikiPageListResponse{Pages: []*types.WikiPage{{Title: title}}}, s.read(ctx, s.readCalls)
}

func TestWikiBrowserReadRecapturesProofAndDiscardsFailedResults(t *testing.T) {
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "reviewed")
	changed := repository.ErrWikiMCPInputsChanged
	for _, name := range []string{
		"changed during query", "changed during capture", "persistent conflict", "revoked KB",
		"revoked proof", "changed workspace", "unverified history", "canceled", "canceled after success",
		"persistent capture conflict", "API key", "shared KB",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(fixture.Web("A", 1))
			defer cancel()
			status, proofCalls, readCalls := http.StatusOK, 2, 2
			kb := &retryReadKB{get: func(int) (uint64, error) { return 1, nil }}
			wiki := &retryReadWiki{
				proof: func(int) error { return nil },
				read: func(ctx context.Context, n int) error {
					require.Equal(t, n, ctx.Value(retryProofVersionKey{}), "each read needs its own proof")
					if n == 1 {
						return fmt.Errorf("query: %w", changed)
					}
					return nil
				},
			}
			switch name {
			case "changed during capture":
				readCalls = 1
				wiki.proof = func(n int) error {
					if n == 1 {
						return changed
					}
					return nil
				}
				wiki.read = func(ctx context.Context, _ int) error {
					require.Equal(t, 2, ctx.Value(retryProofVersionKey{}))
					return nil
				}
			case "persistent conflict":
				status = http.StatusConflict
				wiki.read = func(context.Context, int) error { return changed }
			case "persistent capture conflict":
				status, readCalls = http.StatusConflict, 0
				wiki.proof = func(int) error { return changed }
			case "revoked KB":
				status, proofCalls, readCalls = http.StatusNotFound, 1, 1
				kb.get = func(n int) (uint64, error) {
					if n == 2 {
						return 1, types.ErrLegacySourceRestricted
					}
					return 1, nil
				}
			case "revoked proof":
				status, readCalls = http.StatusInternalServerError, 1
				wiki.proof = func(n int) error {
					if n == 2 {
						return types.ErrLegacySourceRestricted
					}
					return nil
				}
			case "changed workspace":
				status, proofCalls, readCalls = http.StatusInternalServerError, 1, 1
				kb.get = func(n int) (uint64, error) { return uint64(n), nil }
			case "unverified history":
				status, proofCalls, readCalls = http.StatusBadRequest, 1, 0
				wiki.proof = func(int) error { return repository.ErrWikiMCPUnverified }
			case "canceled":
				status, proofCalls, readCalls = http.StatusInternalServerError, 1, 1
				wiki.read = func(context.Context, int) error { cancel(); return changed }
			case "canceled after success":
				status, proofCalls, readCalls = http.StatusInternalServerError, 1, 1
				wiki.read = func(context.Context, int) error { cancel(); return nil }
			case "API key", "shared KB":
				status, proofCalls, readCalls = http.StatusConflict, 0, 1
				wiki.read = func(context.Context, int) error { return changed }
				if name == "API key" {
					ctx, cancel = context.WithCancel(fixture.Key("A", 1, 7, true))
					defer cancel()
				} else {
					kb.get = func(int) (uint64, error) { return 99, nil }
				}
			}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodGet, "/wiki/pages", nil).WithContext(ctx)
			c.Params = gin.Params{{Key: "kb_id", Value: "reviewed"}}
			c.Set(types.TenantIDContextKey.String(), uint64(1))
			h := &WikiPageHandler{kbService: kb, wikiService: wiki}
			h.ListPages(c)
			require.Equal(t, status, response.Code, response.Body.String())
			require.Equal(t, proofCalls, wiki.proofCalls)
			require.Equal(t, readCalls, wiki.readCalls)
			if status != http.StatusOK {
				require.NotContains(t, response.Body.String(), "discarded-old-result")
				require.NotContains(t, response.Body.String(), "fresh-verified-result")
			} else if name == "changed during query" {
				require.Contains(t, response.Body.String(), "fresh-verified-result")
				require.NotContains(t, response.Body.String(), "discarded-old-result")
			}
			if status == http.StatusConflict {
				require.Equal(t, "1", response.Header().Get("Retry-After"))
				require.Contains(t, response.Body.String(), `"code":"wiki_snapshot_changed"`)
			}
		})
	}
}

func TestWikiBrowserSnapshotNeverRetriesMutation(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := context.WithValue(context.Background(), wikiBrowserSnapshotKey{}, wikiBrowserSnapshot{
		kb: "reviewed", base: context.Background(),
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/wiki/pages", nil).WithContext(ctx)
	c.Params = gin.Params{{Key: "kb_id", Value: "reviewed"}}
	calls := 0
	result, err := readWikiBrowserSnapshot(c, &WikiPageHandler{}, func(context.Context) (string, error) {
		calls++
		return "partial", repository.ErrWikiMCPInputsChanged
	})
	require.ErrorIs(t, err, repository.ErrWikiMCPInputsChanged)
	require.Empty(t, result)
	require.Equal(t, 1, calls)
}
