package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This reads real source and invokes the existing Go VLM client. The isolated
// artifact receipt is not a public Knowledge/index/RAG acceptance receipt.
func TestP4LiveBitableAttachmentManifest(t *testing.T) {
	runP4LiveTableManifest(t, "bitable")
}

func TestP4LiveSheetAttachmentManifest(t *testing.T) {
	runP4LiveTableManifest(t, "sheet")
}

func runP4LiveTableManifest(t *testing.T, kind string) {
	path := os.Getenv("FEISHU_P4_LIVE_CONFIG")
	modelPath := os.Getenv("FEISHU_P4_LIVE_MODEL")
	if path == "" || modelPath == "" {
		t.Skip("controlled live source and model config required")
	}
	var cfg struct {
		Token     string                  `json:"token"`
		Resources []core.PersonalResource `json:"resources"`
	}
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &cfg))
	var modelConfig vlm.Config
	b, err = os.ReadFile(modelPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &modelConfig))
	model, err := vlm.NewVLM(&modelConfig, nil)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = core.WithApplicationTables(core.WithApplicationIdentity(ctx, func(context.Context) (string, error) { return cfg.Token, nil }))
	cache := map[string]core.ApplicationAttachment{}
	ctx = core.WithApplicationAttachmentProcessor(ctx, func(call context.Context, a core.ApplicationAttachment) (core.ApplicationAttachment, error) {
		if previous, ok := cache[a.SHA256]; ok {
			a.Text, a.Parser = previous.Text, previous.Parser
			return a, nil
		}
		if a.MIME != "image/png" {
			return a, core.ErrApplicationTableMedia
		}
		ocr, err := model.Predict(call, [][]byte{a.Data}, buildVLMOCRPrompt("", types.VLMConfig{}))
		if err != nil {
			return a, err
		}
		caption, err := model.Predict(call, [][]byte{a.Data}, buildVLMCaptionPrompt(call, types.VLMConfig{}))
		if err != nil {
			return a, err
		}
		if strings.TrimSpace(caption) == "" {
			return a, core.ErrApplicationTableIncomplete
		}
		a.Text = fmt.Sprintf("Image description: %s\nOCR: %s", caption, sanitizeOCRText(ocr))
		a.Parser = "existing_vlm:" + model.GetModelID()
		cache[a.SHA256] = a
		return a, nil
	})
	for _, r := range cfg.Resources {
		if r.Type != kind {
			continue
		}
		c := core.NewClient(&core.Config{AppID: "p4-controlled-bitable-artifact"})
		snapshot, err := c.ReadApplicationTableCandidate(ctx, r)
		require.NoError(t, err)
		text, err := snapshot.IndexText()
		require.NoError(t, err)
		require.NotEmpty(t, snapshot.Attachments)
		store, err := newPersonalManifestStore(t.TempDir())
		require.NoError(t, err)
		a := &types.FeishuPersonalAttempt{ID: uuid.NewString(), ImportID: uuid.NewString(), CandidateVersionID: uuid.NewString(), Revision: r.Modified}
		a.ManifestKey = a.ID + "/" + a.CandidateVersionID + "/f1"
		manifest, err := store.put(a, []byte(text), "controlled-"+kind+".md", snapshot.Attachments...)
		require.NoError(t, err)
		_, err = store.read(a)
		require.NoError(t, err)
		records, fields := 0, 0
		for _, table := range snapshot.Tables {
			records += len(table.Records)
			fields += len(table.Fields)
		}
		t.Logf("RESULT=PRIVATE_TABLE_MANIFEST type=%s tables=%d fields=%d records=%d assets=%d bytes=%d manifest_sha256=%s; public Knowledge/index/RAG NOT_EXECUTED", kind, len(snapshot.Tables), fields, records, len(manifest.Assets), len(text), manifest.Hash)
		return
	}
	t.Fatal("controlled table sample missing")
}
