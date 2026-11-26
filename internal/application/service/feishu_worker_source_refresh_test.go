package service

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

// In-memory DNS and final HTTP wire only. The real OAuth client, safety
// transport and SSRF validation still run; no external connection is made.
// This is not TLS or real Feishu credential/permission acceptance.
func workerOAuthFixtureResolver() *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer func() { _ = server.Close() }()
			var size [2]byte
			if _, err := io.ReadFull(server, size[:]); err != nil {
				return
			}
			wire := make([]byte, binary.BigEndian.Uint16(size[:]))
			if _, err := io.ReadFull(server, wire); err != nil {
				return
			}
			var message dnsmessage.Message
			if message.Unpack(wire) != nil || len(message.Questions) != 1 {
				return
			}
			q := message.Questions[0]
			message.Response, message.RecursionAvailable = true, true
			if q.Type == dnsmessage.TypeA {
				message.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{
					Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 1,
				}, Body: &dnsmessage.AResource{A: [4]byte{93, 184, 216, 34}}}}
			}
			wire, err := message.Pack()
			if err != nil {
				return
			}
			binary.BigEndian.PutUint16(size[:], uint16(len(wire)))
			_, _ = server.Write(append(size[:], wire...))
		}()
		return client, nil
	}}
}

func TestFeishuWorkerIdentityRefreshKeepsClaimAndSSRFChecks(t *testing.T) {
	f := newWorkerSourceIdentityFixture(t)
	work := f.claim(t)
	owner, ok := types.SyncExecutionFromContext(work)
	require.True(t, ok)
	require.NoError(t, f.db.Model(&types.FeishuConnection{}).Where("user_id=?", f.user.ID).
		Update("expires_at", time.Now().Add(-time.Minute)).Error)
	client := core.NewOAuthHTTPClient()
	outer := client.Transport.(feishusafety.Transport)
	ssrf := outer.Base.(*utils.SSRFValidatingRoundTripper)
	ssrf.DNSResolver = workerOAuthFixtureResolver()
	var calls atomic.Int32
	ssrf.Base = feishuTestTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		require.Equal(t, "accounts.feishu.cn", req.URL.Hostname())
		require.Equal(t, "/oauth/v3/token", req.URL.Path)
		require.Nil(t, req.Context().Value(types.PrincipalContextKey))
		actual, exists := types.SyncExecutionFromContext(req.Context())
		require.True(t, exists)
		require.Equal(t, owner, actual)
		identity, outbound := feishusafety.RecoveryOutboundExecution(req.Context())
		require.True(t, outbound)
		require.Equal(t, owner.SyncLogID, identity.Run)
		require.Equal(t, owner.Generation, identity.Generation)
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"access_token":"fixture-user-token",` +
				`"refresh_token":"fixture-rotated-refresh","expires_in":7200,"refresh_token_expires_in":172800}`)),
		}, nil
	})
	f.svc.feishuIdentity.api = client
	bound, err := f.svc.bindFeishuSource(work, f.source)
	require.NoError(t, err)
	require.EqualValues(
		t,
		1,
		calls.Load(),
		"expired access requires a real provider refresh through the safety transport",
	)
	require.GreaterOrEqual(t, f.remote.Load(), int32(2))
	require.Nil(t, bound.Value(types.PrincipalContextKey))
	var connection types.FeishuConnection
	require.NoError(t, f.db.First(&connection, "user_id=?", f.user.ID).Error)
	require.True(t, connection.ExpiresAt.After(time.Now().Add(time.Hour)))
	var token feishuTokens
	require.NoError(t, openFeishu(connection.Tokens, &token))
	require.Equal(t, "fixture-rotated-refresh", token.Refresh)
	// The same actual SSRF wrapper must reject a forbidden target before the
	// inner fixture transport. No whitelist or TLS verification is disabled.
	require.NoError(t, feishusafety.Check(bound), "the runtime/source Guard must allow this live claim first")
	request, err := http.NewRequestWithContext(bound, http.MethodGet, "http://169.254.169.254/latest", nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.ErrorIs(t, err, utils.ErrSSRFRequestRejected)
	require.EqualValues(t, 1, calls.Load(), "SSRF refusal must never reach the inner wire fixture")
}
