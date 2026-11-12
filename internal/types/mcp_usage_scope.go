package types

import (
	"context"
	"sync"
)

// MaxMCPUsageResources bounds metadata without truncating supported large Portal scopes.
const MaxMCPUsageResources = 4096

// MCPUsageScope carries only authorized execution IDs; never request arguments.
type MCPUsageScope struct {
	mu      sync.Mutex
	targets map[string]bool
	hits    map[string]bool
}

// WithMCPUsageScope attaches a shared observation scope to an authenticated invocation.
func WithMCPUsageScope(ctx context.Context) (context.Context, *MCPUsageScope) {
	s := &MCPUsageScope{targets: map[string]bool{}, hits: map[string]bool{}}
	return context.WithValue(ctx, MCPUsageScopeContextKey, s), s
}

// ObserveMCPKnowledgeBase records a knowledge base after execution authorization.
func ObserveMCPKnowledgeBase(ctx context.Context, id string) {
	if s, ok := ctx.Value(MCPUsageScopeContextKey).(*MCPUsageScope); ok && id != "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.targets) < MaxMCPUsageResources {
			s.targets[id] = true
		}
	}
}

// Targets returns distinct authorized execution targets.
func (s *MCPUsageScope) Targets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.targets))
	for id := range s.targets {
		ids = append(ids, id)
	}
	return ids
}

// ObserveMCPHitKnowledgeBase records a knowledge base present in actual results.
func ObserveMCPHitKnowledgeBase(ctx context.Context, id string) {
	if s, ok := ctx.Value(MCPUsageScopeContextKey).(*MCPUsageScope); ok && id != "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.hits) < MaxMCPUsageResources {
			s.hits[id] = true
		}
	}
}

// Hits returns distinct knowledge bases observed in results.
func (s *MCPUsageScope) Hits() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.hits))
	for id := range s.hits {
		ids = append(ids, id)
	}
	return ids
}
