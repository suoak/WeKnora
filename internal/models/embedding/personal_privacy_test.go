package embedding

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/stretchr/testify/require"
)

type personalPrivacyEmbedder struct{}

func (personalPrivacyEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1}, nil
}

func (personalPrivacyEmbedder) BatchEmbed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1}}, nil
}
func (personalPrivacyEmbedder) GetModelName() string { return "synthetic" }
func (personalPrivacyEmbedder) GetModelID() string   { return "synthetic" }
func (personalPrivacyEmbedder) GetDimensions() int   { return 1 }
func (personalPrivacyEmbedder) BatchEmbedWithPool(
	ctx context.Context, model Embedder, inputs []string,
) ([][]float32, error) {
	return model.BatchEmbed(ctx, inputs)
}

func TestPersonalPayloadNeverReachesEnabledAPMExporter(t *testing.T) {
	var mu sync.Mutex
	var wire bytes.Buffer
	spy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		_, _ = wire.Write(b)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer spy.Close()
	m, err := langfuse.Init(langfuse.Config{
		Enabled: true, Host: spy.URL, PublicKey: "synthetic", SecretKey: "synthetic", FlushAt: 1,
		QueueSize: 16, FlushInterval: time.Millisecond, RequestTimeout: time.Second, SampleRate: 1,
	})
	require.NoError(t, err)
	defer func() { _, _ = langfuse.Init(langfuse.Config{}) }()
	wrapper := &langfuseEmbedder{inner: personalPrivacyEmbedder{}}
	const canary = "SYNTHETIC_P1_PRIVATE_EMBEDDING_CANARY"
	_, err = wrapper.Embed(logger.WithPrivatePayload(context.Background()), canary)
	require.NoError(t, err)
	_, err = wrapper.BatchEmbed(logger.WithPrivatePayload(context.Background()), []string{canary})
	require.NoError(t, err)
	_, err = wrapper.Embed(context.Background(), "non-private-observable-control")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, m.Shutdown(ctx))
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, wire.Bytes(), "enabled exporter must have a positive control")
	require.Contains(t, wire.String(), "non-private-observable-control")
	require.NotContains(t, wire.String(), canary)
}
