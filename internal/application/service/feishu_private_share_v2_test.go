package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type v2PrivateShareKB struct {
	interfaces.KnowledgeBaseRepository
}

func (v2PrivateShareKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "private", TenantID: 1, CreatorID: "A", PrivateOwnerID: "A"}, nil
}

func TestFeishuV2PrivateKBSharingCannotWidenItsAudience(t *testing.T) {
	// Missing share/org repositories deliberately fail the test if the request
	// reaches a side effect. Publication belongs to the explicit document
	// approval flow into a different KB, not a privacy reclassification.
	s := &kbShareService{kbRepo: v2PrivateShareKB{}}
	_, err := s.ShareKnowledgeBase(personalUser("A"), "private", "organization", "A", 1, types.OrgRoleViewer)
	require.ErrorIs(t, err, ErrSharePermissionDenied)
}
