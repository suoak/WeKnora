package chatpipeline

import (
	"context"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

type verifiedRAGConfigurationKey struct{}

// Request-local evidence of server-resolved templates and the query-understand
// stage. It attests configuration only, never document content or permission.
type verifiedRAGConfiguration struct {
	mu                                               sync.Mutex
	session, user, query, prompt, template, language string
	rewrite                                          string
	intent                                           types.QueryIntent
	recorded                                         bool
}

// WithVerifiedRAGConfiguration is called after the service resolves the agent
// and workspace configuration. HTTP payloads cannot supply this context value.
func WithVerifiedRAGConfiguration(ctx context.Context, request *types.ChatManage) context.Context {
	return context.WithValue(ctx, verifiedRAGConfigurationKey{}, &verifiedRAGConfiguration{
		session: request.SessionID, user: request.UserID, query: request.Query,
		prompt: request.SummaryConfig.Prompt, template: request.SummaryConfig.ContextTemplate,
		language: request.Language,
	})
}

// Only the query-understand plugin seals the effective rewrite before invoking
// downstream stages. A caller changing a ChatManage field is not a rewrite proof.
func recordVerifiedQueryRewrite(ctx context.Context, request *types.ChatManage) {
	proof, ok := ctx.Value(verifiedRAGConfigurationKey{}).(*verifiedRAGConfiguration)
	if !ok {
		return
	}
	proof.mu.Lock()
	defer proof.mu.Unlock()
	if !proof.matches(request) || proof.recorded {
		return
	}
	proof.rewrite, proof.intent, proof.recorded = request.RewriteQuery, request.Intent, true
}

func (p *verifiedRAGConfiguration) matches(request *types.ChatManage) bool {
	return request != nil && p.session == request.SessionID && p.user == request.UserID &&
		p.query == request.Query && p.prompt == request.SummaryConfig.Prompt &&
		p.template == request.SummaryConfig.ContextTemplate && p.language == request.Language
}

// HasVerifiedRAGConfiguration does not bypass canonical message reconstruction;
// it permits only the exact configuration and rewrite sealed for this request.
func HasVerifiedRAGConfiguration(ctx context.Context, request *types.ChatManage) bool {
	proof, ok := ctx.Value(verifiedRAGConfigurationKey{}).(*verifiedRAGConfiguration)
	if !ok {
		return false
	}
	proof.mu.Lock()
	defer proof.mu.Unlock()
	return proof.recorded && proof.matches(request) && proof.rewrite == request.RewriteQuery && proof.intent == request.Intent
}
