package session

import (
	"time"

	"github.com/gin-gonic/gin"
)

const sseKeepAliveInterval = 15 * time.Second

// Track actual response writes, including resource-rewriter holdback. Comment
// frames keep an idle connection alive without inventing a model event or
// changing persisted completion, source authorization or the generation budget.
type sseKeepAlive struct {
	lastWrite time.Time
	lastSize  int
}

func newSSEKeepAlive(c *gin.Context) *sseKeepAlive {
	return &sseKeepAlive{lastWrite: time.Now(), lastSize: c.Writer.Size()}
}

// pulse runs on the existing handler polling goroutine. Never introduce a
// concurrent ResponseWriter or keep writing after a disconnect/write failure.
func (s *sseKeepAlive) pulse(c *gin.Context, now time.Time) bool {
	if c.Request.Context().Err() != nil {
		return false
	}
	if size := c.Writer.Size(); size != s.lastSize {
		s.lastSize, s.lastWrite = size, now
	}
	if now.Sub(s.lastWrite) < sseKeepAliveInterval {
		return true
	}
	if _, err := c.Writer.WriteString(": keep-alive\n\n"); err != nil {
		return false
	}
	c.Writer.Flush()
	s.lastSize, s.lastWrite = c.Writer.Size(), now
	return true
}
