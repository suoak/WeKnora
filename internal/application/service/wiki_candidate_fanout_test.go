package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type boundedWikiCandidates struct {
	gate    <-chan struct{}
	started chan string
	active  atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
	denied  string
}

func (w *boundedWikiCandidates) VerifiedPages(
	ctx context.Context, tenant uint64, kb string, _ []string,
) ([]*types.WikiPage, bool, error) {
	n := w.active.Add(1)
	defer w.active.Add(-1)
	for previous := w.peak.Load(); n > previous; previous = w.peak.Load() {
		if w.peak.CompareAndSwap(previous, n) {
			break
		}
	}
	w.calls.Add(1)
	w.started <- kb
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-w.gate:
	}
	if kb == w.denied {
		return nil, false, types.ErrLegacySourceRestricted
	}
	return []*types.WikiPage{{
		ID: kb + "-page", TenantID: tenant, KnowledgeBaseID: kb,
		Slug: "concept/match", Title: "synthetic match", Content: "synthetic candidate",
	}}, false, nil
}

func (*boundedWikiCandidates) RevalidatePages(context.Context, uint64, string, []string, []*types.WikiPage) error {
	return nil
}

func TestWikiCandidateFanoutBoundsWorkAndPreservesScopeOrderAndCancellation(t *testing.T) {
	testWikiCandidateFanout(t, fixture.New)
}

func TestWikiCandidateFanoutPostgres(t *testing.T) {
	if os.Getenv("WIKI_HISTORY_POSTGRES_DSN") == "" {
		t.Skip("requires isolated migrated PostgreSQL")
	}
	testWikiCandidateFanout(t, wikiPendingRegenerationFixture)
}

func testWikiCandidateFanout(t *testing.T, newFixture func(*testing.T) *fixture.Fixture) {
	t.Helper()
	for _, scenario := range []string{"complete", "partial", "cancel", "revoke"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.PreparePublished(t)
			ids := []string{}
			for i := 0; i < 4; i++ {
				kb := fmt.Sprintf("fanout-%d", i)
				ids = append(ids, kb)
				require.NoError(t, f.DB.Exec(`INSERT INTO knowledge_bases
(id,name,tenant_id,embedding_model_id,summary_model_id,type)
VALUES(?,?,1,'synthetic','synthetic','document')`, kb, kb).Error)
				require.NoError(t, f.DB.Exec(`INSERT INTO knowledges
(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel)
VALUES(?,1,?,'file','synthetic','synthetic-local','synthetic.txt','completed','enabled','web')`,
					kb+"-doc", kb).Error)
				require.NoError(t, f.DB.Exec(`INSERT INTO chunks
(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at)
VALUES(?,1,?,?,'synthetic','text',1,true,0,0,100)`, kb+"-chunk", kb, kb+"-doc").Error)
			}
			s := publishedServiceM7(t, f)
			gate := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			wiki := &boundedWikiCandidates{gate: gate, started: make(chan string, 4)}
			if scenario == "partial" {
				wiki.denied = ids[2]
			}
			s.Wiki = wiki
			ctx, cancel := context.WithCancel(fixture.Key("A", 1, 1, true))
			finished := make(chan struct{})
			var result *PortalWikiResult
			var resultErr error
			go func() {
				defer close(finished)
				result, resultErr = s.PersonalWiki(ctx, "wiki_search", "synthetic", "", "", 50, 0, ids...)
			}()
			t.Cleanup(func() {
				cancel()
				release()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("candidate workers did not stop")
				}
			})
			for i := 0; i < 2; i++ {
				select {
				case <-wiki.started:
				case <-finished:
					t.Fatalf("request ended before two candidates started: %v", resultErr)
				case <-time.After(5 * time.Second):
					t.Fatal("a serial candidate blocked the remaining authorized scope")
				}
			}
			if scenario == "cancel" {
				cancel()
			}
			if scenario == "revoke" {
				require.NoError(
					t, f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error,
				)
			}
			release()
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("candidate read did not finish")
			}
			require.EqualValues(t, 2, wiki.peak.Load(), "parallel work must retain its fixed two-reader bound")
			if scenario == "cancel" || scenario == "revoke" {
				require.Error(t, resultErr)
				require.Nil(t, result)
				return
			}
			require.NoError(t, resultErr)
			require.EqualValues(t, 4, wiki.calls.Load(), "every requested KB must be searched")
			expected := ids
			if scenario == "partial" {
				expected = []string{ids[0], ids[1], ids[3]}
				require.Equal(t, "partial", result.Status)
			} else {
				require.Equal(t, "complete", result.Status)
			}
			actual := []string{}
			for _, page := range result.Pages {
				actual = append(actual, page.KBID)
			}
			require.Equal(t, expected, actual, "completion order must not reorder or truncate the result scope")
		})
	}
}
