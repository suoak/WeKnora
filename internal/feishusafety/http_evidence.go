package feishusafety

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Only fixed categories and digests cross the audit boundary. Never include
// provider messages, response bodies, URLs, headers or credential material.
type httpStopEvidence struct {
	Family, Operation, SourceHash, EndpointHash, RequestIDHash string
	Status                                                     int
}

func httpAuditDigest(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (t Transport) tripHTTP(g *Guard, req *http.Request, resp *http.Response) {
	family, operation := "feishu", "other"
	switch {
	case t.Embedding:
		family, operation = "embedding", "embedding"
	case t.BackgroundModel:
		family, operation = "background_model", "model"
	default:
		for _, route := range []string{"export_tasks", "user_info", "docx", "wiki", "sheets", "bitable", "download"} {
			if strings.Contains(req.URL.Path, "/"+route+"/") || strings.HasSuffix(req.URL.Path, "/"+route) {
				operation = route
				break
			}
		}
	}
	scope, _ := req.Context().Value(scopeKey{}).(Scope)
	requestID := resp.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = resp.Header.Get("X-Tt-Logid")
	}
	evidence, _ := json.Marshal(httpStopEvidence{
		Family: family, Operation: operation, Status: resp.StatusCode,
		SourceHash: httpAuditDigest(scope.SourceID), EndpointHash: httpAuditDigest(req.URL.Hostname()),
		RequestIDHash: httpAuditDigest(requestID),
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = g.tripUsingEvidence(ctx, g.client, fmt.Sprintf("%s_http_%d", family, resp.StatusCode), string(evidence))
}
