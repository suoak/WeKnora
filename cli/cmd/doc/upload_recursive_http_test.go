package doc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
	"github.com/Tencent/WeKnora/cli/internal/iostreams"
	sdk "github.com/Tencent/WeKnora/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadRecursiveRealMultipartRelativePaths(t *testing.T) {
	iostreams.SetForTest(t)
	root := t.TempDir()
	mkTree(t, root, "root.txt", "nested/inside.txt", "..drafts/dotted.txt")
	var mu sync.Mutex
	seen := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/knowledge-bases/fixture-kb/knowledge/file", r.URL.Path)
		if !assert.NoError(t, r.ParseMultipartForm(1<<20)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("file")
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file.Close()
		name := r.FormValue("fileName")
		assert.NotContains(t, name, "\\")
		assert.NotContains(t, name, "../")
		if header.Filename == "root.txt" {
			assert.Empty(t, r.MultipartForm.Value["fileName"], "root files omit the custom name field")
		}
		assert.Equal(t, "api", r.FormValue("channel"))
		mu.Lock()
		seen[header.Filename] = name
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sdk.KnowledgeResponse{Success: true, Data: sdk.Knowledge{ID: "fixture-id", FileName: header.Filename}})
	}))
	defer server.Close()
	require.NoError(t, runUploadRecursive(context.Background(), &UploadOptions{Recursive: true, Glob: "*"},
		&cmdutil.FormatOptions{Mode: cmdutil.FormatJSON}, sdk.NewClient(server.URL), "fixture-kb", root))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]string{"root.txt": "", "inside.txt": "nested/inside.txt", "dotted.txt": "..drafts/dotted.txt"}, seen)
	assert.Empty(t, knowledgeRelativePath(root, filepath.Join(filepath.Dir(root), "outside.txt")))
}
