//go:build sqlite_fts5 && knowhub_m5_integration

package router

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Actual existing route registrations, handlers, services, SQL repositories,
// Auth and APIKeyRouteAuthorizer. Only employee/approver facts are synthetic.
func TestDocumentM5PythonActualREST(t *testing.T) {
	h := newDocumentHTTPFixture(t)
	var raw [32]byte
	_, err := rand.Read(raw[:])
	require.NoError(t, err)
	nonce := hex.EncodeToString(raw[:])
	var knowledgeCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__test/control" {
			if r.Header.Get("X-Test-Control") != nonce {
				w.WriteHeader(403)
				return
			}
			var input struct {
				Action string `json:"action"`
			}
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				w.WriteHeader(400)
				return
			}
			var err error
			switch input.Action {
			case "reset":
				err = h.f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=NULL WHERE id=4").Error
				if err == nil {
					err = h.f.DB.Exec("UPDATE document_subject_grants SET state='approved',revision=revision+1 WHERE knowledge_id='sensitive' AND user_id='D'").Error
				}
			case "revoke-grant":
				err = h.f.DB.Exec("UPDATE document_subject_grants SET state='revoked',revision=revision+1 WHERE knowledge_id='sensitive' AND user_id='D'").Error
			case "revoke-key":
				err = h.f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=4").Error
			case "status":
			default:
				w.WriteHeader(400)
				return
			}
			if err != nil {
				w.WriteHeader(500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"knowledge_calls": knowledgeCalls.Load()})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/knowledge/") || strings.HasPrefix(r.URL.Path, "/api/v1/chunks/") {
			knowledgeCalls.Add(1)
		}
		h.engine.ServeHTTP(w, r)
	}))
	defer server.Close()
	python, err := exec.LookPath("python")
	require.NoError(t, err)
	script, err := filepath.Abs("../../docs/integration/knowhub-mcp-portal/tools/batch5_python_core.py")
	require.NoError(t, err)
	keys, err := json.Marshal(h.keys)
	require.NoError(t, err)
	cmd := exec.Command(python, script)
	cmd.Env = append(os.Environ(), "KNOWHUB_M5_BACKEND="+server.URL, "KNOWHUB_M5_CONTROL="+nonce, "KNOWHUB_M5_KEYS="+string(keys))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Log(string(output))
	require.Zero(t, h.uncertifiedCalls.Load())
}
