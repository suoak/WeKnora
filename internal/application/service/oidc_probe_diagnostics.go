package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// OIDCProbeHTTPDiagnostics defines oidcprobe httpdiagnostics state.
// Opt-in Token diagnostics contain only public configuration, fixed enums,
// existence/type checks and fingerprints. No free-form header/body text.
type OIDCProbeHTTPDiagnostics struct {
	Request  *OIDCProbeRequestDiagnostics  `json:"request"`
	Response *OIDCProbeResponseDiagnostics `json:"response,omitempty"`
}

// OIDCProbeRequestDiagnostics defines oidcprobe request diagnostics state.
type OIDCProbeRequestDiagnostics struct {
	Method               string          `json:"method"`
	Host                 string          `json:"host"`
	Path                 string          `json:"path"`
	HostMatchesEndpoint  bool            `json:"host_matches_endpoint"`
	ContentType          string          `json:"content_type"`
	AuthorizationType    string          `json:"authorization_type"`
	AuthorizationPresent bool            `json:"authorization_present"`
	BasicFormatValid     bool            `json:"basic_format_valid"`
	ClientID             string          `json:"configured_client_id"`
	RedirectURI          string          `json:"configured_redirect_uri"`
	FormFields           []string        `json:"form_fields"`
	FieldPresence        map[string]bool `json:"field_presence"`
	FieldChecks          map[string]bool `json:"field_checks"`
	UnknownFieldCount    int             `json:"unknown_field_count"`
	FormInspected        bool            `json:"form_inspected"`
	QueryPresent         bool            `json:"query_present"`
	DispatchUTC          string          `json:"dispatch_utc"`
	SentUTC              string          `json:"sent_utc,omitempty"`
}

// OIDCProbeCorrelation defines oidcprobe correlation state.
type OIDCProbeCorrelation struct {
	Present     bool   `json:"present"`
	FormatValid bool   `json:"format_valid"`
	Fingerprint string `json:"sha256,omitempty"`
}

// OIDCProbeResponseDiagnostics defines oidcprobe response diagnostics state.
type OIDCProbeResponseDiagnostics struct {
	ContentType             string                          `json:"content_type"`
	DeclaredLength          int64                           `json:"declared_length"`
	BytesRead               int64                           `json:"bytes_read"`
	BodyEOF                 bool                            `json:"body_eof"`
	PrefixTruncated         bool                            `json:"prefix_truncated"`
	ReadError               bool                            `json:"read_error"`
	BodyElapsedMS           int64                           `json:"body_elapsed_ms"`
	BodyKind                string                          `json:"body_kind"`
	ErrorFields             map[string]string               `json:"error_fields,omitempty"`
	UnknownFieldCount       int                             `json:"unknown_field_count"`
	ErrorDescriptionPresent bool                            `json:"error_description_present"`
	ErrorDescriptionSummary string                          `json:"error_description_summary,omitempty"`
	OAuthError              string                          `json:"oauth_error,omitempty"`
	VendorCode              string                          `json:"vendor_error_code,omitempty"`
	VendorCodeSuppressed    bool                            `json:"vendor_code_suppressed"`
	WWWPresent              bool                            `json:"www_authenticate_present"`
	WWWSchemes              []string                        `json:"www_schemes,omitempty"`
	WWWError                string                          `json:"www_oauth_error,omitempty"`
	WWWRealmPresent         bool                            `json:"www_realm_present"`
	WWWDescriptionPresent   bool                            `json:"www_description_present"`
	Correlation             map[string]OIDCProbeCorrelation `json:"correlation"`
	GatewaySuspected        bool                            `json:"gateway_suspected"`
}

var (
	probePublicCodePattern = regexp.MustCompile(`^(?:[A-Z][A-Z0-9_]{2,47}|[1-9][0-9]{2,7})$`)
	probeBasicPattern      = regexp.MustCompile(`^Basic [A-Za-z0-9+/]+={0,2}$`)
	probeIDPattern         = regexp.MustCompile(
		"^(?:[a-fA-F0-9]{32}|[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA" +
			"-F0-9]{4}-[a-fA-F0-9]{12})$",
	)
	probeTracePattern    = regexp.MustCompile(`^00-[a-f0-9]{32}-[a-f0-9]{16}-[a-f0-9]{2}$`)
	probeWWWErrorPattern = regexp.MustCompile(`(?i)(?:^|[, ])error="([a-z_]{1,40})"`)
)

func probeMediaType(raw string) string {
	v, _, err := mime.ParseMediaType(raw)
	if raw == "" {
		return "absent"
	}
	if err != nil {
		return "other"
	}
	switch strings.ToLower(v) {
	case "application/json",
		"application/problem+json",
		"text/html",
		"text/plain",
		"application/x-www-form-urlencoded":
		return strings.ToLower(v)
	default:
		return "other"
	}
}

func probeRequestDiagnostics(
	req *http.Request,
	clientID, redirect string,
) (*OIDCProbeHTTPDiagnostics, []string) {
	auth := req.Header.Get("Authorization")
	scheme := "absent"
	if auth != "" {
		scheme = "other"
	}
	if strings.HasPrefix(auth, "Basic ") {
		scheme = "Basic"
	}
	if strings.HasPrefix(auth, "Bearer ") {
		scheme = "Bearer"
	}
	method := "other"
	if req.Method == http.MethodPost {
		method = "POST"
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	d := &OIDCProbeRequestDiagnostics{
		Method:               method,
		Host:                 req.URL.Host,
		Path:                 req.URL.Path,
		HostMatchesEndpoint:  host == req.URL.Host,
		ContentType:          probeMediaType(req.Header.Get("Content-Type")),
		AuthorizationType:    scheme,
		AuthorizationPresent: auth != "",
		BasicFormatValid:     probeBasicPattern.MatchString(auth),
		ClientID:             clientID,
		RedirectURI:          redirect,
		FieldPresence:        map[string]bool{},
		FieldChecks:          map[string]bool{},
		QueryPresent:         req.URL.RawQuery != "",
		DispatchUTC:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	private := []string{auth}
	if _, encoded, ok := req.BasicAuth(); ok {
		private = append(private, encoded)
		if decoded, err := url.QueryUnescape(encoded); err == nil {
			private = append(private, decoded)
		}
	}
	if req.GetBody != nil {
		reader, err := req.GetBody()
		if err == nil {
			raw, readErr := io.ReadAll(io.LimitReader(reader, 64*1024+1))
			_ = reader.Close()
			if readErr == nil && len(raw) <= 64*1024 {
				form, parseErr := url.ParseQuery(string(raw))
				if parseErr == nil {
					d.FormInspected = true
					for _, k := range []string{
						"grant_type",
						"code",
						"redirect_uri",
						"client_id",
						"client_secret",
						"code_verifier",
					} {
						v, exists := form[k]
						d.FieldPresence[k] = exists
						if exists {
							d.FormFields = append(d.FormFields, k)
						}
						if k == "code" || k == "client_secret" || k == "code_verifier" {
							private = append(private, v...)
						}
					}
					d.UnknownFieldCount = len(form) - len(d.FormFields)
					d.FieldChecks["grant_type_authorization_code"] = len(form["grant_type"]) == 1 &&
						form.Get("grant_type") == "authorization_code"
					d.FieldChecks["client_id_matches_config"] = len(form["client_id"]) == 1 &&
						form.Get("client_id") == clientID
					d.FieldChecks["redirect_uri_matches_config"] = len(form["redirect_uri"]) == 1 &&
						form.Get("redirect_uri") == redirect
					d.FieldChecks["code_single_nonempty"] = len(form["code"]) == 1 && form.Get("code") != ""
				}
			}
		}
	}
	sort.Strings(d.FormFields)
	return &OIDCProbeHTTPDiagnostics{Request: d}, private
}

func probeDiagnosticValueSafe(value string, private []string) bool {
	if value == "" {
		return false
	}
	v := strings.ToLower(value)
	for _, secret := range private {
		if secret != "" && strings.EqualFold(value, secret) {
			return false
		}
		if len(secret) < 4 {
			continue
		}
		for _, encoded := range []string{
			secret,
			url.QueryEscape(
				secret,
			),
			base64.StdEncoding.EncodeToString(
				[]byte(
					secret,
				),
			),
		} {
			if strings.Contains(v, strings.ToLower(encoded)) {
				return false
			}
		}
	}
	return true
}

func probeOAuthCode(raw string) string {
	switch raw {
	case "invalid_request",
		"invalid_client",
		"invalid_grant",
		"unauthorized_client",
		"unsupported_grant_type",
		"invalid_scope",
		"invalid_token",
		"access_denied",
		"server_error",
		"temporarily_unavailable":
		return raw
	default:
		return ""
	}
}

func probeResponseDiagnostics(resp *http.Response, private []string) *OIDCProbeResponseDiagnostics {
	d := &OIDCProbeResponseDiagnostics{
		ContentType:    probeMediaType(resp.Header.Get("Content-Type")),
		DeclaredLength: resp.ContentLength,
		BodyKind:       "not_observed",
		Correlation:    map[string]OIDCProbeCorrelation{},
	}
	www := resp.Header.Get("WWW-Authenticate")
	d.WWWPresent = www != ""
	if len(www) <= 4096 && www != "" {
		lower := strings.ToLower(www)
		for _, scheme := range []string{"basic", "bearer"} {
			if strings.HasPrefix(lower, scheme+" ") || lower == scheme {
				d.WWWSchemes = append(d.WWWSchemes, scheme)
			}
		}
		if len(d.WWWSchemes) == 0 {
			d.WWWSchemes = []string{"other"}
		}
		d.WWWRealmPresent = strings.Contains(lower, "realm=")
		d.WWWDescriptionPresent = strings.Contains(lower, "error_description=")
		match := probeWWWErrorPattern.FindStringSubmatch(www)
		if len(match) == 2 && probeDiagnosticValueSafe(match[1], private) {
			d.WWWError = probeOAuthCode(match[1])
		}
	}
	for _, name := range []string{"X-Request-ID", "X-Correlation-ID", "Traceparent"} {
		values := resp.Header.Values(name)
		value := resp.Header.Get(name)
		entry := OIDCProbeCorrelation{Present: len(values) > 0}
		entry.FormatValid = len(values) == 1 &&
			((name == "Traceparent" &&
				probeTracePattern.MatchString(
					value,
				)) ||
				(name != "Traceparent" &&
					probeIDPattern.MatchString(
						value,
					)))
		if entry.FormatValid && probeDiagnosticValueSafe(value, private) {
			sum := sha256.Sum256([]byte(value))
			entry.Fingerprint = hex.EncodeToString(sum[:])
		}
		d.Correlation[name] = entry
	}
	return d
}

// Observes only bytes read by the existing decoder/discard path; never drains
// an otherwise unread body, changes decoding, retries or retains raw bytes.
type probeObservedBody struct {
	io.ReadCloser
	report            *OIDCProbeResponseDiagnostics
	started           time.Time
	privateValues     []string
	correlationValues map[string]string
	publicCodes       []string
	prefix            []byte
	closed            bool
}

func (b *probeObservedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.report.BytesRead += int64(n)
	if available := 4096 - len(b.prefix); available > 0 {
		count := n
		if count > available {
			count = available
		}
		b.prefix = append(b.prefix, p[:count]...)
	}
	if err == io.EOF {
		b.report.BodyEOF = true
	} else if err != nil {
		b.report.ReadError = true
	}
	b.report.PrefixTruncated = b.report.BytesRead > 4096 ||
		(b.report.BytesRead == 4096 &&
			!b.report.BodyEOF &&
			(b.report.DeclaredLength < 0 ||
				b.report.DeclaredLength > 4096))
	return n, err
}

func (b *probeObservedBody) Close() error {
	if !b.closed {
		b.closed = true
		b.report.BodyElapsedMS = time.Since(b.started).Milliseconds()
		probeClassifyResponse(b.report, b.prefix, b.publicCodes, b.privateValues, b.correlationValues)
		for i := range b.prefix {
			b.prefix[i] = 0
		}
		b.prefix = nil
		b.privateValues = nil
		b.correlationValues = nil
	}
	return b.ReadCloser.Close()
}

func probeClassifyResponse(
	d *OIDCProbeResponseDiagnostics,
	raw []byte,
	publicCodes, private []string,
	correlations map[string]string,
) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		d.BodyKind = "empty"
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(trimmed, &object) != nil || object == nil {
		lower := strings.ToLower(string(trimmed[:min(len(trimmed), 64)]))
		if d.ContentType == "text/html" || strings.HasPrefix(lower, "<!doctype html") ||
			strings.HasPrefix(lower, "<html") {
			d.BodyKind = "html"
			d.GatewaySuspected = true
		} else {
			d.BodyKind = "non_json_or_incomplete"
		}
		return
	}
	d.BodyKind = "json_object"
	d.ErrorFields = map[string]string{}
	known := map[string]bool{}
	for _, name := range []string{
		"error",
		"error_description",
		"errorCode",
		"error_code",
		"code",
		"message",
		"access_token",
		"id_token",
		"refresh_token",
		"token_type",
	} {
		known[name] = true
		if value, ok := object[name]; ok {
			d.ErrorFields[name] = probeJSONType(value)
		}
	}
	for name := range object {
		if !known[name] {
			d.UnknownFieldCount++
		}
	}
	// Token/error responses can echo tokens not present in the outgoing form.
	for _, name := range []string{
		"access_token",
		"id_token",
		"refresh_token",
		"authorization_code",
		"client_secret",
		"cookie",
		"password",
		"token",
		"tgt",
		"SessionIndex",
		"sessionIndex",
	} {
		var v string
		if json.Unmarshal(object[name], &v) == nil && v != "" {
			private = append(private, v)
		}
	}
	// Correlation values are fingerprints only. Remove even those when a server
	// places a token in a known ID header and echoes that token in its body.
	for name, entry := range d.Correlation {
		if !probeDiagnosticValueSafe(correlations[name], private) {
			entry.Fingerprint = ""
		}
		for _, v := range private {
			sum := sha256.Sum256([]byte(v))
			if entry.Fingerprint == hex.EncodeToString(sum[:]) {
				entry.Fingerprint = ""
			}
		}
		d.Correlation[name] = entry
	}
	if !probeDiagnosticValueSafe(d.WWWError, private) {
		d.WWWError = ""
	}
	if _, ok := object["error_description"]; ok {
		d.ErrorDescriptionPresent = true
		d.ErrorDescriptionSummary = "withheld_untrusted_description"
	}
	var errorCode string
	if json.Unmarshal(object["error"], &errorCode) != nil {
		var codes []string
		if json.Unmarshal(object["error"], &codes) == nil && len(codes) == 1 {
			errorCode = codes[0]
		}
	}
	if probeDiagnosticValueSafe(errorCode, private) {
		d.OAuthError = probeOAuthCode(errorCode)
	}
	for _, name := range []string{"errorCode", "error_code", "code"} {
		value, exists := object[name]
		if !exists {
			continue
		}
		var code string
		if json.Unmarshal(value, &code) != nil {
			var number json.Number
			if json.Unmarshal(value, &number) == nil {
				code = number.String()
			}
		}
		approved := false
		for _, known := range publicCodes {
			if known == code {
				approved = true
			}
		}
		if approved && probePublicCodePattern.MatchString(code) && probeDiagnosticValueSafe(code, private) {
			d.VendorCode = code
		} else {
			d.VendorCodeSuppressed = true
		}
	}
}

func probeJSONType(raw json.RawMessage) string {
	var v interface{}
	if json.Unmarshal(raw, &v) != nil {
		return "invalid"
	}
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}
