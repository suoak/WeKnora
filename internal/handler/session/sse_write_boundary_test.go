package session

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/storageurl"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Model the live source boundary: an authorized prefix is flushed, then a
// source is denied. Both write APIs must retain the same fail-closed behavior.
type withdrawnSSEWriter struct {
	gin.ResponseWriter
	denied         bool
	deniedAttempts int
}

func (w *withdrawnSSEWriter) Write(body []byte) (int, error) {
	if w.denied {
		w.deniedAttempts++
		return 0, types.ErrLegacySourceRestricted
	}
	return w.ResponseWriter.Write(body)
}

func (w *withdrawnSSEWriter) WriteString(body string) (int, error) {
	return w.Write([]byte(body))
}

func (w *withdrawnSSEWriter) Flush() {
	w.ResponseWriter.Flush()
	w.denied = true
}

func TestSSEWriteBoundaryStopsLiveAndReplayOnSourceDenial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"live", "replay", "replay-poll"} {
		t.Run(mode, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest("GET", "/sessions/continue-stream/sess1?message_id=msg1", nil).
				WithContext(ctx)
			c.Params = gin.Params{{Key: "session_id", Value: "sess1"}}
			writer := &withdrawnSSEWriter{ResponseWriter: c.Writer}
			c.Writer = writer
			events := []interfaces.StreamEvent{
				{ID: "first", Type: types.ResponseTypeAnswer, Content: "authorized prefix"},
				{ID: "second", Type: types.ResponseTypeAnswer, Content: "withdrawn-sensitive-canary"},
			}
			h := &Handler{
				sessionService: &stubSessionService{}, messageService: &stubMessageServiceForStream{},
				streamManager: &stubStreamManager{events: events}, fileService: &stubResourceFileService{},
			}
			if mode == "replay-poll" {
				h.streamManager = &growingStreamManager{
					stubStreamManager: stubStreamManager{events: events}, visible: 1,
				}
			}
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				if mode == "live" {
					h.handleAgentEventsForSSE(ctx, c, "sess1", "msg1", "request", nil, false,
						storageurl.NewStreamRewriter(nil))
				} else {
					h.ContinueStream(c)
				}
			}()
			select {
			case <-finished:
			case <-time.After(time.Second):
				cancel()
				<-finished
				t.Fatal("source-denied stream kept polling after delivery failed")
			}
			require.Equal(t, 1, writer.deniedAttempts, "stop at the first failed frame")
			require.Contains(t, recorder.Body.String(), "authorized prefix")
			require.NotContains(t, recorder.Body.String(), "withdrawn-sensitive-canary")
			require.NotContains(t, recorder.Body.String(), `"response_type":"complete"`)
			require.True(t, c.IsAborted())
			require.ErrorIs(t, c.Errors.Last(), types.ErrLegacySourceRestricted)
		})
	}
}

func TestSSEWriteBoundaryPreservesGinWireFraming(t *testing.T) {
	response := &types.StreamResponse{
		ID: "request", ResponseType: types.ResponseTypeAnswer, Content: "中文\n<technical>&\"quoted\"",
		Data: map[string]interface{}{"event_id": "answer"},
	}
	legacy := httptest.NewRecorder()
	old, _ := gin.CreateTestContext(legacy)
	setSSEHeaders(old)
	old.SSEvent("message", response)
	old.Writer.Flush()
	current := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(current)
	c.Request = httptest.NewRequest("GET", "/owned-stream", nil)
	setSSEHeaders(c)
	require.True(t, writeStreamResponse(c, response))
	require.Equal(t, legacy.Body.String(), current.Body.String())
	require.Equal(t, legacy.Header(), current.Header())
	require.True(t, strings.HasSuffix(current.Body.String(), "\n\n"))
}

type shortSSEWriter struct{ gin.ResponseWriter }

func (w shortSSEWriter) Write(body []byte) (int, error) { return len(body) - 1, nil }

func TestSSEWriteBoundaryRejectsShortWritesAndInvalidPayload(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("GET", "/owned-stream", nil)
		response := &types.StreamResponse{ResponseType: types.ResponseTypeComplete, Done: true}
		if invalid {
			response.Data = map[string]interface{}{"unsupported": make(chan int)}
		} else {
			c.Writer = shortSSEWriter{c.Writer}
		}
		require.False(t, writeStreamResponse(c, response))
		require.True(t, c.IsAborted())
		require.False(t, recorder.Flushed)
		require.Empty(t, recorder.Body.String())
		if !invalid {
			require.ErrorIs(t, c.Errors.Last(), io.ErrShortWrite)
		}
	}
}
