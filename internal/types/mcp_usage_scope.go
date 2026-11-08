package types

import (
	"context"
	"sync"
)

type mcpUsageScopeKey struct{}

// MCPUsageScope carries only authorized execution IDs; never request arguments.
type MCPUsageScope struct {
	mu      sync.Mutex
	targets map[string]bool
	hits    map[string]bool
}

func WithMCPUsageScope(ctx context.Context) (context.Context, *MCPUsageScope) {
	s := &MCPUsageScope{targets: map[string]bool{}, hits: map[string]bool{}}
	return context.WithValue(ctx, mcpUsageScopeKey{}, s), s
}

func ObserveMCPKnowledgeBase(ctx context.Context, id string) {
	if s, ok := ctx.Value(mcpUsageScopeKey{}).(*MCPUsageScope); ok && id != "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.targets) < 512 {
			s.targets[id] = true
		}
	}
}

func (s *MCPUsageScope) Targets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.targets))
	for id := range s.targets {
		ids = append(ids, id)
	}
	return ids
}

func ObserveMCPHitKnowledgeBase(ctx context.Context, id string) {
	if s, ok := ctx.Value(mcpUsageScopeKey{}).(*MCPUsageScope); ok && id != "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.hits) < 512 {
			s.hits[id] = true
		}
	}
}
func (s *MCPUsageScope) Hits() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.hits))
	for id := range s.hits {
		ids = append(ids, id)
	}
	return ids
}
