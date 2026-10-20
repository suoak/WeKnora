package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// Personal manifests are outside Knowledge FileService and all public routes.
// A multi-APP deployment must mount this directory on shared durable storage.
type personalManifestStore struct{ root string }
type personalManifest struct {
	Assets      []core.ApplicationAttachment `json:"assets,omitempty"`
	AttemptID   string                       `json:"attempt_id"`
	CandidateID string                       `json:"candidate_id"`
	ImportID    string                       `json:"import_id"`
	Revision    string                       `json:"revision"`
	Hash        string                       `json:"sha256"`
	Size        int64                        `json:"size"`
	Name        string                       `json:"name"`
}

func newPersonalManifestStore(root string) (*personalManifestStore, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("personal staging requires an absolute private directory")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &personalManifestStore{root: resolved}, nil
}

func (s *personalManifestStore) path(key string) (string, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return "", errors.New("invalid personal manifest key")
	}
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, `\.:`) {
			return "", errors.New("invalid personal manifest key")
		}
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return "", err
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		return "", err
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

func (s *personalManifestStore) read(a *types.FeishuPersonalAttempt) (*personalManifest, error) {
	p, err := s.path(a.ManifestKey)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(p, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m personalManifest
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.AttemptID != a.ID || m.CandidateID != a.CandidateVersionID || m.ImportID != a.ImportID || m.Revision != a.Revision {
		return nil, errors.New("personal manifest ownership mismatch")
	}
	data, err := os.ReadFile(filepath.Join(p, "content"))
	if err != nil {
		return nil, err
	}
	if personalManifestDigest(data, m.Assets) != m.Hash || int64(len(data)) != m.Size || m.Size == 0 {
		return nil, errors.New("personal manifest content mismatch")
	}
	for _, asset := range m.Assets {
		if len(asset.SHA256) != 64 {
			return nil, errors.New("personal asset identity invalid")
		}
		if _, err := hex.DecodeString(asset.SHA256); err != nil {
			return nil, errors.New("personal asset identity invalid")
		}
		b, err := os.ReadFile(filepath.Join(p, "assets", asset.SHA256+".bin"))
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256(b)
		if int64(len(b)) != asset.Size || hex.EncodeToString(h[:]) != asset.SHA256 {
			return nil, errors.New("personal manifest asset mismatch")
		}
	}
	return &m, nil
}

func personalManifestDigest(data []byte, assets []core.ApplicationAttachment) string {
	h := sha256.Sum256(data)
	if len(assets) == 0 {
		return hex.EncodeToString(h[:])
	}
	b, _ := json.Marshal(struct {
		ContentSHA256 string                       `json:"content_sha256"`
		Assets        []core.ApplicationAttachment `json:"assets"`
	}{hex.EncodeToString(h[:]), assets})
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func writePersonalFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	return errors.Join(writeErr, f.Close())
}

func (s *personalManifestStore) put(a *types.FeishuPersonalAttempt, data []byte, name string, assets ...core.ApplicationAttachment) (*personalManifest, error) {
	if len(data) == 0 {
		return nil, errors.New("empty personal candidate")
	}
	if m, err := s.read(a); err == nil {
		return m, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	p, err := s.path(a.ManifestKey)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return nil, err
	}
	// Persist newly-created attempt/candidate directory entries as well as the
	// final rename. A successful file fsync alone does not make ancestors durable.
	if err = syncPersonalDir(s.root); err != nil {
		return nil, err
	}
	if err = syncPersonalDir(filepath.Dir(filepath.Dir(p))); err != nil {
		return nil, err
	}
	if err = syncPersonalDir(filepath.Dir(p)); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(p), ".pending-")
	if err != nil {
		return nil, err
	}
	// Only remove this invocation's unpublished private temporary directory.
	defer os.RemoveAll(tmp)
	m := personalManifest{Assets: assets, AttemptID: a.ID, CandidateID: a.CandidateVersionID, ImportID: a.ImportID, Revision: a.Revision, Hash: personalManifestDigest(data, assets), Size: int64(len(data)), Name: filepath.Base(name)}
	encoded, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if err = writePersonalFile(filepath.Join(tmp, "content"), data); err != nil {
		return nil, err
	}
	if len(assets) > 0 {
		if err = os.Mkdir(filepath.Join(tmp, "assets"), 0700); err != nil {
			return nil, err
		}
		written := map[string]bool{}
		var size int64
		for _, asset := range assets {
			h := sha256.Sum256(asset.Data)
			size += int64(len(asset.Data))
			if len(asset.Data) == 0 || int64(len(asset.Data)) != asset.Size || hex.EncodeToString(h[:]) != asset.SHA256 || size > 32<<20 {
				return nil, errors.New("invalid personal manifest asset")
			}
			if written[asset.SHA256] {
				continue
			}
			written[asset.SHA256] = true
			if err = writePersonalFile(filepath.Join(tmp, "assets", asset.SHA256+".bin"), asset.Data); err != nil {
				return nil, err
			}
		}
		if err = syncPersonalDir(filepath.Join(tmp, "assets")); err != nil {
			return nil, err
		}
	}
	if err = writePersonalFile(filepath.Join(tmp, "manifest.json"), encoded); err != nil {
		return nil, err
	}
	if err = syncPersonalDir(tmp); err != nil {
		return nil, err
	}
	if err = os.Rename(tmp, p); err != nil {
		if existing, readErr := s.read(a); readErr == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("personal manifest publish: %w", err)
	}
	if err = syncPersonalDir(filepath.Dir(p)); err != nil {
		return nil, err
	}
	return &m, nil
}

func syncPersonalDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
