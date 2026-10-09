package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGitLabInvalidNextPageFails(t *testing.T) {
	for _, next := range []string{"0", "-1", "abc", "02", "+2", "999999999999999999999999", "backwards"} {
		for _, scope := range []string{"projects", "tree"} {
			t.Run(scope+"/"+next, func(t *testing.T) {
				allowLocalGitLabServer(t)
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					header := next
					if next == "backwards" {
						header = "3"
						if r.URL.Query().Get("page") == "3" {
							header = "2"
						}
					}
					w.Header().Set("X-Next-Page", header)
					_, _ = w.Write([]byte(`[]`))
				}))
				defer server.Close()
				c, err := newClient(server.URL, "test-token")
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(context.Background(), guardTestTimeout)
				defer cancel()
				if scope == "projects" {
					items, err := c.projects(ctx)
					require.Error(t, err)
					require.Nil(t, items)
				} else {
					items, err := c.tree(ctx, "1", "main", "")
					require.Error(t, err)
					require.Nil(t, items)
				}
				require.LessOrEqual(t, requests, 2, "invalid headers must fail before another request")
			})
		}
	}
}

// This store models the StreamHandler boundary, not a source-wide transaction.
// Earlier Emit writes persist across a later pagination failure; Checkpoint
// only becomes durable after the complete project succeeds.
type paginationRecoveryStore struct {
	documents                     map[string]types.FetchedItem
	checkpoint                    *types.SyncCursor
	emits, deletions, checkpoints int
}

func (s *paginationRecoveryStore) Emit(_ context.Context, item types.FetchedItem) error {
	s.emits++
	if item.IsDeleted {
		s.deletions++
		delete(s.documents, item.ExternalID)
	} else {
		s.documents[item.ExternalID] = item
	}
	return nil
}

func (s *paginationRecoveryStore) Checkpoint(_ context.Context, value *types.SyncCursor) error {
	s.checkpoints++
	// Snapshot as persistence would, avoiding aliasing the connector's map.
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.checkpoint)
}

func TestGitLabStreamPaginationFailureAndRetry(t *testing.T) {
	allowLocalGitLabServer(t)
	broken := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"docs","default_branch":"main"}`))
		case "/api/v4/projects/1/repository/commits/main":
			_, _ = w.Write([]byte(`{"id":"head"}`))
		case "/api/v4/projects/1/repository/tree":
			if r.URL.Query().Get("path") == "a-good" {
				_, _ = w.Write([]byte(`[{"type":"blob","name":"a.md","path":"a-good/a.md"}]`))
			} else {
				if broken {
					w.Header().Set("X-Next-Page", "2")
				}
				_, _ = w.Write([]byte(`[{"type":"blob","name":"b.md","path":"z-broken/b.md"}]`))
			}
		default:
			if strings.Contains(r.URL.EscapedPath(), "/repository/files/") {
				_, _ = w.Write([]byte("# complete file"))
			} else {
				http.NotFound(w, r)
			}
		}
	}))
	defer server.Close()
	config := &types.DataSourceConfig{
		Credentials: map[string]interface{}{"base_url": server.URL, "access_token": "test-token"},
		Settings: map[string]interface{}{"projects": []interface{}{
			map[string]interface{}{"project_id": "1", "paths": []interface{}{"a-good", "z-broken"}},
		}},
	}
	normalized, err := parseConfig(config)
	require.NoError(t, err)
	require.Len(t, normalized.Projects, 1)
	require.Equal(t, []string{"a-good", "z-broken"}, normalized.Projects[0].Paths,
		"normalized roots must visit the successful directory before the failing directory")
	old := gitLabCursor(cursor{Projects: map[string]string{"9": "retained"}})
	store := &paginationRecoveryStore{documents: map[string]types.FetchedItem{"existing": {ExternalID: "existing"}}, checkpoint: old}
	connector := NewConnector()
	ctx, cancel := context.WithTimeout(context.Background(), guardTestTimeout)
	defer cancel()
	next, err := connector.FetchStream(ctx, config, old, store)
	require.ErrorContains(t, err, "pagination repeated page")
	require.Nil(t, next)
	require.Zero(t, store.checkpoints)
	require.Equal(t, 1, store.emits, "earlier successful root is not rolled back")
	require.Len(t, store.documents, 2)
	require.Same(t, old, store.checkpoint)
	require.Zero(t, store.deletions)

	broken = false
	next, err = connector.FetchStream(ctx, config, store.checkpoint, store)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Equal(t, 1, store.checkpoints)
	require.Len(t, store.documents, 3, "upsert replay must not duplicate documents or remove existing entries")
	require.Zero(t, store.deletions)
	require.Equal(t, map[string]string{"9": "retained", "1": "head"}, next.ConnectorCursor["projects"])
	emits := store.emits
	_, err = connector.FetchStream(ctx, config, store.checkpoint, store)
	require.NoError(t, err)
	require.Equal(t, emits, store.emits, "unchanged commit must not emit again")
}
