//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalPublishedWikiInputRequiresCurrentCompleteReceipt(t *testing.T) {
	exerciseNativeResumeWithFormat(t, false, false, nil, nativeResumeFormatOptions{
		Format: "docx",
		AssertPublished: func(
			f *personalRecoveryFixture, source *personalExactDocument, proof types.PersonalExactReceipt,
		) {
			db := f.db.WithContext(context.Background())
			digest, err := repository.WikiPersonalPublication(db, 1, source.kb, source.knowledge)
			require.NoError(t, err)
			require.Len(t, digest, 64)
			for _, fault := range []string{
				"staging receipt", "wrong tenant", "changed binding", "disabled file",
				"changed chunk", "changed vector", "missing receipt",
			} {
				tx := db.Begin()
				require.NoError(t, tx.Error)
				tenant := uint64(1)
				switch fault {
				case "staging receipt":
					require.NoError(t, tx.Model(&types.TaskPendingOp{}).Where(
						"scope=? AND scope_id=?", types.PersonalExactReceiptScope, source.knowledge,
					).Update("op", "staging").Error)
				case "wrong tenant":
					tenant = 2
				case "changed binding":
					require.NoError(t, tx.Model(&types.ResourceBinding{}).Where("id=?", proof.BindingID).
						Update("owner_id", "other-document").Error)
				case "disabled file":
					require.NoError(t, tx.Model(&types.StoredResource{}).Where("id=?", proof.ResourceID).
						Update("state", "inactive").Error)
				case "changed chunk":
					require.NoError(t, tx.Exec("UPDATE chunks SET content=content||' changed' WHERE knowledge_id=?",
						source.knowledge).Error)
				case "changed vector":
					require.NoError(t, tx.Exec("UPDATE embeddings SET content=content||' changed' WHERE knowledge_id=?",
						source.knowledge).Error)
				case "missing receipt":
					require.NoError(t, tx.Where("scope=? AND scope_id=?",
						types.PersonalExactReceiptScope, source.knowledge).Delete(&types.TaskPendingOp{}).Error)
				}
				_, err := repository.WikiPersonalPublication(tx, tenant, source.kb, source.knowledge)
				require.ErrorIs(t, err, repository.ErrWikiMCPUnverified, fault)
				require.NoError(t, tx.Rollback().Error)
			}
			again, err := repository.WikiPersonalPublication(db, 1, source.kb, source.knowledge)
			require.NoError(t, err)
			require.Equal(t, digest, again)
		},
	})
}
