package datasource

// DocumentFailureError means a known document-local ingest failure was recorded.
// It never authorizes a publication or permits ignoring execution/security fences.
type DocumentFailureError struct {
	Code  string
	Cause error
}

func (e *DocumentFailureError) Error() string { return e.Code }
func (e *DocumentFailureError) Unwrap() error { return e.Cause }
