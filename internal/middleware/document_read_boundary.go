package middleware

import "github.com/Tencent/WeKnora/internal/application/access/httpboundary"

// The HTTP boundary carries no permission facts. Keeping its transport-only
// implementation independent of middleware avoids a repository import cycle
// in the real Python -> Go ACL integration fixture.
type DocumentReadRoute = httpboundary.DocumentReadRoute
type DocumentReadBoundary = httpboundary.DocumentReadBoundary

var NewDocumentReadBoundary = httpboundary.NewDocumentReadBoundary

const maxDocumentResponseBytes = httpboundary.MaxDocumentResponseBytes
