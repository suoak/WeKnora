package mcpserver

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/mark3labs/mcp-go/mcp"
)

type (
	contentUsageErrorKey struct{}
	contentUsageError    struct{ code string }
)

// Classify the original cause without retaining sensitive business error text.
func contentFailure(ctx context.Context, cause error, message string) *mcp.CallToolResult {
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
	if state, ok := ctx.Value(contentUsageErrorKey{}).(*contentUsageError); ok {
		state.code = code
	}
	return mcp.NewToolResultError(message)
}
