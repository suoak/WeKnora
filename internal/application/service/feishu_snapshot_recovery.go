package service

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// Only the in-process live connector observation can issue a recovery candidate.
// The error retains the original version fence and never serializes source bytes.
type feishuSnapshotChangedError struct {
	cause   error
	current *types.FetchedItem
}

func (e *feishuSnapshotChangedError) Error() string { return e.cause.Error() }
func (e *feishuSnapshotChangedError) Unwrap() error { return e.cause }

// Both the pre-parse proof and final pre-publication check carry the actual
// live observation. A bare version fence must never invent a recovery identity.
func snapshotObservationError(err error, current *types.FetchedItem) error {
	var fence *repository.LegacyFenceError
	if errors.As(err, &fence) && fence.Reason == "latest_source_snapshot_changed" {
		return &feishuSnapshotChangedError{cause: err, current: current}
	}
	return err
}

func verifyFinalFeishuSnapshot(ctx context.Context, item *types.FetchedItem) error {
	if item == nil || item.CurrentSnapshot == nil {
		return ErrFeishuStagingRequired
	}
	fresh, err := item.CurrentSnapshot(ctx)
	if err != nil {
		return err
	}
	return snapshotObservationError(repository.MatchLatestFetchedSnapshot(item, fresh), fresh)
}

// recoverFeishuSnapshot retries one document once, with a freshly observed,
// identically bound snapshot. Every storage/parser/permission/publication check
// runs again. Continued edits become a deferred document, not an entire-run loop.
func recoverFeishuSnapshot(
	ctx context.Context, item *types.FetchedItem,
	stage func(*types.FetchedItem) (*types.FeishuLegacyReceipt, error),
) (*types.FeishuLegacyReceipt, error) {
	if item == nil {
		return nil, ErrFeishuStagingRequired
	}
	item.AutoRecovered = false
	receipt, err := stage(item)
	var changed *feishuSnapshotChangedError
	if receipt != nil || !errors.As(err, &changed) || item.CurrentSnapshot == nil ||
		!sameSnapshotRecoveryBinding(item, changed.current) {
		return receipt, err
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	candidate := *changed.current
	candidate.Metadata = maps.Clone(candidate.Metadata)
	candidate.SubtreeKeep = slices.Clone(candidate.SubtreeKeep)
	candidate.CurrentSnapshot = item.CurrentSnapshot
	candidate.AutoRecovered = false
	receipt, err = stage(&candidate)
	var duplicate *types.DuplicateKnowledgeError
	if legacyReceiptConfirmed(receipt) && (err == nil || errors.As(err, &duplicate)) {
		// A duplicate receipt is also a durable acceptance. Never substitute
		// the new body, revision or recovery counter after a failed publication.
		candidate.AutoRecovered = true
		*item = candidate
	}
	return receipt, err
}

func sameSnapshotRecoveryBinding(original, current *types.FetchedItem) bool {
	if !sameSnapshotSourceBinding(original, current) || original.ReplacesSubtree != current.ReplacesSubtree ||
		len(original.SubtreeKeep) != 0 || len(current.SubtreeKeep) != 0 {
		return false
	}
	// External children were emitted from the original inventory. They need a
	// complete new traversal; replacing only their parent cannot certify bytes.
	return true
}

func sameSnapshotSourceBinding(original, current *types.FetchedItem) bool {
	if original == nil || current == nil || current.IsDeleted || original.IsDeleted ||
		len(current.Content) == 0 || original.ExternalID == "" || original.ExternalID != current.ExternalID ||
		original.SourceResourceID != current.SourceResourceID || original.URL != current.URL ||
		getFileType(original.FileName) != getFileType(current.FileName) ||
		current.UpdatedAt.Before(original.UpdatedAt) ||
		original.Metadata["attachment"] == "true" || current.Metadata["attachment"] == "true" {
		return false
	}
	for _, key := range []string{"obj_type", "obj_token", "node_token", "space_id"} {
		if original.Metadata[key] != current.Metadata[key] {
			return false
		}
	}
	return true
}
