package router

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/go-viper/mapstructure/v2"
	"github.com/panjf2000/ants/v2"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type controlledRankDBLogger struct{ gormlog.Interface }

func (l controlledRankDBLogger) Trace(
	ctx context.Context, started time.Time, statement func() (string, int64), err error,
) {
	if err != nil {
		state := "unknown"
		if coded, ok := err.(interface{ SQLState() string }); ok {
			state = coded.SQLState()
		}
		sql, _ := statement()
		fmt.Printf("CONTROLLED_RANK_AB_DB_ERROR: type=%T state=%s statement_sha256=%x\n",
			err, state, sha256.Sum256([]byte(sql)))
	}
	l.Interface.Trace(ctx, started, statement, err)
}

// This opt-in operator harness starts no listener, worker or scheduler. Its
// credentials arrive only on stdin when the compiled test is run deliberately.
// Normal CI skips it. Both variants use the real MCP route, authentication,
// admission, source/KB/key fences and unchanged 30-second request budget.
func TestControlledPersonalRankAB(t *testing.T) {
	if os.Getenv("CONTROLLED_PERSONAL_RANK_AB") != "1" {
		t.Skip("requires explicit operator invocation and protected stdin credentials")
	}
	var input struct {
		DSN, Key, YAML, RuntimeSHA string
		Comparison                 string
		Env                        map[string]string
		ExpectedKBs                []string
		ResultLimit                int
		Cases                      []struct {
			ID, Question, Tool string
			Arguments          map[string]any
		}
	}
	if json.NewDecoder(io.LimitReader(os.Stdin, 4<<20)).Decode(&input) != nil {
		t.Fatal("invalid controlled input")
	}
	if input.Comparison == "" {
		input.Comparison = "ranking"
	}
	if input.Comparison != "ranking" && input.Comparison != "technical_api_routes" {
		t.Fatal("unsupported controlled comparison")
	}
	for _, item := range input.Cases {
		if input.Comparison == "technical_api_routes" && (item.Tool != "read_document" || len(item.Arguments) == 0) {
			t.Fatal("technical projection comparison permits only normal explicit document reads")
		}
		if input.Comparison == "ranking" && (item.Tool != "" || len(item.Arguments) != 0) {
			t.Fatal("ranking retains its frozen hybrid-only protocol")
		}
	}
	if input.ResultLimit == 0 {
		input.ResultLimit = 10
	}
	if input.ResultLimit != 10 && input.ResultLimit != 30 {
		t.Fatal("only the original limit or explicitly diagnostic limit is supported")
	}
	dsn, err := url.Parse(input.DSN)
	if err != nil {
		t.Fatal("invalid controlled database connection")
	}
	verifiedTLS := dsn.Query().Get("sslmode") == "verify-full" && dsn.Query().Get("sslrootcert") != ""
	// The production application uses its existing private Docker network for
	// PostgreSQL, while the external audit gateway uses verified TLS and a
	// restricted audit role. The operator must attest the exact application
	// network before enabling this private-only transport. It changes no HBA
	// rule, database privilege, public listener, or production TLS setting.
	sameAppNetwork := os.Getenv("CONTROLLED_PRIVATE_APPLICATION_NETWORK") == "1" &&
		dsn.Hostname() == "postgres" && dsn.Port() == "5432" && dsn.Query().Get("sslmode") == "disable"
	if !verifiedTLS && !sameAppNetwork || dsn.Query().Get("default_transaction_read_only") != "on" ||
		len(input.Cases) > 6 || len(input.ExpectedKBs) != 71 {
		t.Fatal("read-only verified transport and frozen complete-scope preconditions required")
	}
	for name, value := range input.Env {
		if name == "SYSTEM_AES_KEY" || name == "RETRIEVE_DRIVER" || name == "WEKNORA_MODEL_MAX_CONCURRENCY" ||
			name == "WEKNORA_VERIFY_SSL" || strings.HasPrefix(name, "PERSONAL_MCP_") ||
			strings.HasPrefix(name, "FEISHU_") || strings.HasPrefix(name, "KNOWHUB_") ||
			strings.HasPrefix(name, "SOURCEID_") || strings.HasPrefix(name, "MODEL_") {
			t.Setenv(name, value)
		}
	}
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("personal_mcp.compatible_global_ranks", false)
	v.SetDefault("personal_mcp.technical_api_routes", false)
	yaml := os.Expand(input.YAML, func(name string) string {
		if value, ok := input.Env[name]; ok {
			return value
		}
		return "${" + name + "}"
	})
	var cfg config.Config
	if v.ReadConfig(strings.NewReader(yaml)) != nil ||
		v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) { dc.TagName = "yaml" }) != nil ||
		cfg.PersonalMCP == nil || !cfg.PersonalMCP.Enabled || cfg.PersonalMCP.PolicyReference == "" {
		t.Fatal("actual approved deployment MCP configuration required")
	}
	db, err := gorm.Open(postgres.Open(input.DSN), &gorm.Config{
		Logger: controlledRankDBLogger{gormlog.Default.LogMode(gormlog.Silent)},
	})
	if err != nil {
		t.Fatal("read-only database connection unavailable")
	}
	conn, err := db.DB()
	if err != nil {
		t.Fatal("read-only database pool unavailable")
	}
	t.Cleanup(func() {
		if conn.Close() != nil {
			t.Error("read-only database pool did not close cleanly")
		}
	})
	conn.SetMaxOpenConns(10)
	var readOnly string
	if db.Raw("SHOW default_transaction_read_only").Scan(&readOnly).Error != nil || readOnly != "on" {
		t.Fatal("database connections must default to read only")
	}
	var concreteStores int64
	if db.Table("knowledge_bases").Where("id IN ? AND vector_store_id IS NOT NULL AND vector_store_id<>''",
		input.ExpectedKBs).Count(&concreteStores).Error != nil || concreteStores != 0 {
		t.Fatal("this harness cannot substitute an environment engine for a concrete store binding")
	}
	db = types.EnablePrivateKBGuards(types.EnableLegacyReadGuards(db))
	tenants := service.NewTenantService(repository.NewTenantRepository(db), repository.NewStorageBackendRepository(db))
	userRepo := repository.NewUserRepository(db)
	members := service.NewTenantMemberService(repository.NewTenantMemberRepository(db), nil, userRepo, nil)
	users := service.NewUserService(&cfg, userRepo, nil, tenants, members, nil)
	keyRepo := repository.NewTenantAPIKeyRepository(db)
	keys := service.NewTenantAPIKeyService(keyRepo)
	kbs := repository.NewKnowledgeBaseRepository(db)
	pool, err := ants.NewPool(1)
	if err != nil {
		t.Fatal("embedding worker initialization failed")
	}
	defer pool.Release()
	models := service.NewModelService(repository.NewModelRepository(db), kbs, nil, nil,
		embedding.NewBatchEmbedder(pool), tenants)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	if registry.Register(retriever.NewKVHybridRetrieveEngine(
		pgretrieve.NewPostgresRetrieveEngineRepository(db), types.PostgresRetrieverEngineType)) != nil {
		t.Fatal("normal PostgreSQL engine initialization failed")
	}
	for _, candidate := range []bool{false, true} {
		if input.Comparison == "ranking" {
			cfg.PersonalMCP.CompatibleGlobalRanks = candidate
		} else {
			cfg.PersonalMCP.TechnicalAPIRoutes = candidate
		}
		portal := service.NewPortalPersonalKnowledge(&cfg, db, members, keyRepo, kbs,
			repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db),
			repository.NewChunkRepository(db), models, registry, nil, nil, nil)
		if portal == nil {
			t.Fatal("normal personal knowledge constructor rejected the configuration")
		}
		embed := portal.Embed
		portal.Embed = func(ctx context.Context, d access.PortalKBDescriptor, query string) ([]float32, error) {
			vector, err := embed(ctx, d, query)
			if err != nil {
				fmt.Printf("CONTROLLED_RANK_AB_EMBED_ERROR: type=%T forbidden=%t\n", err, err == access.ErrForbidden)
			}
			return vector, err
		}
		engine := gin.New()
		engine.Use(legacyReadScopeMiddleware(tenants))
		RegisterPersonalMCPRoutes(engine, mcpserver.NewConfiguredPersonalServer(&cfg, portal),
			tenants, users, members, keys, &cfg)
		rpc := func(key, method string, params any) (int, json.RawMessage) {
			raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/personal", bytes.NewReader(raw))
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			reply := httptest.NewRecorder()
			engine.ServeHTTP(reply, req)
			return reply.Code, append(json.RawMessage(nil), reply.Body.Bytes()...)
		}
		if code, _ := rpc("", "initialize", nil); code != http.StatusUnauthorized {
			t.Fatal("unauthenticated MCP request was not rejected")
		}
		if code, _ := rpc(input.Key, "initialize", map[string]any{
			"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "knowhub-controlled-rank-ab", "version": "1"},
		}); code != http.StatusOK {
			t.Fatal("normal MCP initialization failed")
		}
		code, directory := rpc(input.Key, "tools/call", map[string]any{
			"name": "list_knowledge_bases", "arguments": map[string]any{},
		})
		var envelope struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		var actual []struct {
			ID string `json:"id"`
		}
		if code != http.StatusOK || json.Unmarshal(directory, &envelope) != nil || envelope.Result.IsError ||
			len(envelope.Result.Content) != 1 {
			t.Fatal("normal MCP scope enumeration failed")
		}
		if json.Unmarshal([]byte(envelope.Result.Content[0].Text), &actual) != nil {
			t.Fatal("normal MCP scope enumeration failed")
		}
		ids := make([]string, len(actual))
		for i, kb := range actual {
			ids[i] = kb.ID
		}
		slices.Sort(ids)
		slices.Sort(input.ExpectedKBs)
		if !slices.Equal(ids, input.ExpectedKBs) {
			t.Fatal("normal authentication resolved a different knowledge scope")
		}
		preflight, _ := json.Marshal(map[string]any{
			"candidate": candidate, "unauthenticated_http": http.StatusUnauthorized,
			"normal_mcp_scope_equal": true, "authorized_kbs": len(ids), "database_read_only": true,
		})
		fmt.Println("CONTROLLED_RANK_AB_PREFLIGHT:" + string(preflight))
		for _, question := range input.Cases {
			started := time.Now()
			tool := "search_knowledge"
			args := map[string]any{"query": question.Question, "mode": "hybrid", "limit": input.ResultLimit}
			if input.Comparison == "technical_api_routes" {
				tool, args = question.Tool, question.Arguments
			}
			code, response := rpc(input.Key, "tools/call", map[string]any{
				"name": tool, "arguments": args,
			})
			var payload any = response
			if !json.Valid(response) {
				payload = string(response)
			}
			row := map[string]any{
				"case": question.ID, "candidate": candidate, "http": code, "response": payload,
				"duration_ms":    float64(time.Since(started).Microseconds()) / 1000,
				"authorized_kbs": len(ids), "original_question": input.Comparison == "ranking", "scope_narrowed": false,
				"comparison": input.Comparison, "tool": tool, "explicit_document_read": input.Comparison == "technical_api_routes",
				"result_limit":         input.ResultLimit,
				"runtime_baseline_sha": input.RuntimeSHA, "database_read_only": true,
				"transport": "actual Go MCP handler with normal authentication; no listener or worker",
			}
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal("cannot retain the actual comparison response")
			}
			fmt.Println("CONTROLLED_RANK_AB_ROW:" + string(raw))
		}
		if portal.ClosePortalCandidateWork() != nil {
			t.Fatal("candidate work did not shut down cleanly")
		}
	}
}
