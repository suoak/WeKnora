package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type deletionRouteKnowledge struct{ interfaces.KnowledgeService }

func (*deletionRouteKnowledge) GetKnowledgeDeletionMetadata(ctx context.Context, id string) (*types.Knowledge, error) {
	if !types.IsKnowledgeDeletionMetadata(ctx, id) || types.CallerFromContext(ctx).TenantID != 1 {
		return nil, apperrors.NewNotFoundError("Knowledge not found")
	}
	return &types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: "kb", ParseStatus: "processing"}, nil
}

func (s *deletionRouteKnowledge) GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error) {
	return s.GetKnowledgeDeletionMetadata(ctx, id)
}

func (*deletionRouteKnowledge) GetOwningKBCreatorID(ctx context.Context, id string) (string, error) {
	if !types.IsKnowledgeDeletionMetadata(ctx, id) {
		return "", types.ErrLegacySourceRestricted
	}
	return "owner", nil
}

type deletionRouteKB struct {
	interfaces.KnowledgeBaseService
}

func (*deletionRouteKB) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	if !types.IsKBDeletionMetadata(ctx, id) {
		return nil, types.ErrLegacySourceRestricted
	}
	return &types.KnowledgeBase{ID: id, TenantID: 1, CreatorID: "owner"}, nil
}

type deletionRouteQueue struct{ tasks []*asynq.Task }

func (q *deletionRouteQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: "delete-task"}, nil
}

func TestKnowledgeDeletionMetadataRegisteredBeforeOwnershipAndWriteGuards(t *testing.T) {
	for _, actor := range []struct {
		user   string
		role   types.TenantRole
		tenant uint64
		status int
	}{
		{"owner", types.TenantRoleContributor, 1, http.StatusOK},
		{"other", types.TenantRoleContributor, 1, http.StatusForbidden},
		{"other", types.TenantRoleViewer, 1, http.StatusForbidden},
		{"owner", types.TenantRoleAdmin, 2, http.StatusNotFound},
	} {
		t.Run(actor.user+string(actor.role), func(t *testing.T) {
			enabled := true
			cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
			kg, kb, queue := &deletionRouteKnowledge{}, &deletionRouteKB{}, &deletionRouteQueue{}
			h := handler.NewKnowledgeHandler(cfg, kg, kb, nil, nil, nil, nil, nil, queue, nil, nil)
			g := &rbacGuards{
				cfg: cfg, knowledgeService: kg, kbService: kb,
				knowledgeKBCreator: h.KBCreatorLookupFromKnowledgeID,
			}
			r := gin.New()
			r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, actor.tenant)
				ctx = context.WithValue(ctx, types.UserIDContextKey, actor.user)
				ctx = context.WithValue(ctx, types.TenantRoleContextKey, actor.role)
				ctx = types.WithCaller(ctx, types.Caller{TenantID: actor.tenant, UserID: actor.user, Role: actor.role})
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			RegisterKnowledgeRoutes(r.Group("/api/v1"), h, g)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge/doc", nil))
			require.Equal(t, actor.status, w.Code, w.Body.String())
			if actor.status != http.StatusOK {
				require.Empty(t, queue.tasks)
				return
			}
			require.Len(t, queue.tasks, 1)
			var payload types.KnowledgeListDeletePayload
			require.NoError(t, json.Unmarshal(queue.tasks[0].Payload(), &payload))
			require.EqualValues(t, 1, payload.TenantID)
			require.Equal(t, "kb", payload.KnowledgeBaseID)
			require.Equal(t, []string{"doc"}, payload.KnowledgeIDs)
			_, err := kg.GetKnowledgeByIDOnly(context.Background(), "doc")
			require.Error(t, err)
			require.Error(t, access.RequireKBWrite(context.Background(), &types.KnowledgeBase{ID: "kb", TenantID: 1}))
		})
	}
}
