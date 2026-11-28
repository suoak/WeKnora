package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
)

// queueSourceAdmission preserves ordinary HTTP admission. Only an exact live
// SQL reparse continuation may use the existing scheduler authority path; that
// path independently reloads persisted source delegation and platform stops.
// No Web principal, access token, outbound permit or parser budget is minted.
func (s *DataSourceService) queueSourceAdmission(
	ctx context.Context, ds *types.DataSource, fingerprint string,
) (bool, error) {
	if !repository.HasSourceDocumentReparseDelivery(ctx) {
		return feishusynccontrol.QueueNormalSource(ctx, ds.ID, fingerprint)
	}
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return true, err
	}
	request, err := store.CheckSourceDocumentReparseDelivery(ctx, ds)
	if err != nil {
		return true, err
	}
	if err := s.documentReparseAuthority(ctx, ds, request.Initiator.UserID); err != nil {
		return true, err
	}
	return feishusynccontrol.QueueScheduledSource(ctx, ds.ID, fingerprint)
}
