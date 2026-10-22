package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDocumentM3DerivedCacheChecksEverySource(t *testing.T) {
	f := newEnterpriseFixture(t)
	require.NoError(t, f.db.Exec(`INSERT INTO knowledges SELECT 'second',tenant_id,knowledge_base_id,type,parse_status,enable_status,channel,updated_at,deleted_at FROM knowledges WHERE id='doc'`).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO chunks SELECT 'second-chunk',tenant_id,knowledge_base_id,'second','Synthetic second source',1,true,'text',1,updated_at,deleted_at FROM chunks WHERE id='chunk'`).Error)
	f.approveClean(t)
	f.approve(t, "knowledge", "second")
	r := f.documentReader()
	ctx := documentCtx("A", 1)
	one, err := r.Authorize(ctx, "public-kb", "doc", types.DocumentRead)
	require.NoError(t, err)
	two, err := r.Authorize(ctx, "public-kb", "second", types.DocumentRead)
	require.NoError(t, err)
	proof, err := access.BuildDerivedText(ctx, []*access.DocumentReadGrant{one, two}, NewChunkRepository(f.db), func(rows []access.EnterpriseParsedChunk) (string, error) {
		require.Len(t, rows, 2)
		return rows[0].Content + "; " + rows[1].Content, nil
	})
	require.NoError(t, err)
	text, err := proof.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, text, "second source")
	require.NoError(t, f.db.Exec(`UPDATE knowledge_access_policies SET state='revoked',revision=revision+1 WHERE knowledge_id='second'`).Error)
	text, err = proof.Read(ctx)
	require.Error(t, err)
	require.Empty(t, text)
	var historical *access.DerivedText
	_, err = historical.Read(ctx)
	require.Error(t, err)
}

type containedFileBase struct {
	syntheticDocumentBase
	contained bool
}

func (b containedFileBase) CheckDocumentReadBase(ctx context.Context, tenant uint64, kb string, op types.DocumentOperation, public bool) error {
	if op == types.DocumentFile {
		if !b.contained {
			return access.ErrForbidden
		}
		op = types.DocumentRead
	}
	return b.syntheticDocumentBase.CheckDocumentReadBase(ctx, tenant, kb, op, public)
}

type syntheticResourceStore struct {
	ref    access.ControlledResource
	body   string
	onOpen func()
	opens  int
}

func (s *syntheticResourceStore) LoadControlledResource(context.Context, string) (*access.ControlledResource, error) {
	copy := s.ref
	return &copy, nil
}
func (s *syntheticResourceStore) OpenControlledResource(context.Context, *access.ControlledResource) (io.ReadCloser, error) {
	s.opens++
	if s.onOpen != nil {
		s.onOpen()
	}
	return io.NopCloser(strings.NewReader(s.body)), nil
}

func TestDocumentM3ResourceBrokerContainmentAndWithdrawal(t *testing.T) {
	for _, scenario := range []string{"allow", "legacy-uncontained", "withdraw-during-open", "rebind-during-open", "wrong-tenant", "oversize", "blob-overwrite", "unversioned"} {
		t.Run(scenario, func(t *testing.T) {
			f := newEnterpriseFixture(t)
			f.approveClean(t)
			f.documentPolicy(t, "doc", "restricted")
			_, err := f.subject(t, "B", types.DocumentFile, "approved", 0)
			require.NoError(t, err)
			base := containedFileBase{syntheticDocumentBase{f}, scenario != "legacy-uncontained"}
			r := access.NewDocumentReader(true, f.repo, base, f.employees)
			store := &syntheticResourceStore{ref: access.ControlledResource{ID: "opaque-test-resource", TenantID: 99, KBID: "public-kb", DocumentID: "doc", StorageReference: "synthetic-storage-object"}, body: "synthetic file bytes"}
			digest := sha256.Sum256([]byte(store.body))
			store.ref.ContentSHA256 = hex.EncodeToString(digest[:])
			version, _, err := resourceVersion(f.db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: "doc"})
			require.NoError(t, err)
			store.ref.DocumentVersion = version
			if scenario == "blob-overwrite" {
				store.body = "unreviewed replacement bytes"
			}
			if scenario == "unversioned" {
				store.ref.DocumentVersion = ""
			}
			if scenario == "withdraw-during-open" {
				store.onOpen = func() { _, err := f.subject(t, "B", types.DocumentFile, "revoked", 0); require.NoError(t, err) }
			}
			if scenario == "rebind-during-open" {
				store.onOpen = func() { store.ref.StorageReference = "rebound" }
			}
			if scenario == "wrong-tenant" {
				store.ref.TenantID = 100
			}
			if scenario == "oversize" {
				store.body = strings.Repeat("x", access.MaxControlledResourceBytes+1)
			}
			body, err := access.NewResourceBroker(true, r, store).Read(enterpriseWebContext("B"), "opaque-test-resource")
			if scenario == "allow" {
				require.NoError(t, err)
				require.Equal(t, "synthetic file bytes", string(body))
			} else {
				require.Error(t, err)
				require.Empty(t, body)
			}
			if scenario == "legacy-uncontained" {
				require.Zero(t, store.opens)
			}
		})
	}
}
