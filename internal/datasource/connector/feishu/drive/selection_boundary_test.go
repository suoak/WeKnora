package drive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/stretchr/testify/require"
)

func TestDriveSelectedDirectChildDoesNotTraversePeers(t *testing.T) {
	for _, foundPage := range []string{"", "second"} {
		t.Run("found-page="+foundPage, func(t *testing.T) {
			var listed []string
			mux := http.NewServeMux()
			mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal",
				func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, core.TokenResponse{TenantAccessToken: "synthetic-only", Expire: 7200})
				},
			)
			mux.HandleFunc("/open-apis/drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
				folder, page := r.URL.Query().Get("folder_token"), r.URL.Query().Get("page_token")
				listed = append(listed, folder+":"+page)
				if folder != "root" {
					t.Errorf("selected direct file must not be probed as folder or traverse peer: %s", folder)
					http.Error(w, "outside selected scope", http.StatusForbidden)
					return
				}
				files := []core.DriveFile{{Token: "peer-folder", Type: "folder", ParentToken: "root"}}
				if page == foundPage {
					files = append(files, core.DriveFile{Token: "selected", Type: "docx", ParentToken: "root"})
				}
				writeJSON(w, core.DriveFileListResponse{Data: core.DriveFileListData{
					Files: files, HasMore: true, NextPageToken: "second",
				}})
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := core.NewClient(&core.Config{AppID: "synthetic", AppSecret: "synthetic", BaseURL: server.URL})
			files, err := listDriveFilesForResource(context.Background(), client, "root:selected")
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, "selected", files[0].Token)
			if foundPage == "" {
				require.Equal(t, []string{"root:"}, listed, "stop paging immediately after membership is established")
			} else {
				require.Equal(t, []string{"root:", "root:second"}, listed)
			}
		})
	}
}

func TestDriveSelectedDirectChildPermissionFailsClosed(t *testing.T) {
	var listed []string
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, core.TokenResponse{TenantAccessToken: "synthetic-only", Expire: 7200})
		})
	mux.HandleFunc("/open-apis/drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
		listed = append(listed, r.URL.Query().Get("folder_token"))
		w.WriteHeader(http.StatusForbidden)
		writeJSON(w, core.ApiResponse{Code: 99991672, Msg: "synthetic access denied"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := core.NewClient(&core.Config{AppID: "synthetic", AppSecret: "synthetic", BaseURL: server.URL})
	files, err := listDriveFilesForResource(context.Background(), client, "root:selected")
	require.Error(t, err)
	require.Empty(t, files)
	require.Equal(t, []string{"root"}, listed, "no fallback traversal when parent listing permission is denied")
}

func TestDriveSelectedFolderAndNestedCompatibility(t *testing.T) {
	for _, selected := range []string{"selected-folder", "nested-file"} {
		t.Run(selected, func(t *testing.T) {
			var listed []string
			mux := http.NewServeMux()
			mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal",
				func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, core.TokenResponse{TenantAccessToken: "synthetic-only", Expire: 7200})
				},
			)
			mux.HandleFunc("/open-apis/drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
				folder := r.URL.Query().Get("folder_token")
				listed = append(listed, folder)
				var files []core.DriveFile
				switch folder {
				case "root":
					files = []core.DriveFile{
						{Token: "selected-folder", Type: "folder", ParentToken: "root"},
						{Token: "peer-file", Type: "docx", ParentToken: "root"},
					}
				case "selected-folder":
					files = []core.DriveFile{{Token: "nested-file", Type: "docx", ParentToken: folder}}
				case "nested-file":
					writeJSON(w, core.ApiResponse{Code: 1061002, Msg: "params error"})
					return
				default:
					t.Errorf("unexpected folder enumeration: %s", folder)
				}
				writeJSON(w, core.DriveFileListResponse{Data: core.DriveFileListData{Files: files}})
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := core.NewClient(&core.Config{AppID: "synthetic", AppSecret: "synthetic", BaseURL: server.URL})
			files, err := listDriveFilesForResource(context.Background(), client, "root:"+selected)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, "nested-file", files[0].Token)
			if selected == "selected-folder" {
				require.Equal(t, []string{"root", "selected-folder"}, listed)
			} else {
				require.Equal(t, []string{"root", "nested-file", "root", "selected-folder"}, listed)
			}
		})
	}
}

func TestDriveSelectedDirectChildPaginationCycleFailsClosed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, core.TokenResponse{TenantAccessToken: "synthetic-only", Expire: 7200})
		})
	var listed []string
	mux.HandleFunc("/open-apis/drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
		listed = append(listed, r.URL.Query().Get("folder_token"))
		writeJSON(w, core.DriveFileListResponse{Data: core.DriveFileListData{HasMore: true, NextPageToken: "same"}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := core.NewClient(&core.Config{AppID: "synthetic", AppSecret: "synthetic", BaseURL: server.URL})
	files, err := listDriveFilesForResource(context.Background(), client, "root:selected")
	require.ErrorContains(t, err, "repeated page token")
	require.Empty(t, files)
	require.Equal(t, []string{"root", "root"}, listed)
}
