package types

// DocumentOperation is deliberately separate from management and KB membership.
type DocumentOperation string

const (
	DocumentRead   DocumentOperation = "read"
	DocumentSearch DocumentOperation = "search"
	DocumentFile   DocumentOperation = "file"
)

func (o DocumentOperation) Valid() bool {
	return o == DocumentRead || o == DocumentSearch || o == DocumentFile
}

// DocumentSubjectGrant is an explicit user entitlement, bounded by the current
// approved resource version. It never replaces the existing KB or Key checks.
type DocumentSubjectGrant struct {
	KnowledgeID     string
	UserID          string
	Operation       DocumentOperation
	State           string
	Revision        int64
	ResourceVersion string
}

type DocumentGrantChange struct {
	DocumentSubjectGrant
	ActorID string
	Reason  string
}
