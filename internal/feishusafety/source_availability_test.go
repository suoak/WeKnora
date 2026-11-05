package feishusafety

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafetySourceAvailabilityFaultKeepsSiblingAdmission(t *testing.T) {
	for _, model := range []bool{false, true} {
		for _, status := range []int{408, 429, 500, 502, 503, 504, 599} {
			t.Run(fmt.Sprintf("model%t/status%d", model, status), func(t *testing.T) {
				g, redis := fixture(t, Config{})
				InstallDefault(g)
				t.Cleanup(func() { InstallDefault(nil) })
				ctx := WithScope(context.Background(), "failing-source")
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gateway.invalid/doc", nil)
				require.NoError(t, err)
				transport := Transport{Base: sourceStatusTransport{status: status}, BackgroundModel: model}
				response, err := transport.RoundTrip(req)
				require.NoError(t, err)
				require.Equal(t, status, response.StatusCode)
				_, err = io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.NoError(t, g.Check(context.Background()))
				require.Zero(t, redis.Exists(ctx, g.key("trip"), "asynq:{sync}:paused").Val())
				require.Zero(t, redis.HLen(ctx, g.key("inflight")).Val())
				sibling := req.Clone(WithScope(context.Background(), "healthy-sibling"))
				transport.Base = sourceStatusTransport{status: 200}
				response, err = transport.RoundTrip(sibling)
				require.NoError(t, err)
				require.Equal(t, 200, response.StatusCode)
				require.NoError(t, response.Body.Close())
			})
		}
	}
}
