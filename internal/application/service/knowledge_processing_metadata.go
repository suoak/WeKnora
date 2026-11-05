package service

import (
	"context"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// GetKnowledgeProcessingMetadata serves only the exact status-route scope. It
// does not load a published receipt or turn staging output into readable content.
func (s *knowledgeService) GetKnowledgeProcessingMetadata(
	ctx context.Context, id string,
) (*types.Knowledge, error) {
	if !types.IsKnowledgeProcessingMetadata(ctx, id) || types.CallerFromContext(ctx).TenantID == 0 {
		return nil, apperrors.NewForbiddenError("Processing metadata scope required")
	}
	reader, ok := s.repo.(interface {
		GetKnowledgeProcessingMetadata(context.Context, string) (*types.Knowledge, error)
	})
	if !ok {
		return nil, apperrors.NewServiceUnavailableError("Processing metadata unavailable")
	}
	return reader.GetKnowledgeProcessingMetadata(ctx, id)
}
