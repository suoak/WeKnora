package feishusafety

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDependencySuspensionDrainsActiveHTTPWithoutRelatching(t *testing.T) {
	for _, headers := range []bool{false, true} {
		for _, embedding := range []bool{false, true} {
			t.Run(fmt.Sprintf("headers=%t/embedding=%t", headers, embedding), func(t *testing.T) {
				g, client := fixture(t, Config{})
				InstallDefault(g)
				t.Cleanup(func() { InstallDefault(nil) })
				ctx := WithScope(t.Context(), "source")
				req, err := http.NewRequestWithContext(ctx, http.MethodPost,
					"https://gateway.invalid/embeddings", strings.NewReader(`{"input":["test"]}`))
				require.NoError(t, err)
				started, done := make(chan struct{}), make(chan error, 1)
				go func() {
					response, runErr := (Transport{Base: cancellationTransport{headers, started}, Embedding: embedding}).RoundTrip(req)
					if runErr == nil {
						data, readErr := io.ReadAll(response.Body)
						_ = response.Body.Close()
						if len(data) != 0 {
							done <- fmt.Errorf("unverified response escaped")
							return
						}
						runErr = readErr
					}
					done <- runErr
				}()
				<-started
				g.SuspendDependency(true)
				require.NoError(t, client.Del(ctx, g.PermitKey()).Err())
				select {
				case err := <-done:
					require.ErrorIs(t, err, ErrControlTemporarilyUnavailable)
				case <-time.After(2 * time.Second):
					t.Fatal("request did not drain")
				}
				require.Zero(t, client.Exists(ctx, g.key("trip")).Val())
				require.Zero(t, client.HLen(ctx, g.key("inflight")).Val())
				if embedding {
					require.EqualValues(t, 1, client.ZCard(ctx, g.key("window")).Val(), "retain token accounting")
				}
				_, _, err = ReserveEmbedding(ctx, []string{"test"})
				require.ErrorIs(t, err, ErrControlTemporarilyUnavailable)
				require.Zero(t, client.Exists(ctx, g.key("trip")).Val())
				g.SuspendDependency(false)
				require.Error(t, g.Check(ctx), "clearing the wait flag does not grant a permit")
				require.NoError(t, client.Set(ctx, g.PermitKey(), g.Fingerprint(), time.Second).Err())
				require.NoError(t, g.Check(ctx))
				require.NoError(t, g.Trip(context.Background(), "operator_emergency_stop"))
				g.SuspendDependency(true)
				g.SuspendDependency(false)
				require.ErrorIs(t, g.Check(ctx), ErrClosed)
			})
		}
	}
}
