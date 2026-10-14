package service

// This observer never dumps headers/bodies or changes transport policy. Fields
// supplied by a peer are reduced to enums; network addresses must parse as IPs.
import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sourceIDNetworkEvent struct {
	Kind       string   `json:"kind"`
	UTC        string   `json:"utc"`
	Beijing    string   `json:"beijing"`
	Addresses  []string `json:"addresses,omitempty"`
	Result     string   `json:"result,omitempty"`
	DurationMS *int64   `json:"duration_ms,omitempty"`
}

type sourceIDTokenTelemetry struct {
	mu                         sync.Mutex
	phaseStarts                map[string][]time.Time
	Trace                      string                 `json:"trace"`
	UTC                        string                 `json:"utc"`
	Beijing                    string                 `json:"beijing"`
	Scheme                     string                 `json:"scheme"`
	Accept                     string                 `json:"accept_class"`
	UserAgentPresent           bool                   `json:"user_agent_header_present"`
	UserAgentClass             string                 `json:"user_agent_class"`
	WrittenUserAgent           string                 `json:"written_user_agent_class"`
	AuthorizationValuesWritten int                    `json:"authorization_values_written"`
	BasicStrategy              string                 `json:"basic_strategy"`
	SecretLoaded               bool                   `json:"client_secret_loaded"`
	FormEncoding               bool                   `json:"oauth_form_encoding_executed"`
	SetBasicAuth               bool                   `json:"set_basic_auth_used"`
	AuthorizationCount         int                    `json:"authorization_header_value_count"`
	DuplicateAuthorization     bool                   `json:"duplicate_authorization"`
	BodyValueCount             int                    `json:"body_value_count"`
	KnownBodyCounts            map[string]int         `json:"known_body_field_counts"`
	DuplicateBodyFields        bool                   `json:"duplicate_body_fields"`
	PKCEDisabled               bool                   `json:"pkce_disabled"`
	State                      string                 `json:"state_validation"`
	Nonce                      string                 `json:"nonce_validation_at_token_dispatch"`
	Proxy                      string                 `json:"proxy_policy"`
	Remote                     string                 `json:"remote_address"`
	TLS                        string                 `json:"tls_version"`
	ALPN                       string                 `json:"negotiated_protocol"`
	HTTP                       string                 `json:"response_http_version"`
	Reused                     bool                   `json:"connection_reused"`
	Events                     []sourceIDNetworkEvent `json:"network_events"`
	Dropped                    int                    `json:"events_dropped"`
	WriteAttempts              int                    `json:"observed_write_attempts"`
	SuccessfulWrites           int                    `json:"observed_completed_writes"`
	Retry                      bool                   `json:"retry_observed"`
	Redirect                   bool                   `json:"redirect_response"`
	Date                       string                 `json:"response_date_utc"`
	Server                     string                 `json:"server_class"`
	Via                        string                 `json:"via_class"`
	Realm                      string                 `json:"www_realm_class"`
	ProxyChallenge             bool                   `json:"proxy_authenticate_present"`
	Failure                    string                 `json:"network_failure_class"`
	RejectingLayer             string                 `json:"rejecting_layer"`
	Outcome                    string                 `json:"outcome"`
}

func (d *sourceIDTokenTelemetry) MarshalJSON() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	type publicView sourceIDTokenTelemetry
	return json.Marshal((*publicView)(d))
}

func telemetryTimes() (string, string) {
	now := time.Now()
	return now.UTC().Format(time.RFC3339Nano), now.In(time.FixedZone("Beijing", 8*3600)).Format(time.RFC3339Nano)
}

func telemetryAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil {
		return "NOT_CAPTURED"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "NOT_CAPTURED"
	}
	return net.JoinHostPort(net.ParseIP(host).String(), strconv.Itoa(n))
}

func (d *sourceIDTokenTelemetry) event(kind, result string, addresses ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.Events) >= 64 {
		d.Dropped++
		return
	}
	u, b := telemetryTimes()
	e := sourceIDNetworkEvent{Kind: kind, UTC: u, Beijing: b, Result: result, Addresses: addresses}
	phase := strings.TrimSuffix(strings.TrimSuffix(kind, "_start"), "_done")
	if phase == "tcp" && len(addresses) > 0 {
		phase += "/" + addresses[0]
	}
	if d.phaseStarts == nil {
		d.phaseStarts = map[string][]time.Time{}
	}
	if strings.HasSuffix(kind, "_start") {
		d.phaseStarts[phase] = append(d.phaseStarts[phase], time.Now())
	}
	if strings.HasSuffix(kind, "_done") && len(d.phaseStarts[phase]) > 0 {
		elapsed := time.Since(d.phaseStarts[phase][0]).Milliseconds()
		e.DurationMS = &elapsed
		d.phaseStarts[phase] = d.phaseStarts[phase][1:]
	}
	d.Events = append(d.Events, e)
}

// secretLoaded is a boolean computed by the caller, never a credential.
func observeSourceIDTokenRequest(req *http.Request, trace string, secretLoaded, pkce, statePassed bool, basic bool) (*http.Request, *sourceIDTokenTelemetry) {
	u, b := telemetryTimes()
	d := &sourceIDTokenTelemetry{Trace: trace, UTC: u, Beijing: b, Scheme: "other", Accept: "other", UserAgentClass: "transport_default_not_observed", BasicStrategy: "not_basic", SecretLoaded: secretLoaded, PKCEDisabled: !pkce, State: "NOT_CAPTURED", Nonce: "NOT_YET_EXECUTED", Proxy: "no_proxy_in_configured_transport", Remote: "NOT_CAPTURED", TLS: "NOT_CAPTURED", ALPN: "NOT_CAPTURED", HTTP: "NOT_CAPTURED", Date: "NOT_CAPTURED", Server: "absent", Via: "absent", Realm: "absent", Failure: "none", RejectingLayer: "UNKNOWN", KnownBodyCounts: map[string]int{}}
	d.Events = []sourceIDNetworkEvent{}
	d.WrittenUserAgent = "NOT_CAPTURED"
	if req.URL.Scheme == "https" || req.URL.Scheme == "http" {
		d.Scheme = req.URL.Scheme
	}
	if req.Header.Get("Accept") == "application/json" {
		d.Accept = "application/json"
	}
	if statePassed {
		d.State = "PASS"
	}
	if basic {
		d.BasicStrategy = "oauth_form_escape_then_set_basic_auth_base64_once"
		d.FormEncoding = true
		d.SetBasicAuth = true
	}
	for key, values := range req.Header {
		if strings.EqualFold(key, "Authorization") {
			d.AuthorizationCount += len(values)
		}
	}
	d.DuplicateAuthorization = d.AuthorizationCount > 1
	d.UserAgentPresent = req.Header.Get("User-Agent") != ""
	if d.UserAgentPresent {
		d.UserAgentClass = "other"
		if req.Header.Get("User-Agent") == "Go-http-client/1.1" {
			d.UserAgentClass = "Go-http-client/1.1"
		}
	}
	if req.GetBody != nil {
		r, err := req.GetBody()
		if err == nil {
			// Same bounded inspection as existing diagnostics, only counts kept.
			buf := make([]byte, 65537)
			n := 0
			for n < len(buf) {
				m, e := r.Read(buf[n:])
				n += m
				if e != nil || m == 0 {
					break
				}
			}
			r.Close()
			if n <= 65536 {
				if form, e := url.ParseQuery(string(buf[:n])); e == nil {
					for k, v := range form {
						d.BodyValueCount += len(v)
						if len(v) > 1 {
							d.DuplicateBodyFields = true
						}
						switch k {
						case "grant_type", "client_id", "client_secret", "code", "redirect_uri", "code_verifier":
							d.KnownBodyCounts[k] = len(v)
						}
					}
				}
			}
			for i := range buf {
				buf[i] = 0
			}
		}
	}
	traceHooks := &httptrace.ClientTrace{
		WroteHeaderField: func(key string, values []string) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if strings.EqualFold(key, "Authorization") {
				d.AuthorizationValuesWritten += len(values)
			}
			if strings.EqualFold(key, "User-Agent") {
				d.WrittenUserAgent = "other"
				if len(values) == 1 && values[0] == "Go-http-client/1.1" {
					d.WrittenUserAgent = "Go-http-client/1.1"
				}
			}
		},
		DNSStart: func(httptrace.DNSStartInfo) { d.event("dns_start", "started") },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			addresses := []string{}
			for _, a := range info.Addrs {
				if len(addresses) >= 8 {
					break
				}
				if a.IP.To16() != nil {
					addresses = append(addresses, a.IP.String())
				}
			}
			result := "ok"
			if info.Err != nil {
				result = "error"
			}
			d.event("dns_done", result, addresses...)
		},
		ConnectStart: func(_, addr string) { d.event("tcp_start", "started", telemetryAddress(addr)) },
		ConnectDone: func(_, addr string, err error) {
			result := "ok"
			if err != nil {
				result = "error"
			}
			d.event("tcp_done", result, telemetryAddress(addr))
		},
		TLSHandshakeStart: func() { d.event("tls_start", "started") },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			d.mu.Lock()
			switch state.Version {
			case tls.VersionTLS12:
				d.TLS = "TLS1.2"
			case tls.VersionTLS13:
				d.TLS = "TLS1.3"
			default:
				d.TLS = "other"
			}
			switch state.NegotiatedProtocol {
			case "h2", "http/1.1":
				d.ALPN = state.NegotiatedProtocol
			case "":
				d.ALPN = "absent"
			default:
				d.ALPN = "other"
			}
			d.mu.Unlock()
			result := "ok"
			if err != nil {
				result = "error"
			}
			d.event("tls_done", result)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			d.mu.Lock()
			d.Reused = info.Reused
			d.Remote = telemetryAddress(info.Conn.RemoteAddr().String())
			d.mu.Unlock()
			d.event("connection_acquired", "ok")
		},
		WroteHeaders: func() { d.event("headers_written", "ok") },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			d.mu.Lock()
			d.WriteAttempts++
			d.Retry = d.WriteAttempts > 1
			if info.Err == nil {
				d.SuccessfulWrites++
			}
			d.mu.Unlock()
			result := "ok"
			if info.Err != nil {
				result = "error"
			}
			d.event("request_write_completed", result)
		},
		GotFirstResponseByte: func() { d.event("first_response_byte", "ok") },
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), traceHooks)), d
}

func telemetryProduct(raw string) string {
	if raw == "" {
		return "absent"
	}
	lower := strings.ToLower(raw)
	for _, p := range []string{"nginx", "openresty", "envoy", "apache", "microsoft-iis"} {
		if lower == p || strings.HasPrefix(lower, p+"/") {
			return p
		}
	}
	return "other"
}

func (d *sourceIDTokenTelemetry) response(resp *http.Response, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.Failure = probeTransportFailure(err)
		return
	}
	if resp == nil {
		d.Failure = "transport_error"
		return
	}
	switch resp.Proto {
	case "HTTP/1.1", "HTTP/2.0":
		d.HTTP = resp.Proto
	default:
		d.HTTP = "other"
	}
	d.Redirect = resp.StatusCode >= 300 && resp.StatusCode < 400
	if date, e := http.ParseTime(resp.Header.Get("Date")); e == nil {
		d.Date = date.UTC().Format(time.RFC3339)
	}
	d.Server = telemetryProduct(resp.Header.Get("Server"))
	if resp.Header.Get("Via") != "" {
		d.Via = "present_value_withheld"
	}
	www := strings.ToLower(resp.Header.Get("WWW-Authenticate"))
	if www != "" {
		d.Realm = "not_detected"
		if len(www) <= 4096 && strings.Contains(www, "realm=") {
			d.Realm = "present_value_withheld"
		}
	}
	d.ProxyChallenge = resp.Header.Get("Proxy-Authenticate") != ""
}

func (d *sourceIDTokenTelemetry) complete() { d.event("exchange_complete", "finished") }
