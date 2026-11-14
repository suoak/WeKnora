package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalExactResumeImagesReusesOnlyMatchingOwnedBytes(t *testing.T) {
	for _, change := range []string{
		"valid", "changed_bytes", "other_document", "other_tenant",
		"missing", "ambiguous",
	} {
		t.Run(change, func(t *testing.T) {
			f, a, _ := pendingExactFixture(t)
			data := []byte("independently checked embedded image bytes")
			path, err := f.ks.fileSvc.SaveBytes(context.Background(), data, 1, "resume-image.png", false)
			require.NoError(t, err)
			sum := sha256.Sum256(data)
			row := types.StoredResource{
				ID: "image", Handle: "imageabcdefghijklmnopq", TenantID: 1,
				Provider: "local", PhysicalPath: path, Size: int64(len(data)), ContentHash: hex.
						EncodeToString(sum[:]),
				LocationHash: "image-location", State: "active", Lifecycle: "persistent",
			}
			require.NoError(t, f.db.Create(&row).Error)
			binding := types.ResourceBinding{
				ID: "image-binding", ResourceID: row.ID, TenantID: 1,
				OwnerType: types.ResourceOwnerKnowledge, OwnerID: a.LogicalKnowledgeID, Relation: types.
						ResourceRelationExtractedImage,
			}
			require.NoError(t, f.db.Create(&binding).Error)
			switch change {
			case "changed_bytes":
				data[0] = 'X'
			case "other_document":
				require.NoError(
					t,
					f.db.Exec("UPDATE resource_bindings SET owner_id='other' WHERE id=?", binding.ID).Error,
				)
			case "other_tenant":
				require.NoError(
					t,
					f.db.Exec("UPDATE resource_bindings SET tenant_id=2 WHERE id=?", binding.ID).Error,
				)
			case "missing":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE resources SET physical_path='local://missing' WHERE id=?",
						row.ID,
					).Error,
				)
			case "ambiguous":
				row.ID, row.Handle, row.LocationHash = "image-two", "imagetwoabcdefghijklmn", "image-two-location"
				require.NoError(t, f.db.Create(&row).Error)
				binding.ID, binding.ResourceID = "image-two-binding", row.ID
				require.NoError(t, f.db.Create(&binding).Error)
			}
			files := &exactResumeImageFiles{FileService: f.ks.fileSvc, consumer: &exactPersonalConsumer{
				recovery: f.p, source: &personalExactDocument{tenant: 1, knowledge: a.
						LogicalKnowledgeID}, resuming: true,
			}}
			url, err := files.SaveBytes(context.Background(), data, 1, "unused.png", false)
			if change == "valid" {
				require.NoError(t, err)
				require.Equal(t, types.BuildResourcePath(row.Handle), url)
			} else {
				require.Error(t, err)
				require.Empty(t, url)
			}
		})
	}
}

func TestPersonalExactResumeEmptyImagesRequireCurrentProofAndNoOutput(t *testing.T) {
	for _, change := range []string{"valid", "retained_chunk", "retained_vector", "stale_owner"} {
		t.Run(change, func(t *testing.T) {
			f, a, intent := pendingExactFixture(t)
			renameExactProtocolFixture(t, f, a, &intent, "image.docx", "docx")
			owner, err := f.p.repo.Claim(context.Background(), a.ID, f.p.lease)
			require.NoError(t, err)
			proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
			require.NoError(t, err)
			work, err := f.p.exactDocumentExecutionContext(context.Background(), proof)
			require.NoError(t, err)
			consumer := &exactPersonalConsumer{recovery: f.p, source: proof, resuming: true, resumeEmpty: true}
			switch change {
			case "retained_chunk":
				require.NoError(t, f.db.Exec("INSERT INTO chunks(id,knowledge_id) VALUES('old',?)", a.LogicalKnowledgeID).Error)
			case "retained_vector":
				require.NoError(t, f.db.Exec("INSERT INTO embeddings(knowledge_id) VALUES(?)", a.LogicalKnowledgeID).Error)
			case "stale_owner":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET owner_nonce='changed' WHERE id=?", a.ID).Error)
			}
			files := &exactResumeImageFiles{FileService: f.ks.fileSvc, consumer: consumer}
			data := []byte("checked image")
			url, err := files.SaveBytes(work, data, 1, "fresh.png", false)
			if change != "valid" {
				require.Error(t, err)
				require.Empty(t, url)
				return
			}
			require.NoError(t, err)
			r, err := f.ks.fileSvc.GetFile(work, url)
			require.NoError(t, err)
			defer r.Close()
			actual, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, data, actual)
		})
	}
}
