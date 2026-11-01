package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *knowledgeService) bindExactStoredImages(
	ctx context.Context, consumer *exactPersonalConsumer, images []docparser.StoredImage,
) error {
	if consumer == nil {
		return errPersonalExactDocument
	}
	if _, err := consumer.recovery.revalidateExactPersonalDocument(ctx, consumer.source); err != nil {
		return err
	}
	if s.resourceCatalog == nil {
		// Legacy local storage retains its existing path authorization. A
		// catalog-enabled deployment must prove every extracted-image binding.
		return nil
	}
	for _, image := range images {
		resource, err := s.resourceCatalog.Resolve(ctx, image.ServingURL)
		if err != nil {
			return err
		}
		if resource == nil || resource.TenantID != consumer.source.tenant {
			return errPersonalExactDocument
		}
		if err := s.resourceCatalog.Bind(ctx, image.ServingURL,
			types.ResourceOwnerKnowledge, consumer.source.knowledge, types.ResourceRelationExtractedImage); err != nil {
			return err
		}
	}
	_, err := consumer.recovery.revalidateExactPersonalDocument(ctx, consumer.source)
	return err
}
