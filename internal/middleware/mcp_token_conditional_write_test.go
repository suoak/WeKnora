package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// A barrier wraps only the real SQL read; it never fabricates persistence or
// authentication results. The paused caller holds no SQLite transaction/lock.
type endpointReadBarrier struct {
	interfaces.MCPEndpointRepository
	once   sync.Once
	read   chan struct{}
	resume chan struct{}
}

func (r *endpointReadBarrier) GetByID(ctx context.Context, id string) (*types.MCPEndpoint, error) {
	ep, err := r.MCPEndpointRepository.GetByID(ctx, id)
	if err == nil && ep != nil {
		r.once.Do(func() {
			close(r.read)
			select {
			case <-r.resume:
			case <-ctx.Done():
			}
		})
	}
	return ep, err
}

func endpointAdminContext() context.Context {
	return types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "wang", Role: types.TenantRoleAdmin})
}

func waitEndpointSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("barrier deadline, no sleep-based scheduling")
	}
}

func TestMCPEndpointHTTPConditionalConfigInterleavings(t *testing.T) {
	for _, change := range []string{"config", "disable", "scope", "tools", "rotate", "delete"} {
		t.Run(change, func(t *testing.T) {
			h := newLifecycleHarness(t)
			barrier := &endpointReadBarrier{MCPEndpointRepository: repository.NewMCPEndpointRepository(h.db), read: make(chan struct{}), resume: make(chan struct{})}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(barrier.resume) }) })
			svc := service.NewMCPEndpointService(barrier, h.adapters, &lifecycleShares{db: h.db}, nil)
			engine := h.server.Config.Handler.(*gin.Engine)
			admin := func(c *gin.Context) {
				c.Set(types.TenantIDContextKey.String(), uint64(1))
				c.Request = c.Request.WithContext(endpointAdminContext())
				c.Next()
			}
			yes := true
			guard := middleware.RequireRole(types.TenantRoleAdmin, &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &yes}})
			engine.PUT("/api/v1/mcp-endpoints/:endpoint_id", admin, guard, handler.NewMCPEndpointHandler(svc).UpdateMCPEndpoint)
			done := make(chan int, 1)
			go func() {
				code, _ := h.request(t, "PUT", "/api/v1/mcp-endpoints/"+h.id, "wang", "", map[string]any{"name": "stale config"})
				done <- code
			}()
			waitEndpointSignal(t, barrier.read)
			var err error
			var newToken string
			switch change {
			case "config":
				name := "winning config"
				_, err = h.svc.Update(endpointAdminContext(), 1, h.id, interfaces.MCPEndpointUpdate{Name: &name})
			case "disable":
				enabled := false
				_, err = h.svc.Update(endpointAdminContext(), 1, h.id, interfaces.MCPEndpointUpdate{Enabled: &enabled})
			case "scope":
				ids := []string{"KB-A"}
				_, err = h.svc.Update(endpointAdminContext(), 1, h.id, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &ids})
			case "tools":
				tools := []string{types.MCPEndpointToolListKnowledgeBases}
				_, err = h.svc.Update(endpointAdminContext(), 1, h.id, interfaces.MCPEndpointUpdate{Tools: &tools})
			case "rotate":
				_, newToken, err = h.svc.RotateToken(endpointAdminContext(), 1, h.id)
			case "delete":
				err = h.svc.Delete(endpointAdminContext(), 1, h.id)
			}
			if err != nil {
				t.Fatal(err)
			}
			release.Do(func() { close(barrier.resume) })
			select {
			case code := <-done:
				if code != 409 {
					t.Fatalf("stale HTTP config status=%d, want409", code)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("HTTP caller did not complete")
			}
			t.Logf("interleaving=%s: winning mutation completed; stale HTTP PUT=409", change)
			ep, err := h.svc.Get(context.Background(), 1, h.id)
			if change == "delete" {
				if err == nil {
					t.Fatal("deleted endpoint readable")
				}
				var raw types.MCPEndpoint
				if err := h.db.Unscoped().First(&raw, "id = ?", h.id).Error; err != nil || !raw.DeletedAt.Valid {
					t.Fatal("deleted SQL row resurrected")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "config":
				if ep.Name != "winning config" {
					t.Fatal("lost config update")
				}
			case "disable":
				if ep.Enabled {
					t.Fatal("stale enabled restored")
				}
				if code, _ := h.search(t, h.token, "KB-A"); code != 403 {
					t.Fatal("disabled credential allowed")
				}
			case "scope":
				if len(ep.KnowledgeBaseIDs) != 1 || ep.KnowledgeBaseIDs[0] != "KB-A" {
					t.Fatal("scope restored")
				}
				if _, allowed := h.search(t, h.token, "KB-S"); allowed {
					t.Fatal("removed KB allowed")
				}
			case "rotate":
				if code, _ := h.search(t, h.token, "KB-A"); code != 401 {
					t.Fatal("rotated credential revived")
				}
				if code, allowed := h.search(t, newToken, "KB-A"); code != 200 || !allowed {
					t.Fatal("new token broken")
				}
				if len(ep.KnowledgeBaseIDs) != 2 {
					t.Fatal("rotation changed scope")
				}
			case "tools":
				if ep.HasTool(types.MCPEndpointToolSearchKnowledge) {
					t.Fatal("stale write restored tool grant")
				}
				if _, allowed := h.search(t, h.token, "KB-A"); allowed {
					t.Fatal("revoked tool executed")
				}
			case "delete":
				if code, _ := h.search(t, h.token, "KB-A"); code != 401 {
					t.Fatal("deleted credential revived")
				}
			}
		})
	}
}

func TestMCPEndpointRotationConditionalInterleavings(t *testing.T) {
	for _, winner := range []string{"config", "rotate", "delete"} {
		t.Run(winner, func(t *testing.T) {
			h := newLifecycleHarness(t)
			barrier := &endpointReadBarrier{MCPEndpointRepository: repository.NewMCPEndpointRepository(h.db), read: make(chan struct{}), resume: make(chan struct{})}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(barrier.resume) }) })
			svc := service.NewMCPEndpointService(barrier, h.adapters, &lifecycleShares{db: h.db}, nil)
			type outcome struct {
				token string
				err   error
			}
			done := make(chan outcome, 1)
			go func() { _, token, err := svc.RotateToken(endpointAdminContext(), 1, h.id); done <- outcome{token, err} }()
			waitEndpointSignal(t, barrier.read)
			var token string
			var err error
			switch winner {
			case "config":
				ids := []string{"KB-A"}
				_, err = h.svc.Update(endpointAdminContext(), 1, h.id, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &ids})
			case "rotate":
				_, token, err = h.svc.RotateToken(endpointAdminContext(), 1, h.id)
			case "delete":
				err = h.svc.Delete(endpointAdminContext(), 1, h.id)
			}
			if err != nil {
				t.Fatal(err)
			}
			release.Do(func() { close(barrier.resume) })
			select {
			case result := <-done:
				if result.err == nil || result.token != "" {
					t.Fatal("stale rotate published success/token")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("rotate waiter timeout")
			}
			if winner == "rotate" {
				if code, allowed := h.search(t, token, "KB-A"); code != 200 || !allowed {
					t.Fatal("winning rotated credential lost")
				}
			}
			if winner == "config" {
				ep, err := h.svc.Get(context.Background(), 1, h.id)
				if err != nil || len(ep.KnowledgeBaseIDs) != 1 {
					t.Fatal("rotation restored old scope")
				}
			}
			if winner != "config" {
				if code, _ := h.search(t, h.token, "KB-A"); code != 401 {
					t.Fatal("old token revived")
				}
			}
			t.Logf("winner=%s; stale rotate rejected without publishing token", winner)
		})
	}
}

func TestMCPEndpointHTTPZeroValuesAndToolsPolicy(t *testing.T) {
	h := newLifecycleHarness(t)
	ids := []string{}
	description := ""
	enabled := false
	ep, err := h.svc.Update(endpointAdminContext(), 1, h.id, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &ids, Description: &description, Enabled: &enabled})
	if err != nil || ep.Enabled || len(ep.KnowledgeBaseIDs) != 0 || ep.Description != "" {
		t.Fatal("zero-value config not saved")
	}
	tools := []string{}
	if _, err := h.svc.Update(endpointAdminContext(), 1, h.id, interfaces.MCPEndpointUpdate{Tools: &tools}); err == nil {
		t.Fatal("existing empty-tools API policy changed")
	}
	if _, err := h.svc.Update(endpointAdminContext(), 2, h.id, interfaces.MCPEndpointUpdate{Enabled: &enabled}); err == nil {
		t.Fatal("foreign workspace update accepted")
	}
	if code, _ := h.search(t, h.token, "KB-A"); code != 403 {
		t.Fatal("disabled token accepted")
	}
	// No response/error should echo credentials. A normal Get only returns metadata.
	raw, _ := json.Marshal(ep)
	if strings.Contains(string(raw), h.token) || strings.Contains(string(raw), service.HashMCPEndpointToken(h.token)) {
		t.Fatal("credential serialized")
	}
}

func TestMCPEndpointDeleteAfterCommittedRotation(t *testing.T) {
	h := newLifecycleHarness(t)
	_, token, err := h.svc.RotateToken(endpointAdminContext(), 1, h.id)
	if err != nil {
		t.Fatal(err)
	}
	if code, allowed := h.search(t, token, "KB-A"); code != 200 || !allowed {
		t.Fatal("committed rotated key not active")
	}
	if err := h.svc.Delete(endpointAdminContext(), 1, h.id); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{h.token, token} {
		if code, _ := h.search(t, key, "KB-A"); code != 401 {
			t.Fatal("delete did not revoke latest generation")
		}
	}
}

func TestMCPEndpointHTTPStorageErrorDoesNotPublishToken(t *testing.T) {
	h := newLifecycleHarness(t)
	// Injection is at the real SQL callback boundary, not a successful mock repo.
	if err := h.db.Callback().Update().Before("gorm:update").Register("qa:storage_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "mcp_endpoints" {
			tx.AddError(errors.New("synthetic SQL error contains " + h.token))
		}
	}); err != nil {
		t.Fatal(err)
	}
	code, body := h.request(t, "POST", "/api/v1/mcp-endpoints/"+h.id+"/rotate-token", "wang", "", nil)
	if code != 500 || strings.Contains(string(body), h.token) || strings.Contains(string(body), "synthetic SQL") {
		t.Fatal("SQL error leaked or false rotation success")
	}
	var reply map[string]any
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatal(err)
	}
	if _, ok := reply["data"]; ok {
		t.Fatal("failed rotation exposed new credential data")
	}
	if code, allowed := h.search(t, h.token, "KB-A"); code != 200 || !allowed {
		t.Fatal("failed rotation revoked committed old key")
	}
}
