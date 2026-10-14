// Standalone, synthetic-only analysis. No SID endpoints or real credentials.
// Run with GO111MODULE=off GOTOOLCHAIN=local go run this-file.go.
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

func must(ok bool) {
	if !ok {
		panic("synthetic assertion failed")
	}
}

func main() {
	fixtures := []struct{ Name, Secret string }{
		{"unreserved", "SyntheticOnly-AZ_19.~"},
		{"plus", "Synthetic+A"}, {"slash", "Synthetic/A"},
		{"equals", "Synthetic=A"}, {"percent", "Synthetic%A"},
		{"colon", "Synthetic:A"}, {"space", "Synthetic A"},
		{"non_ascii", "Synthetic-测试"},
		{"already_encoded_literal", "Synthetic%2BA"},
	}
	rows := []map[string]interface{}{}
	for _, f := range fixtures {
		raw, _ := http.NewRequest("POST", "http://127.0.0.1/unused", nil)
		raw.SetBasicAuth("knowhub", f.Secret)
		oauth, _ := http.NewRequest("POST", "http://127.0.0.1/unused", nil)
		oauth.SetBasicAuth(url.QueryEscape("knowhub"), url.QueryEscape(f.Secret))
		// Postman runtime's ordinary Basic signer: UTF-8 username:password,
		// base64 once. This does not model unknown OAuth helper preprocessing.
		postman := "Basic " + base64.StdEncoding.EncodeToString([]byte("knowhub:"+f.Secret))
		_, decoded, ok := oauth.BasicAuth()
		must(ok)
		unescaped, err := url.QueryUnescape(decoded)
		must(err == nil && unescaped == f.Secret)
		must(raw.Header.Get("Authorization") == postman)
		rows = append(rows, map[string]interface{}{
			"fixture":                  f.Name,
			"synthetic_secret":         f.Secret,
			"oauth_password_component": url.QueryEscape(f.Secret),
			"set_basic_auth_equals_raw_postman_algorithm":        true,
			"oauth_header_equals_raw_header":                     oauth.Header.Get("Authorization") == postman,
			"oauth_receiver_one_form_decode_recovers_original":   true,
			"oauth_base64_decode_only_receiver_matches_original": decoded == f.Secret,
			"double_query_escape_differs":                        url.QueryEscape(url.QueryEscape(f.Secret)) != url.QueryEscape(f.Secret),
		})
	}
	observed := map[string]interface{}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		must(r.ParseForm() == nil)
		u, p, ok := r.BasicAuth()
		observed["method_post"] = r.Method == "POST"
		observed["basic_received_intact"] = ok && u == "knowhub" && p == "SyntheticOnly-AZ_19.~"
		observed["client_id_with_basic_present"] = r.PostForm.Get("client_id") == "knowhub"
		observed["client_secret_form_absent"] = !r.PostForm.Has("client_secret")
		observed["code_verifier_absent"] = !r.PostForm.Has("code_verifier")
		observed["redirect_uri_exact"] = r.PostForm.Get("redirect_uri") == "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback"
		observed["content_type_urlencoded"] = r.Header.Get("Content-Type") == "application/x-www-form-urlencoded"
		w.Header().Set("WWW-Authenticate", `Basic realm="synthetic-only"`)
		w.WriteHeader(401)
	}))
	defer server.Close()
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {"knowhub"}, "code": {"synthetic-one-use-code"}, "redirect_uri": {"http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback"}}
	request, _ := http.NewRequest("POST", server.URL, strings.NewReader(form.Encode()))
	request.SetBasicAuth(url.QueryEscape("knowhub"), url.QueryEscape("SyntheticOnly-AZ_19.~"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	resp, err := server.Client().Do(request)
	must(err == nil)
	bytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	must(err == nil && resp.StatusCode == 401 && len(bytes) == 0)
	for _, value := range observed {
		must(value == true)
	}
	observed["mock_status"] = resp.StatusCode
	observed["mock_response_bytes"] = len(bytes)
	json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"synthetic_only": true, "real_sid_requests": 0, "fixtures": rows, "loopback_request_test": observed})
}
