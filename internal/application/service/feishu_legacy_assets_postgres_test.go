package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type assetUploadFault struct{ interfaces.FileService }

type candidateVLM struct {
	failOCR bool
	before  func()
}

func (m *candidateVLM) GetModelName() string { return "candidate-test-vlm" }
func (m *candidateVLM) GetModelID() string   { return "candidate-test-vlm" }
func (m *candidateVLM) Predict(_ context.Context, _ [][]byte, prompt string) (string, error) {
	if m.before != nil {
		m.before()
	}
	if strings.Contains(prompt, "OCR assistant") {
		if m.failOCR {
			return "", errors.New("synthetic OCR failure")
		}
		return "imageocrcanary verified words", nil
	}
	return "imagecaptioncanary verified diagram", nil
}

type candidateModelService struct {
	interfaces.ModelService
	vision vlm.VLM
}

func (m *candidateModelService) GetVLMModel(context.Context, string) (vlm.VLM, error) {
	return m.vision, nil
}

func (f assetUploadFault) CheckStagingCapacity(context.Context, int64) error { return nil }
func (f assetUploadFault) SaveFile(context.Context, *multipart.FileHeader, uint64, string) (string, error) {
	return "", errors.New("synthetic image upload fault")
}

// Called by the existing real parser/ParadeDB harness for local and registered
// local storage, with both flat and parent-child chunking enabled.
func exerciseLegacyCandidateImages(ctx context.Context, t *testing.T, db *gorm.DB,
	ks *knowledgeService, ds *types.DataSource, failEmbedding func(bool),
) {
	t.Helper()
	kb, err := ks.kbService.GetKnowledgeBaseByID(ctx, ds.KnowledgeBaseID)
	require.NoError(t, err)
	oldVLM := kb.VLMConfig
	model := &types.Model{
		ID: uuid.NewString(), TenantID: ds.TenantID, Name: "candidate-test-vlm",
		Type: types.ModelTypeVLLM, Source: types.ModelSourceRemote,
	}
	if kb.ChunkingConfig.EnableParentChild {
		model.TenantID = types.DefaultBuiltinModelTenantID
		model.IsBuiltin = true
		require.NoError(t, db.FirstOrCreate(&types.Tenant{
			ID: types.DefaultBuiltinModelTenantID, Name: "isolated builtin models", Status: "active",
		}).Error)
	}
	require.NoError(t, db.Create(model).Error)
	fake := &candidateVLM{}
	oldModels := ks.modelService
	ks.modelService = &candidateModelService{ModelService: oldModels, vision: fake}
	defer func() {
		ks.modelService = oldModels
		b, e := json.Marshal(oldVLM)
		require.NoError(t, e)
		require.NoError(t, db.Model(kb).UpdateColumn("vlm_config", types.JSON(b)).Error)
	}()
	kb.VLMConfig = types.VLMConfig{Enabled: true, ModelID: model.ID}
	cfg, err := json.Marshal(kb.VLMConfig)
	require.NoError(t, err)
	require.NoError(t, db.Model(kb).UpdateColumn("vlm_config", types.JSON(cfg)).Error)
	var raw bytes.Buffer
	require.NoError(t, png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 80, 80))))
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw.Bytes())
	item := &types.FetchedItem{
		ExternalID: "candidate-image-document", FileName: "images.md", Title: "candidate image document",
		UpdatedAt: time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC),
		Content: []byte("# Asset canary\n\nimageoldcanary old knowledge.\n\n" +
			"![first](" + url + ")\n\n![repeat](" + url + ")"),
	}
	gate := repository.NewTenantRepository(types.EnableLegacyReadGuards(db)).(interface {
		CheckLegacyFileRead(context.Context, uint64, string) (bool, error)
	})
	fake.before = func() {
		fake.before = nil
		var privatePath string
		require.NoError(t, db.Raw("SELECT a.manifest->'assets'->0->>'path' FROM feishu_legacy_resources r "+
			"JOIN feishu_legacy_attempts a ON a.id=r.current_attempt WHERE r.data_source_id=? AND r.external_id=?",
			ds.ID, item.ExternalID).Scan(&privatePath).Error)
		require.NotEmpty(t, privatePath, "verified asset inventory is durable before OCR")
		_, readErr := gate.CheckLegacyFileRead(ctx, ds.TenantID, privatePath)
		require.ErrorIs(t, readErr, types.ErrLegacySourceRestricted, "uploaded candidate image is not public")
		if ks.resourceCatalog != nil {
			resource, e := ks.resourceCatalog.Resolve(ctx, privatePath)
			require.NoError(t, e)
			bound, e := repository.NewResourceRepository(db).IsReferencedByKnowledgeBase(
				ctx, ds.TenantID, kb.ID, resource.ID)
			require.NoError(t, e)
			require.False(t, bound, "upload did not bind to the stable knowledge")
		}
	}
	receipt, err := ks.StageFeishuLegacyItem(ctx, ds, item, nil)
	require.NoError(t, err)
	var attempt types.FeishuLegacyAttempt
	require.NoError(t, db.First(&attempt, "id=?", receipt.AttemptID).Error)
	var manifest types.FeishuLegacyManifest
	require.NoError(t, json.Unmarshal(attempt.Manifest, &manifest))
	require.Len(t, manifest.Assets, 1, "same bytes upload once per attempt")
	oldAsset := manifest.Assets[0]
	managed, err := gate.CheckLegacyFileRead(ctx, ds.TenantID, oldAsset.Path)
	require.NoError(t, err)
	require.True(t, managed)
	if ks.resourceCatalog != nil {
		resource, e := ks.resourceCatalog.Resolve(ctx, oldAsset.Path)
		require.NoError(t, e)
		bound, e := repository.NewResourceRepository(db).IsReferencedByKnowledgeBase(
			ctx, ds.TenantID, kb.ID, resource.ID)
		require.NoError(t, e)
		require.True(t, bound, "atomic publication bound the image to the real knowledge")
	}
	files := ks.resolveFileServiceForPath(ctx, kb, oldAsset.Path)
	read := func(p string) []byte {
		f, e := files.GetFile(ctx, p)
		require.NoError(t, e)
		defer func() { require.NoError(t, f.Close()) }()
		b, e := io.ReadAll(f)
		require.NoError(t, e)
		return b
	}
	require.Equal(t, raw.Bytes(), read(oldAsset.Path))
	var imageRows int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("knowledge_id=? AND chunk_type IN ?",
		receipt.KnowledgeID, []string{"image_ocr", "image_caption"}).Count(&imageRows).Error)
	require.Equal(t, int64(2), imageRows)
	var oldChunks string
	snapshotSQL := "SELECT json_agg(c ORDER BY id)::text FROM chunks c WHERE knowledge_id=? AND deleted_at IS NULL"
	require.NoError(t, db.Raw(snapshotSQL, receipt.KnowledgeID).Scan(&oldChunks).Error)
	item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
	item.Content = append(item.Content, []byte("\n\nOCR failure candidate")...)
	fake.failOCR = true
	_, err = ks.StageFeishuLegacyItem(ctx, ds, item, nil)
	fake.failOCR = false
	require.ErrorContains(t, err, "image_enrichment_failed")
	var kept types.FeishuLegacyResource
	require.NoError(t, db.First(&kept, "id=?", receipt.ResourceID).Error)
	require.Equal(t, receipt.AttemptID, kept.ActiveAttempt)
	item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
	item.Content = append(item.Content, []byte("\n\nexpired owner candidate")...)
	fake.before = func() {
		fake.before = nil
		expireSQL := "UPDATE feishu_legacy_attempts SET lease_until=clock_timestamp()-interval '1 second' " +
			"WHERE id=(SELECT current_attempt FROM feishu_legacy_resources WHERE id=?)"
		require.NoError(t, db.Exec(expireSQL, receipt.ResourceID).Error)
	}
	_, err = ks.StageFeishuLegacyItem(ctx, ds, item, nil)
	var fence *repository.LegacyFenceError
	require.ErrorAs(t, err, &fence)
	require.Equal(t, "attempt_lease_expired", fence.Reason)
	require.NoError(t, db.First(&kept, "id=?", receipt.ResourceID).Error)
	require.Equal(t, receipt.AttemptID, kept.ActiveAttempt)
	item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
	item.Content = append(item.Content, bytes.Repeat([]byte("\n\nmustnotpublish image content failure "), 2000)...)
	failEmbedding(true)
	_, err = ks.StageFeishuLegacyItem(ctx, ds, item, nil)
	failEmbedding(false)
	require.Error(t, err, "middle batch failure must reject the whole image version")
	var live types.FeishuLegacyResource
	require.NoError(t, db.First(&live, "id=?", receipt.ResourceID).Error)
	require.Equal(t, receipt.AttemptID, live.ActiveAttempt)
	require.NotEqual(t, live.ActiveAttempt, live.CurrentAttempt)
	var currentChunks string
	require.NoError(t, db.Raw(snapshotSQL, receipt.KnowledgeID).Scan(&currentChunks).Error)
	require.JSONEq(t, oldChunks, currentChunks)
	require.Equal(t, raw.Bytes(), read(oldAsset.Path))
	_, err = gate.CheckLegacyFileRead(ctx, ds.TenantID, oldAsset.Path)
	require.NoError(t, err)
	// The real image file was written before embedding, but its attempt is not
	// active. Even a guessed sibling path must fail the existing file gate.
	privatePath := "local://" + fmt.Sprint(ds.TenantID) + "/" + live.CurrentAttempt + "/candidate.png"
	_, err = gate.CheckLegacyFileRead(ctx, ds.TenantID, privatePath)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
	item.Content = []byte("# Asset canary\n\nimagenewcanary updated knowledge.\n\n![image](" + url + ")")
	updated, err := ks.StageFeishuLegacyItem(ctx, ds, item, nil)
	require.NoError(t, err)
	require.Equal(t, receipt.KnowledgeID, updated.KnowledgeID)
	var newer types.FeishuLegacyAttempt
	require.NoError(t, db.First(&newer, "id=?", updated.AttemptID).Error)
	require.NoError(t, json.Unmarshal(newer.Manifest, &manifest))
	require.Len(t, manifest.Assets, 1)
	require.NotEqual(t, oldAsset.Path, manifest.Assets[0].Path)
	_, err = gate.CheckLegacyFileRead(ctx, ds.TenantID, oldAsset.Path)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.Equal(t, raw.Bytes(), read(oldAsset.Path), "superseded bytes retained")
	_, err = gate.CheckLegacyFileRead(ctx, ds.TenantID, manifest.Assets[0].Path)
	require.NoError(t, err)
	var hits int64
	require.NoError(t, db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? "+
		"AND content ||| 'imagenewcanary'", receipt.KnowledgeID).Scan(&hits).Error)
	require.Positive(t, hits)
	// A newer revision cannot steal a live candidate lease. Once that candidate
	// finishes, a normal retry publishes the newer complete image graph.
	outer := *item
	outer.UpdatedAt = outer.UpdatedAt.Add(time.Minute)
	outer.Content = append(append([]byte{}, item.Content...), []byte("\n\nobsolete overlapping update")...)
	winner := *item
	winner.UpdatedAt = outer.UpdatedAt.Add(time.Minute)
	winner.Content = append(append([]byte{}, item.Content...), []byte("\n\nwinner overlapping update")...)
	var winning *types.FeishuLegacyReceipt
	fake.before = func() {
		fake.before = nil
		var publishErr error
		winning, publishErr = ks.StageFeishuLegacyItem(ctx, ds, &winner, nil)
		require.ErrorIs(t, publishErr, repository.ErrLegacyVersionBusy)
	}
	outerReceipt, err := ks.StageFeishuLegacyItem(ctx, ds, &outer, nil)
	require.NoError(t, err)
	winning, err = ks.StageFeishuLegacyItem(ctx, ds, &winner, nil)
	require.NoError(t, err)
	require.NotNil(t, winning)
	require.NotEqual(t, outerReceipt.AttemptID, winning.AttemptID)
	require.NoError(t, db.First(&kept, "id=?", receipt.ResourceID).Error)
	require.Equal(t, winning.AttemptID, kept.ActiveAttempt)
	var winningAttempt types.FeishuLegacyAttempt
	require.NoError(t, db.First(&winningAttempt, "id=?", winning.AttemptID).Error)
	var winningManifest types.FeishuLegacyManifest
	require.NoError(t, json.Unmarshal(winningAttempt.Manifest, &winningManifest))
	winningAsset := winningManifest.Assets[0].Path
	// Isolated authorization fault injection: no version or file is changed.
	require.NoError(t, db.Model(&types.FeishuLegacyResource{}).Where("id=?", kept.ID).Update("state", "revoked").Error)
	_, err = gate.CheckLegacyFileRead(ctx, ds.TenantID, winningAsset)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.NoError(t, db.Model(&types.FeishuLegacyResource{}).Where("id=?", kept.ID).Update("state", "active").Error)
	_, err = gate.CheckLegacyFileRead(ctx, ds.TenantID, winningAsset)
	require.NoError(t, err)
	require.Equal(t, raw.Bytes(), read(oldAsset.Path), "even concurrent replacement retains old image bytes")
}
