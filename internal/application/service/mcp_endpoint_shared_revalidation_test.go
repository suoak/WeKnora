package service

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

type endpointLiveShares struct {
	interfaces.KBShareService
	allowed bool
}

// Only the authorization input is synthetic. Endpoint persistence and CAS
// use the production GORM repository. No sleep determines the interleaving.
type endpointSaveRevocationShares struct {
	interfaces.KBShareService
	allowed           atomic.Bool
	checking, release chan struct{}
	trace             func(string)
}

func (s *endpointSaveRevocationShares) CheckTenantKBPermission(ctx context.Context, id string, tenant uint64, _ types.TenantRole) (types.OrgMemberRole, bool, error) {
	s.trace("authorization-enter")
	if s.checking != nil {
		close(s.checking)
		select {
		case <-s.release:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	ok := s.allowed.Load() && id == "shared" && tenant == 1
	s.trace("authorization-complete")
	return types.OrgRoleViewer, ok, nil
}

type endpointSaveCommitBarrier struct {
	interfaces.MCPEndpointRepository
	ready, release chan struct{}
	trace          func(string)
}

func (r *endpointSaveCommitBarrier) Update(ctx context.Context, ep *types.MCPEndpoint) error {
	r.trace("before-commit")
	if r.ready != nil {
		close(r.ready)
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := r.MCPEndpointRepository.Update(ctx, ep)
	if err == nil {
		r.trace("commit")
	}
	return err
}

func TestMCPEndpointSharedSaveRevocationInterleavings(t *testing.T) {
	for _, phase := range []string{"before-check", "during-check", "after-check-before-commit"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "scope.sqlite")), &gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			sqlDB.SetMaxOpenConns(1)
			defer sqlDB.Close()
			if err := db.AutoMigrate(&types.MCPEndpoint{}); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			start := time.Now()
			var events []string
			trace := func(event string) {
				mu.Lock()
				defer mu.Unlock()
				events = append(events, event)
				t.Logf("event=%s monotonic_ns=%d", event, time.Since(start).Nanoseconds())
			}
			shares := &endpointSaveRevocationShares{trace: trace}
			shares.allowed.Store(true)
			repo := &endpointSaveCommitBarrier{MCPEndpointRepository: repository.NewMCPEndpointRepository(db), trace: trace}
			svc := &mcpEndpointService{repo: repo, kbShareService: shares, kbService: &stubKBForMCPEndpoint{kbs: map[string]*types.KnowledgeBase{"own": {ID: "own", TenantID: 1}, "shared": {ID: "shared", TenantID: 2}}}}
			ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "synthetic-admin", Role: types.TenantRoleAdmin})
			ep, token, err := svc.Create(ctx, 1, &types.MCPEndpoint{Name: "scope", Enabled: true, Tools: types.StringArray{"search_knowledge"}, KnowledgeBaseIDs: types.StringArray{"own"}})
			if err != nil {
				t.Fatal(err)
			}
			var pause, resume chan struct{}
			if phase == "during-check" {
				pause, resume = make(chan struct{}), make(chan struct{})
				shares.checking, shares.release = pause, resume
			}
			if phase == "after-check-before-commit" {
				pause, resume = make(chan struct{}), make(chan struct{})
				repo.ready, repo.release = pause, resume
			}
			revoke := func() { shares.allowed.Store(false); trace("revocation") }
			if phase == "before-check" {
				revoke()
			}
			done := make(chan error, 1)
			ids := []string{"own", "shared"}
			trace("save-start")
			go func() {
				_, err := svc.Update(ctx, 1, ep.ID, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &ids})
				done <- err
			}()
			if pause != nil {
				select {
				case <-pause:
				case <-ctx.Done():
					t.Fatal("save did not reach barrier")
				}
				revoke()
				close(resume)
			}
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal("save did not finish")
			}
			stored, readErr := repo.GetByID(ctx, ep.ID)
			if readErr != nil || stored == nil {
				t.Fatal("cannot read committed state")
			}
			if phase == "after-check-before-commit" {
				if err != nil || len(stored.KnowledgeBaseIDs) != 2 {
					t.Fatalf("authorized save boundary: %v", err)
				}
				// A historical ID is configuration, not a current permission grant.
				if _, err := svc.validateKnowledgeBases(ctx, 1, []string{"shared"}); err == nil {
					t.Fatal("new permission check accepted revoked KB")
				}
			} else if err == nil || len(stored.KnowledgeBaseIDs) != 1 {
				t.Fatal("revocation before completed check must reject save without changing SQL state")
			}
			if _, err := svc.Authenticate(ctx, ep.ID, token); err != nil {
				t.Fatal("resource revocation incorrectly revoked machine token")
			}
			mu.Lock()
			t.Logf("linearized_events=%v stored_kb_count=%d", events, len(stored.KnowledgeBaseIDs))
			mu.Unlock()
		})
	}
}

func (s *endpointLiveShares) CheckTenantKBPermission(_ context.Context, id string, tenant uint64, _ types.TenantRole) (types.OrgMemberRole, bool, error) {
	return types.OrgRoleViewer, s.allowed && tenant == 1 && id == "shared", nil
}
func TestMCPEndpointSharedSaveRevalidation(t *testing.T) {
	repo := newStubMCPEndpointRepo()
	shares := &endpointLiveShares{allowed: true}
	svc := &mcpEndpointService{repo: repo, kbShareService: shares, kbService: &stubKBForMCPEndpoint{kbs: map[string]*types.KnowledgeBase{
		"own": {ID: "own", TenantID: 1}, "shared": {ID: "shared", TenantID: 2}, "foreign": {ID: "foreign", TenantID: 3},
	}}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "synthetic-admin", Role: types.TenantRoleAdmin})
	ep, token, err := svc.Create(ctx, 1, &types.MCPEndpoint{Name: "shared", Enabled: true, Tools: types.StringArray{"search_knowledge"}, KnowledgeBaseIDs: types.StringArray{"own", "shared", "shared"}})
	if err != nil || len(ep.KnowledgeBaseIDs) != 2 {
		t.Fatalf("live shared and dedup: %v %v", ep, err)
	}
	bad := []string{"foreign"}
	if _, err = svc.Update(ctx, 1, ep.ID, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &bad}); err == nil {
		t.Fatal("forged ID accepted")
	}
	if _, err = svc.validateKnowledgeBases(ctx, 4, []string{"shared"}); err == nil {
		t.Fatal("caller switched workspace")
	}
	shares.allowed = false
	// Authentication is still valid; per-KB authorization is independently revoked.
	if _, err := svc.Authenticate(context.Background(), ep.ID, token); err != nil {
		t.Fatalf("KB share revocation must not alter machine token lifecycle: %v", err)
	}
	selection := []string{"own", "shared"}
	if _, err = svc.Update(ctx, 1, ep.ID, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &selection}); err == nil {
		t.Fatal("revoked saved ID accepted on update")
	}
	stored, err := repo.GetByID(ctx, ep.ID)
	if err != nil || len(stored.KnowledgeBaseIDs) != 2 {
		t.Fatalf("failed save overwrote config: %v %v", stored, err)
	}
}

func TestMCPEndpointCreatorDowngradeDoesNotRevokeMachineToken(t *testing.T) {
	svc := newMCPEndpointServiceForTest(newStubMCPEndpointRepo(), nil)
	admin := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "synthetic-creator", Role: types.TenantRoleAdmin})
	ep, token, err := svc.Create(admin, 7, &types.MCPEndpoint{Name: "machine", Enabled: true, Tools: types.StringArray{"search_knowledge"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, caller := range []types.Caller{
		{TenantID: 7, UserID: "synthetic-creator", Role: types.TenantRoleViewer},
		{TenantID: 8, UserID: "synthetic-creator", Role: types.TenantRoleViewer},
		{}, // Creator exits; the machine token authenticates independently of web identity.
	} {
		got, err := svc.Authenticate(types.WithCaller(context.Background(), caller), ep.ID, token)
		if err != nil || got.TenantID != 7 {
			t.Fatalf("creator change incorrectly invalidates/scopes machine token: %v %v", got, err)
		}
	}
	// Administrators can still revoke the machine credential through its existing lifecycle.
	if err := svc.Delete(admin, 7, ep.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), ep.ID, token); err == nil {
		t.Fatal("deleted endpoint token still authenticates")
	}
}
