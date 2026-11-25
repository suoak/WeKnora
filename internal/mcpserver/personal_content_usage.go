package mcpserver

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/officeguard"
	"github.com/mark3labs/mcp-go/mcp"
)

type (
	contentUsageErrorKey struct{}
	contentUsageError    struct{ code string }
)

// Classify the original cause without retaining sensitive business error text.
func contentFailure(ctx context.Context, cause error, message string) *mcp.CallToolResult {
	var appErr *werrors.AppError
	if errors.As(cause, &appErr) && appErr.Code == werrors.ErrOfficeEncrypted {
		// Only allow known constant messages. Business errors can contain paths or
		// source details; never forward arbitrary error text into the MCP result.
		message := officeguard.Message
		if appErr.Message == officeguard.DRMMessage {
			message = officeguard.DRMMessage
		}
		result := usageFailure(ctx, "invalid_arguments", "OFFICE_ENCRYPTED (2400): "+message)
		result.StructuredContent = map[string]any{"error": map[string]any{
			"code": int(werrors.ErrOfficeEncrypted), "message": message,
		}}
		return result
	}
	code := "execution_error"
	var timeout interface{ Timeout() bool }
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		code = "timeout"
	case errors.Is(cause, context.Canceled):
		code = "canceled"
	case errors.As(cause, &timeout) && timeout.Timeout():
		code = "timeout"
	case errors.Is(cause, access.ErrForbidden):
		code = "permission_denied"
	case errors.Is(cause, service.ErrPersonalUpload),
		errors.Is(cause, service.ErrPersonalContentConflict),
		errors.Is(cause, service.ErrPersonalContentArguments):
		code = "invalid_arguments"
	}
	return usageFailure(ctx, code, message)
}

// Attach a controlled accounting category without changing the public result.
func usageFailure(ctx context.Context, code, message string) *mcp.CallToolResult {
	if state, ok := ctx.Value(contentUsageErrorKey{}).(*contentUsageError); ok {
		state.code = code
	}
	return mcp.NewToolResultError(message)
}
