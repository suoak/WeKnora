package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type admissionSnapshotStore struct {
	*selectedMCPStore
	loads int
}

func (s *admissionSnapshotStore) LoadDocumentReadSnapshot(
	ctx context.Context, user string, key uint64, kb string,
) (*types.EnterpriseReadSnapshot, error) {
	load := func() (*types.EnterpriseReadSnapshot, error) {
		s.loads++
		return s.selectedMCPStore.LoadDocumentReadSnapshot(ctx, user, key, kb)
	}
	return access.ReadPortalSnapshot(ctx, s, user, key, kb, false, false, load)
}

func TestPersonalAdmissionSnapshotPhaseHasIndependentOutputFence(t *testing.T) {
	for _, mutation := range []string{"none", "key", "version", "hold"} {
		t.Run(mutation, func(t *testing.T) {
			s, original, ctx := selectedMCPFixture()
			store := &admissionSnapshotStore{selectedMCPStore: original}
			s.knowledge.Resolver.Store = store
			s.knowledge.Resolver.Documents = access.NewDocumentReader(
				true, store, selectedMCPBase{}, selectedMCPEmployee{},
			)
			called := false
			handler := s.enforceGuard(func(
				handlerCtx context.Context, _ mcp.CallToolRequest,
			) (*mcp.CallToolResult, error) {
				called = true
				require.Equal(t, 1, store.loads, "admission must merge identical candidate policy reads")
				// An independent handler read must not see the admission cache.
				_, err := store.LoadDocumentReadSnapshot(handlerCtx, "A", 1, "selected")
				require.NoError(t, err)
				require.Equal(t, 2, store.loads)
				switch mutation {
				case "key":
					original.revoked = true
				case "version":
					original.version = "v2"
				case "hold":
					original.held = true
				}
				return mcp.NewToolResultText("SYNTHETIC_OUTPUT"), nil
			})
			request := selectedMCPRequest(t, map[string]any{"knowledge_base_ids": []string{"selected"}})
			result, err := handler(ctx, request)
			require.NoError(t, err)
			require.True(t, called)
			if mutation == "none" {
				require.False(t, result.IsError)
				require.Equal(t, 3, store.loads, "the final fence must independently reload policy")
			} else {
				require.True(t, result.IsError)
				encoded, encodeErr := json.Marshal(result)
				require.NoError(t, encodeErr)
				require.NotContains(t, string(encoded), "SYNTHETIC_OUTPUT")
			}
		})
	}
}
