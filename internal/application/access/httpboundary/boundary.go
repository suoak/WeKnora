package httpboundary

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// DocumentReadRoute is a server-owned certification, never request input.
// Authorize runs after the normal Auth middleware, before the handler. Its
// returned closure must revalidate the exact live grants at the output boundary.
// ValidateResponse must reject unknown provenance and uncertified fields.
type DocumentReadRoute struct {
	Authorize        func(*gin.Context) (func() error, error)
	ValidateResponse func([]byte) error
}

type DocumentReadBoundary struct {
	enabled bool
	routes  map[string]DocumentReadRoute
}

func NewDocumentReadBoundary(enabled bool, routes map[string]DocumentReadRoute) *DocumentReadBoundary {
	copy := make(map[string]DocumentReadRoute, len(routes))
	for k, v := range routes {
		copy[k] = v
	}
	return &DocumentReadBoundary{enabled: enabled, routes: copy}
}

const documentReadBoundaryStateKey = "knowhub.document_read_boundary.state"

type documentReadBoundaryState struct {
	route      DocumentReadRoute
	check      func() error
	authorized bool
}

// Outer must be registered before anonymous file/shortlink/embed/MCP routes.
// Enabled mode isolates the entire router: every uncertified route is denied,
// including writes, raw files, Wiki/graph/FAQ/history and streaming endpoints.
// This is intentionally an isolation barrier, not a production enable switch.
func (b *DocumentReadBoundary) Outer() gin.HandlerFunc {
	return func(c *gin.Context) {
		if b == nil || !b.enabled {
			c.Next()
			return
		}
		route, ok := b.routes[c.Request.Method+" "+c.FullPath()]
		if !ok || route.Authorize == nil || route.ValidateResponse == nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "document route HOLD"})
			return
		}
		state := &documentReadBoundaryState{route: route}
		c.Set(documentReadBoundaryStateKey, state)
		original := c.Writer
		buffer := &documentResponseBuffer{ResponseWriter: original, header: original.Header().Clone(), status: http.StatusOK}
		c.Writer = buffer
		defer func() { c.Writer = original }()
		c.Next()
		c.Writer = original
		if !state.authorized || state.check == nil || buffer.failed || c.Request.Context().Err() != nil || state.check() != nil || route.ValidateResponse(buffer.body.Bytes()) != nil {
			original.Header().Set("Cache-Control", "private, no-store")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "document authorization withdrawn or response HOLD"})
			return
		}
		// Do not forward Location/Link/Set-Cookie or storage-related headers.
		// Route certification only covers a bounded JSON body.
		contentType := buffer.header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/json") || buffer.status != http.StatusOK {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "document response type HOLD"})
			return
		}
		original.Header().Set("Content-Type", contentType)
		original.Header().Set("Cache-Control", "private, no-store")
		original.Header().Set("Pragma", "no-cache")
		original.Header().Set("X-Content-Type-Options", "nosniff")
		original.WriteHeader(buffer.status)
		_, _ = original.Write(buffer.body.Bytes())
	}
}

// Inner must be registered immediately after the normal authentication layer.
// Omitting it fails the outer output check; it never implicitly authorizes.
func (b *DocumentReadBoundary) Inner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if b == nil || !b.enabled {
			c.Next()
			return
		}
		value, ok := c.Get(documentReadBoundaryStateKey)
		if !ok {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		state, ok := value.(*documentReadBoundaryState)
		if !ok {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		check, err := state.route.Authorize(c)
		if err != nil || check == nil || check() != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		state.check, state.authorized = check, true
		c.Next()
	}
}

const MaxDocumentResponseBytes = 8 << 20

var errDocumentResponseHOLD = errors.New("document response cannot be safely buffered")

type documentResponseBuffer struct {
	gin.ResponseWriter
	header          http.Header
	body            bytes.Buffer
	status          int
	written, failed bool
}

func (w *documentResponseBuffer) Header() http.Header { return w.header }
func (w *documentResponseBuffer) WriteHeader(code int) {
	if !w.written {
		w.status = code
	}
}
func (w *documentResponseBuffer) WriteHeaderNow() { w.written = true }
func (w *documentResponseBuffer) Write(p []byte) (int, error) {
	w.written = true
	if len(p) > MaxDocumentResponseBytes-w.body.Len() {
		w.failed = true
		return 0, errDocumentResponseHOLD
	}
	return w.body.Write(p)
}
func (w *documentResponseBuffer) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *documentResponseBuffer) Status() int                       { return w.status }
func (w *documentResponseBuffer) Size() int {
	if !w.written {
		return -1
	}
	return w.body.Len()
}
func (w *documentResponseBuffer) Written() bool { return w.written }
func (w *documentResponseBuffer) Flush()        { w.failed = true }
func (w *documentResponseBuffer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.failed = true
	return nil, nil, errDocumentResponseHOLD
}
func (w *documentResponseBuffer) Pusher() http.Pusher { return nil }
