//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPersonalExactStandaloneXLSXPublishesAfterLocalAuthorizationFailure(t *testing.T) {
	t.Setenv(repository.PersonalExactStandaloneXLSXEnv, "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX", "1")
	exerciseNativeResumeWithFormat(t, true, false, standaloneTestWorkbook(t, nil), nativeResumeFormatOptions{
		Format: "xlsx", AuthorizationFailure: true,
	})
}

func TestPersonalExactStandaloneXLSXAuthorizationResumeRemainsClosed(t *testing.T) {
	faults := []string{"chunk", "vector", "image binding", "workspace disabled", "source hash", "feature off"}
	for _, fault := range faults {
		t.Run(fault, func(t *testing.T) {
			t.Setenv(repository.PersonalExactStandaloneXLSXEnv, "1")
			t.Setenv("FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX", "1")
			exerciseNativeResumeWithFormat(t, true, false, standaloneTestWorkbook(t, nil), nativeResumeFormatOptions{
				Format: "xlsx", AuthorizationFailure: true,
				AssertClosed: func(
					f *personalRecoveryFixture, observed *types.FeishuPersonalAttempt, calls *atomic.Int64,
				) {
					// Historical fault injection is confined to the disposable schema.
					// No successful publication is fabricated and the actual resume
					// checks, model guards and CAS remain enabled for the assertion.
					tx := f.db.Begin()
					require.NoError(t, tx.Error)
					require.NoError(t, tx.Exec("SET LOCAL session_replication_role=replica").Error)
					switch fault {
					case "chunk":
						require.NoError(t, tx.Create(&types.Chunk{
							ID: uuid.NewString(), TenantID: 1, KnowledgeID: observed.LogicalKnowledgeID,
							KnowledgeBaseID: "kb", ChunkType: types.ChunkTypeText, Content: "unproved old output",
						}).Error)
					case "vector":
						require.NoError(t, tx.Exec("INSERT INTO embeddings(source_id,source_type,dimension,"+
							"knowledge_id,is_enabled) VALUES(?,0,1024,?,false)",
							uuid.NewString(), observed.LogicalKnowledgeID).Error)
					case "image binding":
						resource := types.StoredResource{
							ID: uuid.NewString(), Handle: "unprovedabcdefghijklmn", TenantID: 1, Provider: "local",
							PhysicalPath: "local://1/unproved.png", LocationHash: uuid.NewString(),
							State: types.ResourceStateActive, Lifecycle: types.ResourceLifecyclePersistent,
						}
						require.NoError(t, tx.Create(&resource).Error)
						require.NoError(t, tx.Create(&types.ResourceBinding{
							ID: uuid.NewString(), TenantID: 1, ResourceID: resource.ID,
							OwnerType: types.ResourceOwnerKnowledge, OwnerID: observed.LogicalKnowledgeID,
							Relation: types.ResourceRelationExtractedImage,
						}).Error)
					case "workspace disabled":
						require.NoError(t, tx.Exec("UPDATE tenants SET status='disabled' WHERE id=1").Error)
					case "source hash":
						require.NoError(t, tx.Exec("UPDATE resources SET content_hash=repeat('a',64)").Error)
					case "feature off":
						t.Setenv(repository.PersonalExactStandaloneXLSXEnv, "0")
					}
					require.NoError(t, tx.Commit().Error)
					before := exactPendingSnapshot(t, f, observed.ID)
					err := f.p.prepareExactPersonalResume(context.Background(), observed)
					if fault == "feature off" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, observed.ID)))
					require.Zero(t, calls.Load(), "a refused resume must not call an embedding model")
				},
			})
		})
	}
}
