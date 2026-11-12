package mcpserver

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type protocolOnlyStore struct {
	*selectedMCPStore
	err   error
	calls int
}

func (s *protocolOnlyStore) VerifyPortalMCPProtocolCredential(
	_ context.Context, user string, key, tenant uint64,
) error {
	s.calls++
	if user != "A" || key != 1 || tenant != 99 {
		return errors.New("wrong authenticated identity")
	}
	return s.err
}

func TestPersonalProtocolDiscoveryDoesNotResolveCorpusOrGrantData(t *testing.T) {
	s, underlying, ctx := selectedMCPFixture()
	store := &protocolOnlyStore{selectedMCPStore: underlying}
	s.knowledge.Resolver.Store = store
	for _, method := range []string{"initialize", "tools/list", "resources/list"} {
		body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{}}`
		req := httptest.NewRequest("POST", personalPath, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, req)
		require.NotEqual(t, 403, out.Code)
		require.Zero(t, underlying.unrelatedReads)
	}
	require.Equal(t, 3, store.calls)
	// A real tool request still performs the complete authorization and fails
	// when its corpus cannot be resolved, despite successful discovery.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
		`"params":{"name":"list_knowledge_bases","arguments":{}}}`
	req := httptest.NewRequest("POST", personalPath, strings.NewReader(body)).WithContext(ctx)
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, req)
	require.Equal(t, 403, out.Code)
	require.Positive(t, underlying.unrelatedReads)
	store.err = errors.New("revoked")
	body = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req = httptest.NewRequest("POST", personalPath, strings.NewReader(body)).WithContext(ctx)
	out = httptest.NewRecorder()
	s.Handler().ServeHTTP(out, req)
	require.Equal(t, 403, out.Code, "a later revoked credential must immediately lose protocol access")
}

func TestPersonalScopeTimeoutIsNotReportedAsPermissionDenial(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{context.DeadlineExceeded, 504}, {errors.New("forbidden"), 403}} {
		out := httptest.NewRecorder()
		writePersonalScopeError(out, httptest.NewRequest("POST", personalPath, nil), tc.err)
		require.Equal(t, tc.status, out.Code)
		if tc.status == 504 {
			require.Equal(t, "scope_timeout", out.Header().Get("X-KnowHub-Error-Code"))
		}
	}
}

func TestPersonalMetadataShortcutExcludesDataAndFutureMethods(t *testing.T) {
	for _, method := range []string{"tools/call", "resources/read", "prompts/get", "future/execute", ""} {
		require.False(t, personalMetadataMethod(method))
	}
	for _, method := range []string{
		"initialize", "ping", "tools/list", "resources/list", "resources/templates/list", "prompts/list",
	} {
		require.True(t, personalMetadataMethod(method))
	}
}
