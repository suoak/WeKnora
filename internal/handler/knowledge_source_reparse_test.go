package handler

import (
	"context"
	"net/http"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type sourceReparseBatchKnowledge struct {
	*mutationAdmissionKnowledge
	checked []string
}

func (s *sourceReparseBatchKnowledge) ValidateKnowledgeReparse(_ context.Context, id string) error {
	s.checked = append(s.checked, id)
	if id == "source" {
		return apperrors.NewConflictError("use datasource synchronization")
	}
	return nil
}

func TestSourceReparseMixedBatchRejectsBeforeAnyEnqueue(t *testing.T) {
	kg := &sourceReparseBatchKnowledge{mutationAdmissionKnowledge: &mutationAdmissionKnowledge{
		rows: []*types.Knowledge{
			{ID: "ordinary", TenantID: 7, KnowledgeBaseID: "kb"},
			{ID: "source", TenantID: 7, KnowledgeBaseID: "kb"},
		},
	}}
	queue := &documentDeleteEnqueuer{}
	h := &KnowledgeHandler{
		cfg: transferHandlerConfig(), kbService: admissionKB("user"),
		kgService: kg, asynqClient: queue,
	}
	r := documentHandlerRouter()
	r.POST("/reparse", h.BatchReparseKnowledge)
	w := mutationRequest(r, http.MethodPost, "/reparse", `{"kb_id":"kb","ids":["ordinary","source"]}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, []string{"ordinary", "source"}, kg.checked)
	require.Nil(t, queue.task, "an ordinary prefix cannot be destructively queued before a source rejection")
}
