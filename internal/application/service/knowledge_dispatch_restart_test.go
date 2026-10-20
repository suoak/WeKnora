package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// restartKnowledgeServiceForTest simulates a fresh process with the same durable
// dependencies, without copying the running service's sync.Map/noCopy fields.
func restartKnowledgeServiceForTest(t *testing.T, old *knowledgeService) *knowledgeService {
	t.Helper()
	svc, err := NewKnowledgeService(old.config, old.repo, old.documentReader, old.kbService,
		old.tenantRepo, old.tenantService, old.chunkService, old.chunkRepo, old.tagRepo, old.tagService,
		old.fileSvc, old.storageResolver, old.resourceCatalog, old.modelService, old.task, old.taskInspector,
		old.graphEngine, old.retrieveEngine, old.ownership, old.redisClient, old.kbShareService,
		old.imageResolver, old.wikiRepo, old.wikiService, old.taskPendingRepo, old.spanTracker, old.audit)
	require.NoError(t, err)
	restarted := svc.(*knowledgeService)
	restarted.personalRecovery = old.personalRecovery
	return restarted
}
