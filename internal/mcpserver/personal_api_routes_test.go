package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestPersonalTechnicalAPIRoutePolicyAndSerialization(t *testing.T) {
	base := logger.WithIsolatedPersonalPayload(context.Background())
	value := map[string]any{
		"content": "POST `/v2/widgets/activate/` and /home/app/private.txt", "technical_api_routes": true,
	}
	for _, enabled := range []bool{false, true} {
		ctx := base
		if enabled {
			ctx = context.WithValue(ctx, personalTechnicalRoutesKey{}, true)
		}
		result, err := personalJSON(ctx, value)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		require.NotContains(t, text, "/home/app/private.txt")
		if enabled {
			require.Contains(t, text, "/v2/widgets/activate/")
		} else {
			require.NotContains(t, text, "/v2/widgets/activate/")
		}
		var decoded any
		require.NoError(t, json.Unmarshal([]byte(text), &decoded))
		require.Equal(t, decoded, result.StructuredContent, "text and structured content must share the same policy")
	}
	cfg := &config.Config{PersonalMCP: &config.PersonalMCPConfig{
		Enabled: true, PolicyReference: "synthetic-approved-policy",
	}}
	require.False(t, NewConfiguredPersonalServer(cfg, &service.PortalMCPKnowledge{}).technicalAPIRoutes)
	cfg.PersonalMCP.TechnicalAPIRoutes = true
	require.True(t, NewConfiguredPersonalServer(cfg, &service.PortalMCPKnowledge{}).technicalAPIRoutes)
	require.False(t, NewPersonalServer(true, &service.PortalMCPKnowledge{}).technicalAPIRoutes,
		"unconfigured constructors do not opt in")
}
