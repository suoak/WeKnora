package container

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"go.uber.org/dig"
)

// TestBuildContainerDependencyGraph exercises the production container
// assembly, including its eager Invoke calls, while DryRun prevents external
// services and background workers from starting. This catches providers that
// exist in the file but are registered after an Invoke first needs them.
func TestBuildContainerDependencyGraph(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("BuildContainer dependency graph panicked: %v", recovered)
		}
	}()

	c := BuildContainer(dig.New(dig.DryRun(true)))
	if err := c.Invoke(func(
		*config.Config,
		*gin.Engine,
		interfaces.ResourceCleaner,
		interfaces.SystemSettingService,
	) {
	}); err != nil {
		t.Fatalf("resolve server entrypoint dependency graph: %v", err)
	}
}
