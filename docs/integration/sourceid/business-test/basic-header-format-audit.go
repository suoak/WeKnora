//go:build ignore

// Synthetic-only audit; never accepts credentials or calls a SID endpoint.
package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
)

func require(ok bool) {
	if !ok {
		panic("synthetic Basic audit assertion failed")
	}
}

func main() {
	type sample struct{ Name, ID, Secret string }
	fixtures := []sample{
		{"alphanumeric", "knowhub", "SyntheticAZ09"},
		{"url_safe", "knowhub", "Synthetic-AZ_09.~"},
		{"plus", "knowhub", "Synthetic+Only"},
		{"slash", "knowhub", "Synthetic/Only"},
		{"equals", "knowhub", "Synthetic=Only"},
		{"percent", "knowhub", "Synthetic%Only"},
		{"colon_in_secret", "knowhub", "Synthetic:Only"},
		{"space_in_secret", "knowhub", "Synthetic Only"},
		{"unicode", "knowhub", "Synthetic-\u6d4b\u8bd5"},
		{"literal_percent_encoding", "knowhub", "Synthetic%2BOnly"},
		{"client_id_plus", "synthetic+client", "SyntheticOnly"},
	}
	rows := []map[string]interface{}{}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		auth := r.Header.Values("Authorization")
		require(len(auth) == 1 && strings.HasPrefix(auth[0], "Basic "))
		require(len(auth[0]) > 6 && auth[0][5] == 0x20 && !strings.ContainsAny(auth[0][6:], " \r\n\t"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	for _, f := range fixtures {
		var headers [2]string
		for index, mode := range []string{"oauth_form", "raw"} {
			id, secret := f.ID, f.Secret
			if mode == "oauth_form" {
				id, secret = url.QueryEscape(id), url.QueryEscape(secret)
			}
			req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("grant_type=authorization_code"))
			require(err == nil)
			req.SetBasicAuth(id, secret)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			h := req.Header.Get("Authorization")
			headers[index] = h
			require(len(req.Header.Values("Authorization")) == 1)
			require(strings.HasPrefix(h, "Basic ") && h[5] == 0x20 && !strings.ContainsAny(h[6:], " \r\n\t"))
			decoded, err := base64.StdEncoding.Strict().DecodeString(h[6:])
			require(err == nil && string(decoded) == id+":"+secret)
			require(decoded[len(id)] == 0x3A && !strings.Contains(string(decoded), "\uff1a"))
			resp, err := server.Client().Do(req)
			require(err == nil && resp.StatusCode == http.StatusNoContent)
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			require(req.Header.Get("Authorization") == h)
		}
		rows = append(rows, map[string]interface{}{"fixture": f.Name, "one_header": true, "basic_ascii_space": true, "separator_ascii_colon": true, "single_base64_decode_matches_expected_components": true, "header_unchanged_after_send": true, "oauth_equals_raw": headers[0] == headers[1], "oauth_component_escape_changes_input": url.QueryEscape(f.ID) != f.ID || url.QueryEscape(f.Secret) != f.Secret})
	}
	require(calls == 2*len(fixtures))
	json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"synthetic_only": true, "real_sid_requests": 0, "real_credential_equivalence": "UNKNOWN", "fixtures": rows, "loopback_requests": calls, "double_base64_detected": false, "unexpected_header_replacement_detected": false})
}
