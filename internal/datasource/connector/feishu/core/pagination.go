package core

import "fmt"

// ContentTruncatedError distinguishes a bounded read from a complete document.
// Callers must preserve the published version when a candidate is truncated.
type ContentTruncatedError struct {
	Resource string
	Limit    int
}

func (e *ContentTruncatedError) Error() string {
	return fmt.Sprintf("feishu %s content truncated at limit %d", e.Resource, e.Limit)
}

// IncompletePaginationError marks a protocol violation, never an end-of-list.
type IncompletePaginationError struct{ Resource, Reason string }

func (e *IncompletePaginationError) Error() string {
	return fmt.Sprintf("feishu %s pagination %s", e.Resource, e.Reason)
}

// maxBitableFields bounds schema discovery independently of record pagination.
const maxBitableFields = 5000

func validateContentPage(resource string, hasMore bool, next string, count int, seen map[string]struct{}) error {
	if !hasMore {
		return nil
	}
	if count == 0 {
		return &IncompletePaginationError{resource, "empty non-final page"}
	}
	if next == "" {
		return &IncompletePaginationError{resource, "missing page token while has_more=true"}
	}
	if _, exists := seen[next]; exists {
		return &IncompletePaginationError{resource, "repeated page token"}
	}
	seen[next] = struct{}{}
	return nil
}
