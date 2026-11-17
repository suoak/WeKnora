package core

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/utils"
)

// A server-owned override isolates public Feishu CDN routing from internal
// model/identity DNS. Every returned address still passes the pinned SSRF
// validator. Never replace the process resolver or fall back to an unchecked IP.
func newFeishuDNSResolver() *net.Resolver {
	servers, err := feishuDNSServers(os.Getenv("FEISHU_DNS_SERVERS"))
	if err == nil && len(servers) == 0 {
		return nil
	}
	var next atomic.Uint64
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		if err != nil {
			return nil, err
		}
		server := servers[(next.Add(1)-1)%uint64(len(servers))]
		dialer := net.Dialer{Timeout: 2 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(server, "53"))
	}}
}

func feishuDNSServers(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 2 {
		return nil, errors.New("invalid_feishu_dns_configuration")
	}
	servers := make([]string, 0, len(parts))
	for _, part := range parts {
		ip := net.ParseIP(strings.TrimSpace(part))
		if ip == nil || !utils.IsPublicIP(ip) {
			return nil, errors.New("invalid_feishu_dns_configuration")
		}
		servers = append(servers, ip.String())
	}
	return servers, nil
}
