package logger

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gormlog "gorm.io/gorm/logger"
)

func TestPrivatePayloadCannotEnterDiagnosticsOrDetachedDebug(t *testing.T) {
	var out bytes.Buffer
	old := appLogger.Out
	SetOutput(&out)
	defer SetOutput(old)
	priorEnabled, priorDir := llmDebug.enabled, llmDebug.dir
	llmDebug.enabled, llmDebug.dir = true, t.TempDir()
	defer func() { llmDebug.enabled, llmDebug.dir = priorEnabled, priorDir }()
	ctx := WithPrivatePayload(context.Background())
	for _, c := range []context.Context{ctx, CloneContext(ctx), CloneContextWithoutTrace(ctx)} {
		require.True(t, PrivatePayload(c))
		Infof(c, "SYNTHETIC_QUERY_FILE_KEY_SIGNED_URL_CANARY")
		LLMDebugLog(c, &LLMCallRecord{
			CallType: "Embedding",
			Sections: []RecordSection{{Title: "Input", Content: "SYNTHETIC_QUERY_FILE_KEY_SIGNED_URL_CANARY"}},
		})
	}
	require.Empty(t, out.String())
	files, err := os.ReadDir(llmDebug.dir)
	require.NoError(t, err)
	require.Empty(t, files)
	Infof(context.Background(), "safe metadata remains observable")
	require.Contains(t, out.String(), "safe metadata remains observable")
	require.True(t, IsolatedPersonalPayload(CloneContext(WithIsolatedPersonalPayload(ctx))))
	require.True(t, IsolatedPersonalPayload(CloneContextWithoutTrace(WithIsolatedPersonalPayload(ctx))))
}

func TestPrivatePayloadSQLNeverFormatsBoundArguments(t *testing.T) {
	l := WithPrivateGORMPayload(gormlog.Default).LogMode(gormlog.Info)
	formatted := false
	l.Trace(WithPrivatePayload(context.Background()), time.Now(), func() (string, int64) {
		formatted = true
		return "SYNTHETIC_SQL_PAYLOAD_CANARY", 0
	}, nil)
	require.False(t, formatted)
}
