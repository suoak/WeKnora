package types

import "testing"

func TestApprovedSyncLaneRequiresExplicitDeploymentOptIn(t *testing.T) {
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "")
	if QueueWeightsForPool(WorkerPoolMaintenance)[QueueApprovedSync] != 0 {
		t.Fatal("disabled recovery lane must have no consumer")
	}
	for _, definition := range QueueDefinitions() {
		if definition.Name == QueueApprovedSync {
			t.Fatal("disabled lane must not appear in runtime inventory")
		}
	}
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "1")
	if QueueWeightsForPool(WorkerPoolMaintenance)[QueueApprovedSync] != 1 {
		t.Fatal("enabled recovery lane must use maintenance worker")
	}
	found := false
	for _, definition := range QueueDefinitions() {
		if definition.Name == QueueApprovedSync {
			found = definition.DeliveryOf == QueueSync
		}
	}
	if !found {
		t.Fatal("enabled lane must declare its existing handler source")
	}
	queue, ok := QueueForTaskType(TypeDataSourceSync)
	if !ok || queue != QueueSync {
		t.Fatal("ordinary sync tasks must retain their shared queue identity")
	}
	if QueueWeightsForSharedPool()[QueueApprovedSync] != 0 {
		t.Fatal("elastic workers must not consume recovery work")
	}
}
