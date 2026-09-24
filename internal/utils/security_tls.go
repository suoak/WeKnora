package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	ssrfTLSMaxConcurrency     = 3
	ssrfTLSStaggerDelay       = 200 * time.Millisecond
	ssrfTLSCandidateTimeout   = 5 * time.Second
	ssrfTLSOverallDialTimeout = 10 * time.Second
)

type ssrfTransportDependencies struct {
	lookupIPAddr     func(context.Context, string) ([]net.IPAddr, error)
	dialContext      func(context.Context, string, string) (net.Conn, error)
	maxConcurrency   int
	staggerDelay     time.Duration
	candidateTimeout time.Duration
	overallTimeout   time.Duration
	logf             func(string, ...interface{})
}

func defaultSSRFTransportDependencies() ssrfTransportDependencies {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return ssrfTransportDependencies{
		lookupIPAddr:     net.DefaultResolver.LookupIPAddr,
		dialContext:      dialer.DialContext,
		maxConcurrency:   ssrfTLSMaxConcurrency,
		staggerDelay:     ssrfTLSStaggerDelay,
		candidateTimeout: ssrfTLSCandidateTimeout,
		overallTimeout:   ssrfTLSOverallDialTimeout,
		logf:             log.Printf,
	}
}

// newSSRFSafeTransportWithDependencies exists so the complete HTTP/HTTPS
// transport behavior can be tested without public DNS or unroutable networks.
// DialTLSContext is only used by net/http for non-proxied HTTPS. Proxied HTTPS
// continues through DialContext + CONNECT using the standard Transport path.
func newSSRFSafeTransportWithDependencies(
	config SSRFSafeHTTPClientConfig,
	deps ssrfTransportDependencies,
) *http.Transport {
	deps = normalizeSSRFTransportDependencies(deps)
	transport := &http.Transport{
		DisableKeepAlives:  config.DisableKeepAlives,
		DisableCompression: config.DisableCompression,
	}

	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %s: %w", addr, err)
		}
		if IsSystemProxy(addr) || IsSSRFWhitelisted(host) {
			return deps.dialContext(ctx, network, addr)
		}
		return ssrfSafeDialContextWithDeps(ctx, network, addr, ssrfDialDependencies{
			lookupIPAddr: deps.lookupIPAddr,
			dialContext:  deps.dialContext,
			perIPTimeout: ssrfDialAttemptTimeout,
		})
	}

	if !config.EnableTLSFallback {
		return transport
	}

	transport.TLSClientConfig = &tls.Config{}
	transport.ForceAttemptHTTP2 = true
	// Read TLSClientConfig at invocation time. net/http performs its HTTP/2
	// initialization before dialing and may add ALPN protocols to this config;
	// capturing an earlier clone here would silently disable h2 negotiation.
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return ssrfSafeDialTLSContextWithDeps(ctx, network, addr, func() *tls.Config {
			return transport.TLSClientConfig
		}, deps)
	}
	return transport
}

func normalizeSSRFTransportDependencies(deps ssrfTransportDependencies) ssrfTransportDependencies {
	defaults := defaultSSRFTransportDependencies()
	if deps.lookupIPAddr == nil {
		deps.lookupIPAddr = defaults.lookupIPAddr
	}
	if deps.dialContext == nil {
		deps.dialContext = defaults.dialContext
	}
	if deps.maxConcurrency <= 0 {
		deps.maxConcurrency = defaults.maxConcurrency
	}
	if deps.staggerDelay <= 0 {
		deps.staggerDelay = defaults.staggerDelay
	}
	if deps.candidateTimeout <= 0 {
		deps.candidateTimeout = defaults.candidateTimeout
	}
	if deps.overallTimeout <= 0 {
		deps.overallTimeout = defaults.overallTimeout
	}
	if deps.logf == nil {
		deps.logf = func(string, ...interface{}) {}
	}
	return deps
}

func ssrfSafeDialTLSContextWithDeps(
	ctx context.Context,
	network string,
	addr string,
	tlsConfig func() *tls.Config,
	deps ssrfTransportDependencies,
) (net.Conn, error) {
	deps = normalizeSSRFTransportDependencies(deps)
	started := time.Now()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	baseTLSConfig := cloneTLSConfig(tlsConfig)
	if baseTLSConfig.InsecureSkipVerify {
		return nil, fmt.Errorf("TLS connection blocked for %s: certificate verification must remain enabled", host)
	}
	baseTLSConfig.ServerName = host
	overallCtx, cancelOverall := context.WithTimeout(ctx, deps.overallTimeout)
	defer cancelOverall()

	// Preserve the pre-existing explicit whitelist/system-proxy behavior. This
	// path is not used for Feishu and does not add any new whitelist exception.
	if IsSystemProxy(addr) || IsSSRFWhitelisted(host) {
		directCtx, cancel := context.WithTimeout(overallCtx, deps.candidateTimeout)
		defer cancel()
		conn, result := dialAndHandshakeTLS(
			directCtx, network, addr, host, baseTLSConfig, deps.dialContext,
		)
		if result.err != nil {
			return nil, fmt.Errorf("HTTPS connection failed for %s during %s: %w", host, result.phase, result.err)
		}
		return conn, nil
	}

	target, err := resolveAndValidateSSRFTarget(overallCtx, addr, deps.lookupIPAddr)
	if err != nil {
		if deadlineErr := overallCtx.Err(); deadlineErr != nil {
			deps.logf(
				"[ssrf-tls] hostname=%s phase=dns total_elapsed=%s category=%s attempted=0 total=0",
				host, time.Since(started), classifyTLSCandidateError(deadlineErr),
			)
			return nil, fmt.Errorf(
				"HTTPS connection failed for %s: attempted=0 total=0: %w", host, deadlineErr,
			)
		}
		return nil, err
	}
	return raceValidatedTLSCandidates(overallCtx, network, target, baseTLSConfig, deps, started)
}

func cloneTLSConfig(provider func() *tls.Config) *tls.Config {
	if provider == nil {
		return &tls.Config{}
	}
	if config := provider(); config != nil {
		return config.Clone()
	}
	return &tls.Config{}
}

type tlsCandidateResult struct {
	conn     net.Conn
	ip       string
	phase    string
	duration time.Duration
	err      error
}

func raceValidatedTLSCandidates(
	ctx context.Context,
	network string,
	target ssrfValidatedTarget,
	baseTLSConfig *tls.Config,
	deps ssrfTransportDependencies,
	started time.Time,
) (net.Conn, error) {
	total := len(target.ips)
	overallCtx, cancelOverall := context.WithTimeout(ctx, deps.overallTimeout)
	defer cancelOverall()

	results := make(chan tlsCandidateResult, total)
	next := 0
	active := 0
	attempted := 0
	var lastErr error
	deps.logf(
		"[ssrf-tls] hostname=%s candidate_count=%d max_concurrency=%d overall_budget=%s",
		target.host, total, deps.maxConcurrency, deps.overallTimeout,
	)

	launch := func() {
		ip := target.ips[next].IP.String()
		next++
		active++
		attempted++
		go func() {
			candidateCtx, cancel := context.WithTimeout(overallCtx, deps.candidateTimeout)
			defer cancel()
			pinnedAddr := net.JoinHostPort(ip, target.port)
			candidateTLSConfig := baseTLSConfig.Clone()
			candidateTLSConfig.ServerName = target.host
			conn, result := dialAndHandshakeTLS(
				candidateCtx, network, pinnedAddr, ip, candidateTLSConfig, deps.dialContext,
			)
			result.ip = ip
			result.conn = conn
			results <- result
		}()
	}

	launch()
	timer := time.NewTimer(deps.staggerDelay)
	defer timer.Stop()

	resetTimer := func() {
		if next >= total || active >= deps.maxConcurrency {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(deps.staggerDelay)
	}

	drainLosers := func(winner net.Conn) {
		cancelOverall()
		for active > 0 {
			result := <-results
			active--
			if result.conn != nil && result.conn != winner {
				_ = result.conn.Close()
			}
		}
	}

	for {
		select {
		case result := <-results:
			active--
			category := classifyTLSCandidateError(result.err)
			if result.err == nil {
				deps.logf(
					"[ssrf-tls] hostname=%s candidate_ip=%s phase=tls duration=%s category=success attempted=%d total=%d",
					target.host, result.ip, result.duration, attempted, total,
				)
				drainLosers(result.conn)
				deps.logf(
					"[ssrf-tls] hostname=%s winning_candidate=%s total_elapsed=%s attempted=%d total=%d",
					target.host, result.ip, time.Since(started), attempted, total,
				)
				return result.conn, nil
			}

			lastErr = result.err
			deps.logf(
				"[ssrf-tls] hostname=%s candidate_ip=%s phase=%s duration=%s category=%s attempted=%d total=%d",
				target.host, result.ip, result.phase, result.duration, category, attempted, total,
			)
			if next < total && active < deps.maxConcurrency {
				launch()
				resetTimer()
			}
			if active == 0 && next >= total {
				return nil, fmt.Errorf(
					"HTTPS connection failed for %s: attempted=%d total=%d: %w",
					target.host, attempted, total, lastErr,
				)
			}

		case <-timer.C:
			if next < total && active < deps.maxConcurrency {
				launch()
			}
			resetTimer()

		case <-overallCtx.Done():
			drainLosers(nil)
			cause := overallCtx.Err()
			if ctxErr := ctx.Err(); ctxErr != nil {
				cause = ctxErr
			}
			deps.logf(
				"[ssrf-tls] hostname=%s phase=tls total_elapsed=%s category=%s attempted=%d total=%d",
				target.host, time.Since(started), classifyTLSCandidateError(cause), attempted, total,
			)
			return nil, fmt.Errorf(
				"HTTPS connection failed for %s: attempted=%d total=%d: %w",
				target.host, attempted, total, cause,
			)
		}
	}
}

func dialAndHandshakeTLS(
	ctx context.Context,
	network string,
	addr string,
	diagnosticIP string,
	config *tls.Config,
	dialContext func(context.Context, string, string) (net.Conn, error),
) (net.Conn, tlsCandidateResult) {
	started := time.Now()
	rawConn, err := dialContext(ctx, network, addr)
	if err != nil {
		return nil, tlsCandidateResult{phase: "tcp", duration: time.Since(started), err: err}
	}

	tlsConn := tls.Client(rawConn, config)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return nil, tlsCandidateResult{phase: "tls", duration: time.Since(started), err: err}
	}
	return tlsConn, tlsCandidateResult{
		ip: diagnosticIP, phase: "tls", duration: time.Since(started),
	}
}

func classifyTLSCandidateError(err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalid x509.CertificateInvalidError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostnameError) || errors.As(err, &certificateInvalid) {
		return "certificate"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "certificate") || strings.Contains(message, "x509") {
		return "certificate"
	}
	return "connection"
}
