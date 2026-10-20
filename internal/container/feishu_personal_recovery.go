package container

import (
	"errors"
	"os"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func configurePersonalRecovery(personal *service.FeishuPersonalService, knowledge interfaces.KnowledgeService, shares interfaces.KBShareService) error {
	if (os.Getenv("FEISHU_APPLICATION_GRANT_TEST_ONLY") == "1" || os.Getenv("FEISHU_APPLICATION_GRANT_ENABLED") == "1") && os.Getenv("FEISHU_PERSONAL_MVP_FIRST_CREATE") != "1" {
		return errors.New("application grant requires MVP first-create protocol")
	}
	if os.Getenv("FEISHU_PERSONAL_P1_PROTOCOL") != "1" && os.Getenv("FEISHU_PERSONAL_MVP_FIRST_CREATE") != "1" {
		return nil
	}
	return service.EnablePersonalRecovery(personal, knowledge, shares, os.Getenv("FEISHU_PERSONAL_STAGING_DIR"))
}
