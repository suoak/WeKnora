package service

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestP4ManifestBindsOriginalAttachmentBytesAndParsingReceipt(t *testing.T) {
	s, err := newPersonalManifestStore(t.TempDir())
	require.NoError(t, err)
	a := &types.FeishuPersonalAttempt{ID: uuid.NewString(), ImportID: uuid.NewString(), CandidateVersionID: uuid.NewString(), Revision: "1"}
	a.ManifestKey = a.ID + "/" + a.CandidateVersionID + "/f1"
	data := []byte("original attachment")
	h := sha256.Sum256(data)
	asset := core.ApplicationAttachment{TableID: "t", FieldID: "f", RecordID: "r", Token: "token", Name: "a.txt", Size: int64(len(data)), SHA256: hex.EncodeToString(h[:]), Text: "original attachment", Parser: "existing_simple", Data: data}
	manifest, err := s.put(a, []byte("canonical table"), "T.md", asset)
	require.NoError(t, err)
	require.Len(t, manifest.Assets, 1)
	require.NotEqual(t, personalManifestDigest([]byte("canonical table"), nil), manifest.Hash, "attachment receipt must participate in the source manifest digest")
	a.ManifestHash = manifest.Hash
	_, err = s.read(a)
	require.NoError(t, err)
	p, err := s.path(a.ManifestKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(p, "assets", asset.SHA256+".bin"), []byte("changed"), 0600))
	_, err = s.read(a)
	require.Error(t, err, "recovery must not adopt a manifest with missing or modified original attachment bytes")
	require.FileExists(t, filepath.Join(p, "content"), "failure must not delete the original candidate")
}
