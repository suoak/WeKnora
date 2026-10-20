package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceIDOIDCUserInfoTransport(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	for _, transport := range []string{"", "bearer_get", "form_post", "multipart_post"} {
		t.Run(transport, func(t *testing.T) {
			const token = "mock-only-token+&="
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Empty(t, r.URL.RawQuery, "credentials must never enter the URL")
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				if transport == "" || transport == "bearer_get" {
					require.Equal(t, http.MethodGet, r.Method)
					require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
				} else {
					require.Equal(t, http.MethodPost, r.Method)
					require.Empty(t, r.Header.Get("Authorization"))
					if transport == "multipart_post" {
						require.True(
							t,
							strings.HasPrefix(
								r.Header.Get(
									"Content-Type",
								),
								"multipart/form-data; boundary=",
							),
						)
						require.NoError(t, r.ParseMultipartForm(4096))
					} else {
						require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
						require.NoError(t, r.ParseForm())
					}
					require.Equal(t, []string{token}, r.PostForm["access_token"])
				}
				_ = json.NewEncoder(w).
					Encode(map[string]interface{}{
						"sub": "mock-subject",
						"attributes": map[string]string{
							"employee": "0001",
						},
					})
			}))
			defer server.Close()
			s := &userService{}
			claims, err := s.fetchOIDCUserInfoWithTransport(
				context.Background(),
				server.URL,
				token,
				transport,
			)
			require.NoError(t, err)
			require.Equal(t, "mock-subject", claims["sub"])
			require.Equal(t, "0001", claims["attributes"].(map[string]interface{})["employee"])
		})
	}
}

func TestSourceIDOIDCUserInfoTransportRejectsUnknownAndSanitizesError(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("mock-secret-token"))
	}))
	defer server.Close()
	s := &userService{}
	_, err := s.fetchOIDCUserInfoWithTransport(
		context.Background(),
		server.URL,
		"mock-secret-token",
		"query_get",
	)
	require.Error(t, err)
	require.Zero(t, calls)
	for _, transport := range []string{"bearer_get", "form_post", "multipart_post"} {
		_, err = s.fetchOIDCUserInfoWithTransport(
			context.Background(),
			server.URL,
			"mock-secret-token",
			transport,
		)
		require.ErrorContains(t, err, "status=401")
		require.NotContains(t, err.Error(), "mock-secret-token")
	}
}
