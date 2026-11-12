package mcpserver

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
)

// The same APP owns this handler. Configuration is off by default; enabled
// isolation requires an explicit policy reference and complete live wiring.
func NewConfiguredPersonalServer(cfg *config.Config, knowledge *service.PortalMCPKnowledge) *PersonalServer {
	if cfg == nil || cfg.PersonalMCP == nil || !cfg.PersonalMCP.Enabled || cfg.PersonalMCP.PolicyReference == "" || knowledge == nil {
		return NewDisabledPersonalServer()
	}
	limits, err := cfg.PersonalMCP.Concurrency.Resolve()
	if err != nil {
		logger.Errorf(context.Background(), "personal MCP disabled: invalid concurrency configuration")
		return NewDisabledPersonalServer()
	}
	return newPersonalServer(true, knowledge, limits)
}
