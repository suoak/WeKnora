package types

import "errors"

// ErrTenantAPIKeyNotFound is shared by persistence and live scope consumers.
// It exposes no credential or underlying SQL/query details.
var ErrTenantAPIKeyNotFound = errors.New("tenant api key not found")
