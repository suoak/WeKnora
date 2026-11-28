package session

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func keepAliveFixture(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *sseKeepAlive, time.Time) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/owned-stream", nil)
	setSSEHeaders(c)
	start := time.Unix(100, 0)
	return c, recorder, &sseKeepAlive{lastWrite: start, lastSize: c.Writer.Size()}, start
}

func TestSSEKeepAliveCommentsDoNotInventAnswerOrCompletion(t *testing.T) {
	c, recorder, keepAlive, start := keepAliveFixture(t)
	require.True(t, keepAlive.pulse(c, start.Add(14*time.Second)))
	require.Empty(t, recorder.Body.String())
	require.True(t, keepAlive.pulse(c, start.Add(15*time.Second)))
	require.Equal(t, ": keep-alive\n\n", recorder.Body.String())
	require.True(t, recorder.Flushed)
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	require.NotContains(t, recorder.Body.String(), "data:")
	require.NotContains(t, recorder.Body.String(), "done")
	require.NotContains(t, recorder.Body.String(), "complete")
	require.True(t, keepAlive.pulse(c, start.Add(29*time.Second)))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), ": keep-alive"))
	require.True(t, keepAlive.pulse(c, start.Add(30*time.Second)))
	require.Equal(t, 2, strings.Count(recorder.Body.String(), ": keep-alive"))
}

func TestSSEKeepAliveResetsOnlyWhenResponseBytesAreWritten(t *testing.T) {
	c, recorder, keepAlive, start := keepAliveFixture(t)
	c.SSEvent("message", gin.H{"response_type": "answer", "content": "verified technical fact", "done": false})
	c.Writer.Flush()
	written := recorder.Body.String()
	require.True(t, keepAlive.pulse(c, start.Add(14*time.Second)))
	require.True(t, keepAlive.pulse(c, start.Add(28*time.Second)))
	require.Equal(t, written, recorder.Body.String(), "active response data resets the idle interval")
	require.True(t, keepAlive.pulse(c, start.Add(29*time.Second)))
	require.Equal(t, written+": keep-alive\n\n", recorder.Body.String())
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "data:"))
}

type failedKeepAliveWriter struct{ gin.ResponseWriter }

func (w failedKeepAliveWriter) WriteString(string) (int, error) { return 0, io.ErrClosedPipe }

func TestSSEKeepAliveStopsOnDisconnectAndWriteFailure(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		c, recorder, keepAlive, start := keepAliveFixture(t)
		if cancelled {
			ctx, cancel := context.WithCancel(c.Request.Context())
			cancel()
			c.Request = c.Request.WithContext(ctx)
		} else {
			c.Writer = failedKeepAliveWriter{c.Writer}
		}
		require.False(t, keepAlive.pulse(c, start.Add(15*time.Second)))
		require.Empty(t, recorder.Body.String())
	}
}
