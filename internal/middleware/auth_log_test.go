package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Exercise actual log output at success, rejection, unknown route and panic,
// while ensuring the observability layer leaves handler input/output intact.
func TestOIDCLogsExcludeSensitivePayloads(t *testing.T) {
	const secret = "synthetic-private-SID-value"
	cases := []struct {
		name, path, method, contentType, body string
		panic                                 bool
	}{
		{"callback", "/api/v1/auth/oidc/callback", "GET", "", "", false},
		{
			"result",
			"/api/v1/auth/oidc/result",
			"POST",
			"application/json",
			`{"token":"` + secret + `","client_secret":"` + secret + `"}`,
			false,
		},
		{
			"logout form",
			"/api/v1/auth/oidc/backchannel-logout",
			"POST",
			"application/x-www-form-urlencoded",
			"logoutRequest=<LogoutRequest><SessionIndex>" +
				secret +
				"</SessionIndex><tgt>" +
				secret +
				"</tgt></LogoutRequest>",
			false,
		},
		{
			"logout xml",
			"/api/v1/auth/oidc/backchannel-logout",
			"POST",
			"text/xml",
			"<SessionIndex>" + secret + "</SessionIndex>",
			false,
		},
		{
			"truncated json",
			"/api/v1/auth/oidc/result",
			"POST",
			"application/json",
			`{"client_secret":"` + strings.Repeat(secret, 1000),
			false,
		},
		{"unknown auth route", "/api/v1/auth/oidc/unknown", "POST", "text/plain", secret, false},
		{"panic", "/api/v1/auth/oidc/callback", "GET", "", "", true},
		{
			"feishu application",
			"/api/v1/system/admin/feishu",
			"PUT",
			"application/json",
			`{"client_secret":"` + secret + `\"` + secret + `"}`,
			false,
		},
		{
			"feishu truncated config",
			"/api/v1/system/admin/feishu",
			"PUT",
			"application/json",
			`{"client_secret":"` + strings.Repeat(secret, 1000),
			false,
		},
		{"feishu callback", "/api/v1/me/feishu/callback", "POST", "text/plain", secret, false},
		{"feishu panic", "/api/v1/me/feishu/authorize", "POST", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			var output bytes.Buffer
			l := logrus.New()
			l.SetOutput(&output)
			l.SetFormatter(&logrus.JSONFormatter{})
			l.SetLevel(logrus.DebugLevel)
			r := gin.New()
			r.ContextWithFallback = true
			r.Use(Logger(), Recovery())
			handler := func(c *gin.Context) {
				if tc.panic {
					panic(secret)
				}
				if counter, ok := c.Request.Body.(*countingBody); ok && counter.read != 0 {
					t.Error("logger consumed sensitive input before handler")
				}
				body, err := io.ReadAll(c.Request.Body)
				if err != nil || string(body) != tc.body {
					t.Error("handler input changed")
				}
				_ = c.Error(errors.New(secret))
				c.Header("Content-Type", "application/json")
				c.String(400, `{"unknown_employee_field":"`+secret+`","access_token":"`+secret+`"}`)
			}
			if tc.name == "unknown auth route" {
				r.NoRoute(handler)
			} else {
				r.Handle(tc.method, tc.path, handler)
			}
			query := "?code=" +
				secret +
				"&state=" +
				secret +
				"&state=" +
				secret +
				"&logoutRequest=" +
				secret +
				"&SessionIndex=" +
				secret +
				"&tgt=" +
				secret +
				"&unknown=" +
				secret
			req := httptest.NewRequest(tc.method, tc.path+query, nil)
			req.Body = &countingBody{Reader: strings.NewReader(tc.body)}
			req.Header.Set("Content-Type", tc.contentType)
			for _, h := range []string{
				"Referer",

				"Cookie",

				"Authorization",

				"X-KnowHub-SourceID-Secret",

				"User-Agent",
			} {
				req.Header.Set(h, secret)
			}
			req = req.WithContext(
				context.WithValue(req.Context(), types.LoggerContextKey, logrus.NewEntry(l)),
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			logs := output.String()
			if strings.Contains(logs, secret) || strings.Contains(logs, "request_body") ||
				strings.Contains(logs, "response_body") ||
				strings.Contains(logs, "?code") {
				t.Fatal("sensitive authentication data reached log output")
			}
			if !strings.Contains(logs, tc.path) || !strings.Contains(logs, "status_code") {
				t.Fatal("safe access evidence missing")
			}
			if tc.panic {
				if w.Code != 500 || strings.Contains(w.Body.String(), secret) {
					t.Fatal("panic handling leaked or changed status")
				}
			} else if w.Code != 400 || !strings.Contains(w.Body.String(), secret) {
				t.Fatal("logger changed handler response")
			}
		})
	}
}

func TestSourceIDQueryAndOpaqueBodyPrivacy(t *testing.T) {
	const secret = "synthetic-logout-private"
	got := sanitizeQuery(

		"logout%52equest=" +
			secret +
			"&SessionIndex=" +
			secret +
			"&TGT=" +
			secret +
			"&client_secret=" +
			secret +
			"&token=" +
			secret,
	)
	if strings.Contains(got, secret) {
		t.Fatal("sensitive query values retained")
	}
	for _, contentType := range []string{
		"application/x-www-form-urlencoded",

		"text/xml",

		"application/xml",

		"text/plain",
	} {

		c, counter := newJSONContext(t, "/not-an-auth-route", secret)
		c.Request.Header.Set("Content-Type", contentType)
		got = readRequestBody(c)
		if strings.Contains(got, secret) || counter.read != 0 {
			t.Fatal("opaque body reached logger")
		}

	}
	if strings.Contains(
		sanitizeBody(`{"logoutRequest":"`+secret+`","SessionIndex":"`+secret+`","tgt":"`+secret+`"}`),
		secret,
	) {
		t.Fatal("JSON session credentials retained")
	}
}
