//go:build sqlite_fts5

package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fullScopeLoadReply struct {
	HTTP, Planned, Completed int
	Status                   string
	Error                    bool
	Detail                   string
	Milliseconds             float64
}

// This opt-in exercise uses PrivateDSN's disposable database and normal viewer
// keys. It never runs against a shared schema or creates a production account.
func exerciseFullScopePostgresLoad(
	t *testing.T, handler http.Handler, db *gorm.DB,
	create func(string, uint64, string) *interfaces.TenantAPIKeyCreateResult,
) {
	t.Helper()
	for i := range 65 {
		kb := fmt.Sprintf("load-full-%03d", i)
		require.NoError(t, db.Exec(`INSERT INTO knowledge_bases
 (id,name,tenant_id,embedding_model_id,summary_model_id,type)
 SELECT ?,?,tenant_id,embedding_model_id,summary_model_id,type
 FROM knowledge_bases WHERE id='public-kb'`, kb, kb).Error)
		require.NoError(t, db.Create(&types.Knowledge{
			ID: kb + "-doc", TenantID: 99, KnowledgeBaseID: kb, Type: "manual", Title: "PUBLIC " + kb,
			Channel: "web", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled",
		}).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO chunks
 (id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,is_enabled,chunk_index,start_at,end_at)
 SELECT kb.id||'-chunk-'||n,99,kb.id,kb.id||'-doc','PUBLIC synthetic bounded load','text',true,n,0,29
 FROM knowledge_bases kb CROSS JOIN generate_series(1,128)n WHERE kb.id LIKE 'load-full-%'`).Error)
	require.NoError(t, db.Exec(`INSERT INTO embeddings
 (source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,is_enabled,embedding)
 SELECT c.id,0,c.id,c.knowledge_id,c.knowledge_base_id,c.content,1024,true,e.embedding
 FROM chunks c CROSS JOIN (SELECT embedding FROM embeddings WHERE source_id='public-doc-chunk') e
 WHERE c.knowledge_base_id LIKE 'load-full-%'`).Error)
	var keys []string
	for i := range 4 {
		id := fmt.Sprintf("load-viewer-%02d", i)
		require.NoError(t, db.Create(&types.User{
			ID: id, Username: id, Email: id + "@synthetic.invalid", PasswordHash: "synthetic-no-login",
			TenantID: 1, IsActive: true,
		}).Error)
		require.NoError(t, db.Exec(`INSERT INTO tenant_members(user_id,tenant_id,role,status)
 VALUES(?,1,'viewer','active')`, id).Error)
		keys = append(keys, create(id, 1, "dynamic_all").Token)
	}
	root := os.Getenv("KNOWHUB_MCP_FULLSCOPE_EVIDENCE_DIR")
	if root != "" {
		require.NoError(t, os.MkdirAll(root, 0o700))
		var database string
		require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
		ready, err := json.Marshal(map[string]any{"database": database, "seeded_kbs": 69, "seeded_chunks": 8320})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, "load-ready.json"), ready, 0o600))
		deadline := time.Now().Add(90 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(root, "load-continue")); err == nil {
				break
			}
			require.True(t, time.Now().Before(deadline), "bounded cold-buffer restart handshake timed out")
			time.Sleep(200 * time.Millisecond)
		}
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 35 * time.Second
	call := func(key string) fullScopeLoadReply {
		start := time.Now()
		request := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
			`"params":{"name":"search_knowledge","arguments":{"query":"PUBLIC","mode":"hybrid","limit":10}}}`)
		req, err := http.NewRequest("POST", server.URL+"/api/v1/mcp/personal", bytes.NewReader(request))
		if err != nil {
			return fullScopeLoadReply{Error: true}
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		response, err := client.Do(req)
		if err != nil {
			return fullScopeLoadReply{Error: true, Milliseconds: float64(time.Since(start).Microseconds()) / 1000}
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		reply := fullScopeLoadReply{HTTP: response.StatusCode, Error: err != nil}
		var envelope struct {
			Error  any `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
				Data    struct {
					Status    string `json:"status"`
					Planned   int    `json:"planned_kbs"`
					Completed int    `json:"completed_kbs"`
				} `json:"structuredContent"`
			} `json:"result"`
		}
		if response.StatusCode == 200 {
			err = json.Unmarshal(body, &envelope)
			reply.Error = reply.Error || err != nil || envelope.Error != nil || envelope.Result.IsError
			if reply.Error {
				reply.Detail = string(body)
			}
			reply.Status = envelope.Result.Data.Status
			reply.Planned, reply.Completed = envelope.Result.Data.Planned, envelope.Result.Data.Completed
		}
		reply.Milliseconds = float64(time.Since(start).Microseconds()) / 1000
		return reply
	}
	pool, err := db.DB()
	require.NoError(t, err)
	if value := os.Getenv("KNOWHUB_MCP_FULLSCOPE_POOL_CAP"); value != "" {
		cap, err := strconv.Atoi(value)
		require.NoError(t, err)
		require.True(t, cap > 0 && cap <= 64)
		pool.SetMaxOpenConns(cap)
	}
	require.NoError(t, pool.Ping())
	var phases []map[string]any
	failed := false
	for _, phase := range []struct {
		name  string
		count int
	}{{"cold_shared_buffers_os_cache_uncontrolled", 1}, {"warm", 1}, {"two_concurrent", 2}, {"four_concurrent", 4}, {"four_viewers_140_request_burst", 140}} {
		replies := make([]fullScopeLoadReply, phase.count)
		before := pool.Stats()
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range replies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				replies[i] = call(keys[i%len(keys)])
			}()
		}
		close(start)
		wg.Wait()
		after := pool.Stats()
		statuses := make(map[int]int)
		var accepted []float64
		var failures []fullScopeLoadReply
		for _, reply := range replies {
			statuses[reply.HTTP]++
			if reply.HTTP == 200 {
				accepted = append(accepted, reply.Milliseconds)
				if reply.Error || reply.Status != "complete" || reply.Planned != 69 || reply.Completed != 69 {
					failures = append(failures, reply)
				}
			} else if reply.HTTP != 429 {
				failures = append(failures, reply)
			}
		}
		sort.Float64s(accepted)
		percentile := func(p int) float64 {
			if len(accepted) == 0 {
				return 0
			}
			return accepted[min((len(accepted)-1)*p/100, len(accepted)-1)]
		}
		phases = append(phases, map[string]any{
			"phase": phase.name, "requests": phase.count, "http": statuses, "accepted": len(accepted),
			"accepted_p50_ms": percentile(50), "accepted_p95_ms": percentile(95), "accepted_p99_ms": percentile(99),
			"pool_wait_count":           after.WaitCount - before.WaitCount,
			"pool_wait_ms":              float64((after.WaitDuration - before.WaitDuration).Microseconds()) / 1000,
			"pool_max_open_connections": after.MaxOpenConnections, "unexpected_results": failures,
		})
		encoded, err := json.Marshal(map[string]any{"scope": "disposable synthetic PostgreSQL", "phases": phases})
		require.NoError(t, err)
		if root != "" {
			require.NoError(t, os.WriteFile(filepath.Join(root, "load-result.json"), encoded, 0o600))
		}
		failed = failed || len(accepted) == 0 || len(failures) != 0
	}
	encoded, err := json.Marshal(map[string]any{"scope": "disposable synthetic PostgreSQL", "phases": phases})
	require.NoError(t, err)
	if root != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "load-result.json"), encoded, 0o600))
	}
	t.Logf("FULLSCOPE_LOAD_REPORT %s", encoded)
	require.False(t, failed, "partial results, 500/504 and transport errors are not successful load outcomes")
}
