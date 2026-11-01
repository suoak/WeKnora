package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type exactDetailReadRepository struct {
	interfaces.KnowledgeRepository
	read func(context.Context, uint64, string) (*types.Knowledge, error)
}

func (r exactDetailReadRepository) GetKnowledgeByID(
	ctx context.Context,
	tenant uint64,
	id string,
) (*types.Knowledge, error) {
	return r.read(ctx, tenant, id)
}

func TestExactPublishedDetailChecksSQLBoundaries(t *testing.T) {
	for _, byIDOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "tenant-scoped", true: "permission-resolution"}[byIDOnly], func(t *testing.T) {
			t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
			ctx := types.WithExecutionTenant(context.Background(), 1)
			ctx = context.WithValue(
				ctx,
				types.PrincipalContextKey,
				types.Principal{Type: types.PrincipalWebUser, ID: "viewer"},
			)
			checks := 0
			revoked := false
			ctx, err := types.WithExactPublishedDocumentRead(ctx, 7, types.ExactDocumentScope{
				TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: func(context.Context) error {
					checks++
					if revoked {
						return types.ErrExactDocumentScope
					}
					return nil
				},
			})
			require.NoError(t, err)
			selections := 0
			revokeDuringSelection := false
			var escaped context.Context
			s := &knowledgeService{
				repo: exactDetailReadRepository{
					read: func(sql context.Context, tenant uint64, id string) (*types.Knowledge, error) {
						selections++
						escaped = sql
						require.EqualValues(t, 1, tenant)
						require.Equal(t, "doc", id)
						ok, err := types.CheckExactDocumentRead(sql, id, "kb")
						require.True(t, ok)
						require.NoError(t, err)
						if revokeDuringSelection {
							revoked = true
						}
						return &types.Knowledge{ID: id, TenantID: tenant, KnowledgeBaseID: "kb"}, nil
					},
				},
			}
			read := s.GetKnowledgeByID
			if byIDOnly {
				read = s.GetKnowledgeByIDOnly
			}
			row, err := read(ctx, "doc")
			require.NoError(t, err)
			require.Equal(t, "doc", row.ID)
			require.Equal(t, 3, checks, "issuance, before SQL, before return; no duplicate remote row check")
			_, err = types.CheckExactDocumentRead(escaped, "doc", "kb")
			require.ErrorIs(t, err, types.ErrExactDocumentScope)
			row, err = read(ctx, "other")
			require.ErrorIs(t, err, types.ErrExactDocumentScope)
			require.Nil(t, row)
			require.Equal(t, 1, selections)
			revokeDuringSelection = true
			row, err = read(ctx, "doc")
			require.ErrorIs(t, err, types.ErrExactDocumentScope)
			require.Nil(t, row, "revocation before return discards selected content")
			row, err = read(ctx, "doc")
			require.ErrorIs(t, err, types.ErrExactDocumentScope)
			require.Nil(t, row)
			require.Equal(t, 2, selections, "already revoked input cannot reach SQL")
		})
	}
}
