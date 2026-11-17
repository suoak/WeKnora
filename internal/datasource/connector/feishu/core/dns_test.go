package core

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeishuDNSOverrideRejectsInvalidConfigurationWithoutDialing(t *testing.T) {
	for _, raw := range []string{" ", "resolver.example", "127.0.0.1", "10.0.0.1", "169.254.169.254",
		"::1", "223.5.5.5,", "223.5.5.5:53", "223.5.5.5,223.6.6.6,8.8.8.8"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("FEISHU_DNS_SERVERS", raw)
			resolver := newFeishuDNSResolver()
			require.NotNil(t, resolver)
			conn, err := resolver.Dial(context.Background(), "udp", "ignored:53")
			require.Nil(t, conn)
			require.EqualError(t, err, "invalid_feishu_dns_configuration")
		})
	}
	t.Setenv("FEISHU_DNS_SERVERS", "")
	require.Nil(t, newFeishuDNSResolver(), "default clients retain the process resolver")
	servers, err := feishuDNSServers("223.5.5.5, 223.6.6.6")
	require.NoError(t, err)
	require.Equal(t, []string{"223.5.5.5", "223.6.6.6"}, servers)
	require.NotNil(t, net.ParseIP(servers[0]))
}
