package mcpserver

import (
	"context"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/officeguard"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestOfficeMCPErrorUsesControlledCodeAndMessage(t *testing.T) {
	result := contentFailure(
		context.Background(),
		&werrors.AppError{Code: werrors.ErrOfficeEncrypted, Message: "private path must not leak"},
		"generic",
	)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	msg := result.Content[0].(mcp.TextContent).Text
	require.Equal(t, "OFFICE_ENCRYPTED (2400): "+officeguard.Message, msg)
	require.Equal(
		t,
		map[string]any{"error": map[string]any{"code": 2400, "message": officeguard.Message}},
		result.StructuredContent,
	)
}

func TestOfficeMCPDRMUsesControlledRightsManagementMessage(t *testing.T) {
	result := contentFailure(
		context.Background(),
		&werrors.AppError{Code: werrors.ErrOfficeEncrypted, Message: officeguard.DRMMessage},
		"generic",
	)
	require.True(t, result.IsError)
	require.Equal(t, "OFFICE_ENCRYPTED (2400): "+officeguard.DRMMessage, result.Content[0].(mcp.TextContent).Text)
	require.Equal(
		t,
		map[string]any{"error": map[string]any{"code": 2400, "message": officeguard.DRMMessage}},
		result.StructuredContent,
	)
}
