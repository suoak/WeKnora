package middleware_test

// Required security assertions. An explicit stale-snapshot interleaving replaces
// sleeps; these must run against the actual GORM repository, not an in-memory
// repository mock. Subsequent credential checks are real HTTP/MCP requests.
import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
)

func TestMCPEndpointStaleConfigWriteCannotRestoreRotatedKey(t *testing.T) {
	h := newLifecycleHarness(t)
	repo := repository.NewMCPEndpointRepository(h.db)
	stale, err := repo.GetByID(context.Background(), h.id)
	if err != nil {
		t.Fatal(err)
	}
	status, _ := h.request(t, "POST", "/api/v1/mcp-endpoints/"+h.id+"/rotate-token", "wang", "", nil)
	if status != 200 {
		t.Fatalf("rotate: %d", status)
	}
	if status, _ := h.search(t, h.token, "KB-A"); status != 401 {
		t.Fatal("old key was not revoked before interleaving")
	}
	stale.Name = "Synthetic stale configuration"
	// This is the same repository update used by the production config handler
	// after its GetByID; token fields must not be restored by a config update.
	if err := repo.Update(context.Background(), stale); err != nil {
		t.Log("stale write refused, as required")
	}
	if status, _ := h.search(t, h.token, "KB-A"); status != 401 {
		t.Fatalf("stale config reactivated rotated credential: %d", status)
	}
}

func TestMCPEndpointStaleWriteCannotResurrectDeletedEndpoint(t *testing.T) {
	h := newLifecycleHarness(t)
	repo := repository.NewMCPEndpointRepository(h.db)
	stale, err := repo.GetByID(context.Background(), h.id)
	if err != nil {
		t.Fatal(err)
	}
	status, _ := h.request(t, "DELETE", "/api/v1/mcp-endpoints/"+h.id, "wang", "", nil)
	if status != 200 {
		t.Fatalf("delete: %d", status)
	}
	if status, _ := h.search(t, h.token, "KB-A"); status != 401 {
		t.Fatal("delete was not effective before interleaving")
	}
	stale.Name = "Synthetic stale configuration"
	if err := repo.Update(context.Background(), stale); err != nil {
		t.Log("stale write refused, as required")
	}
	if status, _ := h.search(t, h.token, "KB-A"); status != 401 {
		t.Fatalf("stale write resurrected deleted machine credential: %d", status)
	}
}
