package core

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
)

const pendingDocumentCursorKey = "feishu_pending_documents_v1"

type documentPlan struct {
	active  map[string]types.SyncDocumentDeferral
	pending map[string]types.SyncDocumentDeferral
}

func prepareDocumentPlan(
	ctx context.Context, cursor *types.SyncCursor, roots []string, h datasource.StreamHandler,
) (*documentPlan, error) {
	p := &documentPlan{
		active: map[string]types.SyncDocumentDeferral{}, pending: map[string]types.SyncDocumentDeferral{},
	}
	if cursor != nil && cursor.ConnectorCursor != nil {
		if raw, exists := cursor.ConnectorCursor[pendingDocumentCursorKey]; exists {
			b, err := json.Marshal(raw)
			if err != nil {
				return nil, err
			}
			if err = json.Unmarshal(b, &p.pending); err != nil || p.pending == nil {
				return nil, errors.New("pending Feishu document cursor unconfirmed")
			}
		}
	}
	if len(p.pending) > 1000 {
		return nil, errors.New("too many pending Feishu documents")
	}
	for key, entry := range p.pending {
		if entry.ExternalID == "" || entry.SourceResourceID == "" || key != entry.Key() {
			return nil, errors.New("pending Feishu document identity unconfirmed")
		}
	}
	if provider, ok := h.(datasource.DocumentDeferralHandler); ok {
		entries, err := provider.DocumentDeferrals(ctx)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, root := range roots {
			allowed[root] = true
		}
		for _, entry := range entries {
			if !allowed[entry.SourceResourceID] || entry.ExternalID == "" || p.active[entry.Key()].ExternalID != "" {
				return nil, errors.New("Feishu document deferral outside configured scope")
			}
			p.active[entry.Key()] = entry
			p.pending[entry.Key()] = entry
		}
	}
	return p, nil
}

func (p *documentPlan) deferred(_ string, externalID string) bool {
	for _, entry := range p.active {
		if entry.ExternalID == externalID {
			return true
		}
	}
	return false
}

func (p *documentPlan) retry(_ string, externalID string) bool {
	for _, entry := range p.pending {
		if entry.ExternalID == externalID {
			return true
		}
	}
	return false
}

func (p *documentPlan) accepted(_ string, externalID string) {
	for key, entry := range p.pending {
		if entry.ExternalID == externalID {
			delete(p.pending, key)
		}
	}
}

func (p *documentPlan) cursor(cursor *types.SyncCursor) *types.SyncCursor {
	if cursor.ConnectorCursor == nil {
		cursor.ConnectorCursor = map[string]interface{}{}
	}
	if len(p.pending) > 0 {
		// Checkpoint handlers may keep snapshots; never share the mutable map.
		snapshot := make(map[string]types.SyncDocumentDeferral, len(p.pending))
		for k, v := range p.pending {
			snapshot[k] = v
		}
		cursor.ConnectorCursor[pendingDocumentCursorKey] = snapshot
	} else {
		delete(cursor.ConnectorCursor, pendingDocumentCursorKey)
	}
	return cursor
}

func observeDocument(h datasource.StreamHandler, root, externalID, disposition string) {
	if observer, ok := h.(datasource.StreamObservationHandler); ok {
		observer.ObserveDocument(root, externalID, disposition)
	}
}
