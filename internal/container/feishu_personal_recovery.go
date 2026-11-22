package container

import (
	"errors"
	"os"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func configurePublishedSnapshotPortal(portal *service.PortalMCPKnowledge, personal *service.FeishuPersonalService) {
	service.BindPersonalPublishedSnapshotReads(portal, personal)
}

func registerPortalCandidateCleanup(portal *service.PortalMCPKnowledge, cleaner interfaces.ResourceCleaner) {
	if portal != nil && cleaner != nil {
		cleaner.RegisterWithName("PortalCandidateWork", portal.ClosePortalCandidateWork)
	}
}

func configurePersonalRecovery(
	personal *service.FeishuPersonalService,
	knowledge interfaces.KnowledgeService,
	shares interfaces.KBShareService,
	dataSources interfaces.DataSourceService,
	housekeeping *service.HousekeepingService,
) error {
	if ds, ok := dataSources.(*service.DataSourceService); ok {
		ds.SetFeishuIdentityService(personal)
		if err := housekeeping.SetSourceDocumentReparseRecovery(ds.RecoverSourceDocumentReparses); err != nil {
			return err
		}
	}
	if (os.Getenv(
		"FEISHU_APPLICATION_GRANT_TEST_ONLY",
	) == "1" ||
		os.Getenv(
			"FEISHU_APPLICATION_GRANT_ENABLED",
		) == "1") &&
		os.Getenv("FEISHU_PERSONAL_MVP_FIRST_CREATE") != "1" {
		return errors.New("application grant requires MVP first-create protocol")
	}
	if os.Getenv("FEISHU_PERSONAL_P1_PROTOCOL") != "1" &&
		os.Getenv("FEISHU_PERSONAL_MVP_FIRST_CREATE") != "1" {
		return nil
	}
	return service.EnablePersonalRecovery(
		personal,
		knowledge,
		shares,
		os.Getenv("FEISHU_PERSONAL_STAGING_DIR"),
	)
}
