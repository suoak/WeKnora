package feishusafety

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestSafetySSRFRejectionRetainsDenyWithoutStoppingSibling(t *testing.T) {
	g, redis := fixture(t, Config{})
	InstallDefault(g)
	t.Cleanup(func() { InstallDefault(nil) })
	ctx := WithScope(context.Background(), "blocked-source")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://169.254.169.254/private", nil)
	require.NoError(t, err)
	base := &ssrfNeverDialTransport{}
	transport := Transport{Base: &utils.SSRFValidatingRoundTripper{Base: base}}
	response, err := transport.RoundTrip(req)
	require.ErrorIs(t, err, utils.ErrSSRFRequestRejected)
	require.Nil(t, response)
	require.Zero(t, base.calls, "blocked addresses must never reach the underlying transport")
	require.NoError(t, g.Check(context.Background()))
	require.Zero(t, redis.Exists(ctx, g.key("trip")).Val())
	sibling, err := http.NewRequestWithContext(
		WithScope(context.Background(), "healthy-sibling"), http.MethodGet, "https://gateway.invalid/public", nil,
	)
	require.NoError(t, err)
	transport.Base = sourceStatusTransport{status: 200}
	response, err = transport.RoundTrip(sibling)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	require.NoError(t, response.Body.Close())
}

func TestSafetySSRFRejectionCannotHideUnhealthyRuntimeOrEmbedding(t *testing.T) {
	for _, embedding := range []bool{false, true} {
		t.Run(fmt.Sprint(embedding), func(t *testing.T) {
			g, redis := fixture(t, Config{})
			if !embedding {
				require.NoError(t, redis.Del(context.Background(), g.PermitKey()).Err())
			}
			tripRequestFailure(WithScope(context.Background(), "blocked-source"), g,
				fmt.Errorf("validated rejection: %w", utils.ErrSSRFRequestRejected), embedding, "headers")
			require.Error(t, g.Check(context.Background()))
			require.Positive(t, redis.Exists(context.Background(), g.key("trip")).Val())
		})
	}
	// Text from an arbitrary I/O error cannot mint the typed pre-connect refusal.
	g, redis := fixture(t, Config{})
	tripRequestFailure(WithScope(context.Background(), "source"), g,
		errors.New(utils.ErrSSRFRequestRejected.Error()), false, "headers")
	require.Positive(t, redis.Exists(context.Background(), g.key("trip")).Val())
}

type ssrfNeverDialTransport struct{ calls int }

func (t *ssrfNeverDialTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("forbidden network request")
}
