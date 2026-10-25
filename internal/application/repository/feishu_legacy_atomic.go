package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
	"time"

	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlog "gorm.io/gorm/logger"
)

// ErrLegacyVersionFence means source ownership or publication integrity was not confirmed.
var ErrLegacyVersionFence = errors.New("Feishu source version ownership lost or unconfirmed")

// ErrLegacyVersionBusy indicates that another candidate still owns a live lease.
var ErrLegacyVersionBusy = errors.New("Feishu source version already being prepared")

func legacySourceKind(kind string) bool {
	return kind == "feishu" || kind == "feishu_drive" || kind == "lark" || kind == "lark_drive"
}

type FeishuLegacyStore struct{ db *gorm.DB }

func NewFeishuLegacyStore(db *gorm.DB) *FeishuLegacyStore {
	return &FeishuLegacyStore{db: db.Session(&gorm.Session{Logger: gormlog.Discard})}
}

func FeishuLegacyStoreFor(repo interfaces.KnowledgeRepository) (*FeishuLegacyStore, error) {
	r, ok := repo.(*knowledgeRepository)
	if !ok {
		return nil, legacyFence("repository_not_supported")
	}
	return NewFeishuLegacyStore(r.db), nil
}

func LegacyManifestHash(m types.FeishuLegacyManifest) (string, error) {
	m.SHA256 = ""
	b, e := json.Marshal(m)
	if e != nil {
		return "", e
	}
	// PostgreSQL jsonb changes object key order inside RawMessage fields.
	// Hash the semantic JSON tree, not the incidental serialized key order.
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	var semantic any
	if e = decoder.Decode(&semantic); e != nil {
		return "", e
	}
	b, e = json.Marshal(semantic)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func LegacyKBConfigHash(kb *types.KnowledgeBase) string {
	copy := *kb
	copy.EnsureDefaults()
	kb = &copy
	b, _ := json.Marshal(
		[]any{
			kb.ChunkingConfig,
			kb.EmbeddingModelID,
			kb.VectorStoreID,
			kb.Type,
			kb.ImageProcessingConfig,
			kb.VLMConfig,
			kb.QuestionGenerationConfig,
			kb.IndexingStrategy,
			kb.StorageBackendID,
			kb.StorageProviderConfig,
		},
	)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func legacyResourceID(ds *types.DataSource, external string) string {
	b, _ := json.Marshal([]any{ds.TenantID, ds.KnowledgeBaseID, ds.ID, external})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func legacyConfigEqual(left, right []byte) bool {
	normalize := func(raw []byte) ([]byte, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	l, le := normalize(left)
	r, re := normalize(right)
	return le == nil && re == nil && bytes.Equal(l, r)
}

func (s *FeishuLegacyStore) ModelStamp(ctx context.Context, id string) (string, error) {
	var hash string
	err := s.db.WithContext(ctx).
		Raw("SELECT md5(row_to_json(m)::text) FROM models m WHERE id=? AND deleted_at IS NULL", id).
		Scan(&hash).
		Error
	if err == nil && hash == "" {
		err = legacyFence("model_missing")
	}
	return hash, err
}

func (s *FeishuLegacyStore) StorageStamp(ctx context.Context, tenant uint64, id string) (string, error) {
	var stamp string
	err := s.db.WithContext(ctx).
		Raw("SELECT md5(row_to_json(b)::text) FROM storage_backends b WHERE id=? AND "+
			"tenant_id=? AND provider='local' AND status='active' AND deleted_at IS NULL", id, tenant).
		Scan(&stamp).
		Error
	if err == nil && stamp == "" {
		err = legacyFence("storage_binding_unavailable")
	}
	return stamp, err
}

// CheckOwner verifies the execution claim and candidate generation/lease.
func (s *FeishuLegacyStore) CheckOwner(
	ctx context.Context,
	ds *types.DataSource,
	owner *types.FeishuLegacyAttempt,
) error {
	return s.db.WithContext(ctx).
		Transaction(func(tx *gorm.DB) error { _, _, err := s.owned(ctx, tx, ds, owner); return err })
}

// Lock ordering matches sync execution: datasource, synclog, resource, knowledge.
// The row lock is retained through the actual public index write, not released
// after a preflight check. Database triggers protect old unmodified workers too.
func (s *FeishuLegacyStore) execution(tx *gorm.DB, ctx context.Context, ds *types.DataSource) error {
	if err := s.executionFence(ctx, tx, ds); err != nil {
		return err
	}
	var config types.DataSourceConfig
	if len(ds.Config) > 0 && json.Unmarshal(ds.Config, &config) != nil {
		return legacyFence("source_configuration_invalid")
	}
	ref, err := types.ParseFeishuSourceReference(&config)
	if err != nil {
		return err
	}
	if ref != nil {
		guard, ok := types.FeishuSourceGuardFromContext(ctx)
		if !ok {
			return legacyFence("credential_reference_unvalidated")
		}
		return guard(tx)
	}
	return nil
}

// Restrictive operations retain the same configuration, generation and claim
// fence, while remaining possible after the remote credential is withdrawn.
// Restoring access and publishing still require execution's credential guard.
func (s *FeishuLegacyStore) executionFence(ctx context.Context, tx *gorm.DB, ds *types.DataSource) error {
	e, ok := types.SyncExecutionFromContext(ctx)
	if !ok || e.DataSourceID != ds.ID || e.TenantID != ds.TenantID || tx.Dialector.Name() != "postgres" {
		return legacyFence("execution_context_invalid")
	}
	current, err := lockSyncDataSource(tx, ds.ID, ds.TenantID)
	if err != nil {
		return err
	}
	if current.Status == types.DataSourceStatusPaused || current.KnowledgeBaseID != ds.KnowledgeBaseID ||
		current.Type != ds.Type ||
		!legacyConfigEqual(current.Config, ds.Config) {
		return legacyFence("source_configuration_changed_or_paused")
	}
	var activeTenant bool
	if err := tx.Raw("SELECT EXISTS(SELECT 1 FROM tenants t JOIN knowledge_bases k ON "+
		"k.tenant_id=t.id WHERE t.id=? AND k.id=? AND t.status='active' AND "+
		"t.deleted_at IS NULL AND k.deleted_at IS NULL)",
		ds.TenantID,
		ds.KnowledgeBaseID).Scan(&activeTenant).Error; err != nil {
		return err
	}
	if !activeTenant {
		return legacyFence("tenant_or_kb_inactive")
	}
	var log types.SyncLog
	err = ownedSyncLog(tx, e).Clauses(clause.Locking{Strength: "UPDATE"}).First(&log).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return legacyFence("execution_generation_or_claim_lost")
	}
	if err != nil {
		return err
	}
	return err
}

// Begin claims a private candidate after checking the source revision watermark.
func (s *FeishuLegacyStore) Begin(
	ctx context.Context,
	ds *types.DataSource,
	item *types.FetchedItem,
	sourceHash string,
) (r *types.FeishuLegacyResource, a *types.FeishuLegacyAttempt, err error) {
	return s.BeginWithSourceIdentity(ctx, ds, item, sourceHash, nil, nil)
}

// BeginWithSourceIdentity preserves raw hashes while admitting proven canonical package equivalence.
func (s *FeishuLegacyStore) BeginWithSourceIdentity(
	ctx context.Context, ds *types.DataSource, item *types.FetchedItem, sourceHash string,
	identity *types.FeishuLegacySourceIdentity, proof *LegacySourceIdentityProof,
) (r *types.FeishuLegacyResource, a *types.FeishuLegacyAttempt, err error) {
	if item.ExternalID == "" || item.UpdatedAt.IsZero() || len(sourceHash) != 64 {
		return nil, nil, legacyFence("invalid_source_revision")
	}
	if !legacyIdentityValid(identity) {
		return nil, nil, legacyFence("source_identity_algorithm_unconfirmed")
	}
	if identity != nil && (item.Metadata["obj_type"] != "docx" || item.Metadata["attachment"] == "true" ||
		!strings.EqualFold(path.Ext(item.FileName), ".docx")) {
		return nil, nil, legacyFence("source_identity_path_unconfirmed")
	}
	if identity != nil {
		copyIdentity := *identity
		identity = &copyIdentity
	}
	r = &types.FeishuLegacyResource{}
	a = &types.FeishuLegacyAttempt{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := s.execution(tx, ctx, ds); e != nil {
			return e
		}
		id := legacyResourceID(ds, item.ExternalID)
		lockIdentity := fmt.Sprintf("%d:%s:%s:%s", ds.TenantID, ds.KnowledgeBaseID, ds.ID, item.ExternalID)
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", lockIdentity).Error; e != nil {
			return e
		}
		// Enrollment is serialized with identity lookup and never allocates a
		// second KID on retry. The source execution row is already locked.
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(r, "id = ?", id).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			var old []types.Knowledge
			e = tx.Where("tenant_id=? AND knowledge_base_id=? AND metadata->>'datasource_id'=? AND "+
				"metadata->>'external_id'=?", ds.TenantID, ds.KnowledgeBaseID, ds.ID, item.ExternalID).
				Limit(2).
				Find(&old).
				Error
			if e != nil {
				return e
			}
			if len(old) > 1 {
				return legacyFence("source_identity_ambiguous")
			}
			kid := uuid.NewString()
			state := "hold"
			if len(old) == 0 {
				// Successful source fetch, no historical public knowledge:
				// staging progress is not an access-withdrawal event. This
				// resource still has no public document/index until Publish.
				state = "active"
			}
			if len(old) == 1 {
				kid = old[0].ID
				state = "active"
			}
			if e = tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "feishu-kid:"+kid).Error; e != nil {
				return e
			}
			*r = types.FeishuLegacyResource{
				ID:              id,
				TenantID:        ds.TenantID,
				KnowledgeBaseID: ds.KnowledgeBaseID,
				DataSourceID:    ds.ID,
				ExternalID:      item.ExternalID,
				KnowledgeID:     kid,
				State:           state,
			}
			r.QuarantineSnapshot = json.RawMessage(`{}`)
			if len(old) == 1 {
				r.OriginalFilePath = old[0].FilePath
			}
			if e = tx.Create(r).Error; e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if r.State == "revoked" {
			return legacyFence("resource_revoked")
		}
		if r.SourceRevision != "" {
			previous, parse := time.Parse(time.RFC3339Nano, r.SourceRevision)
			if parse != nil || item.UpdatedAt.Before(previous) {
				return legacyFence("source_revision_regressed")
			}
		}
		// A high-water timestamp must bind to one content identity even when
		// the last candidate failed before any version was ever published.
		if r.SourceRevision != "" {
			highWater, _ := time.Parse(time.RFC3339Nano, r.SourceRevision)
			if item.UpdatedAt.Equal(highWater) {
				expectedHash := ""
				var expectedAttempt *types.FeishuLegacyAttempt
				if r.CurrentAttempt != "" {
					var observed types.FeishuLegacyAttempt
					if e = tx.First(&observed, "id=?", r.CurrentAttempt).Error; e != nil {
						return e
					}
					observedTime, parseObserved := time.Parse(time.RFC3339Nano, observed.Revision)
					if parseObserved != nil {
						return legacyFence("invalid_source_revision")
					}
					var restricted struct {
						Restricted bool `json:"restricted"`
					}
					withdrawal := r.State == "hold" && observed.Status == "published" &&
						observed.SourceHash == "withdrawal" &&
						json.Unmarshal(observed.Receipt, &restricted) == nil &&
						restricted.Restricted
					// A trusted redaction receipt is not a new content observation.
					// Revalidation must still match the active content watermark.
					if observedTime.Equal(highWater) && !withdrawal {
						expectedHash = observed.SourceHash
						expectedAttempt = &observed
					}
				}
				// Duplicate active bytes may advance only SourceRevision; a
				// current attempt at an older revision cannot bind that watermark.
				if expectedHash == "" && r.ActiveAttempt != "" {
					var active types.FeishuLegacyAttempt
					if e = tx.First(&active, "id=?", r.ActiveAttempt).Error; e != nil {
						return e
					}
					expectedHash = active.SourceHash
					expectedAttempt = &active
				}
				if expectedHash == "" {
					return legacyFence("source_revision_hash_unconfirmed")
				}
				if expectedHash != sourceHash {
					matched, matchErr := legacyAttemptSourceMatches(tx, r, expectedAttempt, sourceHash, identity, proof)
					if matchErr != nil {
						return matchErr
					}
					if !matched {
						if identity != nil && proof == nil {
							var priorManifest types.FeishuLegacyManifest
							if json.Unmarshal(expectedAttempt.Manifest, &priorManifest) == nil &&
								priorManifest.SourceIdentity == nil {
								return legacyFence("source_identity_legacy_proof_required")
							}
						}
						return legacyFence("source_revision_content_conflict")
					}
				}
			}
		}
		// Even unchanged active bytes cannot advance the watermark while a
		// different live candidate may still publish at an older revision.
		if r.CurrentAttempt != "" {
			var prior types.FeishuLegacyAttempt
			if e = tx.First(&prior, "id=?", r.CurrentAttempt).Error; e != nil {
				return e
			}
			if prior.Status == "staging" || prior.Status == "ready" {
				var now time.Time
				if e = tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; e != nil {
					return e
				}
				if prior.LeaseUntil.After(now) {
					return ErrLegacyVersionBusy
				}
				if e = tx.Model(&prior).Update("status", "obsolete").Error; e != nil {
					return e
				}
			}
		}
		if r.ActiveAttempt != "" {
			var active types.FeishuLegacyAttempt
			if e = tx.First(&active, "id=?", r.ActiveAttempt).Error; e != nil {
				return e
			}
			previous, parse := time.Parse(time.RFC3339Nano, active.Revision)
			if parse != nil {
				return parse
			}
			if item.UpdatedAt.Before(previous) {
				return legacyFence("source_revision_regressed")
			}
			matched, matchErr := legacyAttemptSourceMatches(tx, r, &active, sourceHash, identity, proof)
			if matchErr != nil {
				return matchErr
			}
			if matched && r.State == "active" {
				r.SourceRevision = item.UpdatedAt.UTC().Format(time.RFC3339Nano)
				if e = tx.Model(r).Update("source_revision", r.SourceRevision).Error; e != nil {
					return e
				}
				*a = active
				return nil
			}
		}

		var old types.Knowledge
		e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&old, "id=? AND tenant_id=?", r.KnowledgeID, r.TenantID).
			Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		r.Generation++
		r.SourceRevision = item.UpdatedAt.UTC().Format(time.RFC3339Nano)
		r.CurrentAttempt = uuid.NewString()
		r.UpdatedAt = time.Now().UTC()
		initial := types.FeishuLegacyManifest{SourceIdentity: identity}
		if identity != nil {
			initial.SHA256, e = LegacyManifestHash(initial)
			if e != nil {
				return e
			}
		}
		initialManifest, e := json.Marshal(initial)
		if e != nil {
			return e
		}
		*a = types.FeishuLegacyAttempt{
			ID:           r.CurrentAttempt,
			ResourceID:   r.ID,
			Generation:   r.Generation,
			OwnerNonce:   uuid.NewString(),
			Status:       "staging",
			Revision:     item.UpdatedAt.UTC().Format(time.RFC3339Nano),
			SourceHash:   sourceHash,
			ExpectedHash: old.FileHash,
			Manifest:     initialManifest,
			Receipt:      json.RawMessage(`{}`),
		}
		if e = tx.Raw("SELECT clock_timestamp() + interval '20 minutes'").Scan(&a.LeaseUntil).Error; e != nil {
			return e
		}
		if e = tx.Save(r).Error; e != nil {
			return e
		}
		return tx.Create(a).Error
	})
	return
}

func (s *FeishuLegacyStore) owned(
	ctx context.Context,
	tx *gorm.DB,
	ds *types.DataSource,
	owner *types.FeishuLegacyAttempt,
) (*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, error) {
	if err := s.execution(tx, ctx, ds); err != nil {
		return nil, nil, err
	}
	var r types.FeishuLegacyResource
	var a types.FeishuLegacyAttempt
	var identity types.FeishuLegacyResource
	if err := tx.First(&identity,
		"id=? AND data_source_id=? AND tenant_id=? AND knowledge_base_id=?",
		owner.ResourceID,
		ds.ID,
		ds.TenantID,
		ds.KnowledgeBaseID).Error; err != nil {
		return nil, nil, err
	}
	key := fmt.Sprintf(
		"%d:%s:%s:%s",
		identity.TenantID,
		identity.KnowledgeBaseID,
		identity.DataSourceID,
		identity.ExternalID,
	)
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error; err != nil {
		return nil, nil, err
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))",
		"feishu-kid:"+identity.KnowledgeID).Error; err != nil {
		return nil, nil, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "id=?", owner.ResourceID).Error; err != nil {
		return nil, nil, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, "id=?", owner.ID).Error; err != nil {
		return nil, nil, err
	}
	var valid bool
	if err := tx.Raw("SELECT ?::timestamptz > clock_timestamp()", a.LeaseUntil).Scan(&valid).Error; err != nil {
		return nil, nil, err
	}
	if !valid {
		return nil, nil, legacyFence("attempt_lease_expired")
	}
	if r.CurrentAttempt != a.ID || r.Generation != a.Generation || owner.Generation != a.Generation ||
		owner.OwnerNonce != a.OwnerNonce ||
		r.State == "revoked" ||
		(a.Status != "staging" && a.Status != "ready") {
		return nil, nil, legacyFence("attempt_generation_or_owner_changed")
	}
	return &r, &a, nil
}

// Prepare persists a complete version manifest and child-only index candidates.
func (s *FeishuLegacyStore) Prepare(
	ctx context.Context,
	ds *types.DataSource,
	owner *types.FeishuLegacyAttempt,
	m types.FeishuLegacyManifest,
	vectors [][]float32,
) error {
	if len(m.Chunks) == 0 || len(m.Chunks) > 10000 || m.Dimension < 1 || m.Dimension > 4096 {
		return legacyFence("manifest_shape_invalid")
	}
	indexed, err := legacyIndexedChunks(m.Chunks)
	if err != nil {
		return err
	}
	if len(indexed) != len(vectors) {
		return legacyFence("vector_count_mismatch")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, a, e := s.owned(ctx, tx, ds, owner)
		if e != nil {
			return e
		}
		if a.Status != "staging" {
			return legacyFence("attempt_not_staging")
		}
		var initial types.FeishuLegacyManifest
		if e = json.Unmarshal(a.Manifest, &initial); e != nil {
			return e
		}
		if initial.SourceIdentity != nil {
			hash, hashErr := LegacyManifestHash(initial)
			if hashErr != nil || hash != initial.SHA256 {
				return legacyFence("source_identity_manifest_unconfirmed")
			}
		}
		if m.SourceIdentity != nil && !legacyIdentityEqual(m.SourceIdentity, initial.SourceIdentity) {
			return legacyFence("source_identity_manifest_changed")
		}
		m.SourceIdentity = initial.SourceIdentity
		h, e := LegacyManifestHash(m)
		if e != nil {
			return e
		}
		m.SHA256 = h
		if m.Knowledge.ID != r.KnowledgeID || m.Knowledge.TenantID != r.TenantID ||
			m.Knowledge.KnowledgeBaseID != r.KnowledgeBaseID {
			return legacyFence("manifest_document_mismatch")
		}
		metadata := m.Knowledge.GetMetadata()
		if metadata["datasource_id"] != r.DataSourceID || metadata["external_id"] != r.ExternalID {
			return legacyFence("manifest_source_mismatch")
		}
		if e = validateLegacyAssets(tx, r, a, &m, false); e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, c := range m.Chunks {
			if c == nil || seen[c.ID] || c.ID == "" || c.KnowledgeID != r.KnowledgeID ||
				c.KnowledgeBaseID != r.KnowledgeBaseID ||
				c.TenantID != r.TenantID {
				return legacyFence("chunk_document_mismatch")
			}
			seen[c.ID] = true
		}
		for i, c := range indexed {
			if len(vectors[i]) != m.Dimension {
				return legacyFence("vector_dimension_mismatch")
			}
			var norm float64
			for _, v := range vectors[i] {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || math.Abs(float64(v)) > 65504 {
					return legacyFence("vector_value_invalid")
				}
				norm += float64(v) * float64(v)
			}
			if norm == 0 {
				return legacyFence("vector_zero_norm")
			}
			c.ContextHeader = m.ChunkHeaders[c.ID]
			if e = tx.Exec("INSERT INTO feishu_legacy_index_candidates(attempt_id,chunk_id,co"+
				"ntent,dimension,embedding) VALUES(?,?,?,?,?)",
				a.ID,
				c.ID,
				buildLegacyIndexContent(m.Knowledge.Title,
					c.EmbeddingContent()),
				m.Dimension,
				pgvector.NewHalfVector(vectors[i])).Error; e != nil {
				return e
			}
		}
		b, e := json.Marshal(m)
		if e != nil {
			return e
		}
		return tx.Model(a).Updates(map[string]any{"manifest": json.RawMessage(b), "status": "ready"}).Error
	})
}
func buildLegacyIndexContent(title, body string) string { return title + "\n\n" + body }

// Publish updates public Chunk, vector and pg_search relations in the SAME
// database transaction. Public retrieval never consumes private candidates.
func (s *FeishuLegacyStore) Publish(
	ctx context.Context,
	ds *types.DataSource,
	owner *types.FeishuLegacyAttempt,
) (*types.FeishuLegacyReceipt, error) {
	if err := types.CheckFeishuSourceRead(ctx); err != nil {
		return nil, err
	}
	var receipt types.FeishuLegacyReceipt
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, a, e := s.owned(ctx, tx, ds, owner)
		if e != nil {
			return e
		}
		if a.Status != "ready" {
			return legacyFence("attempt_not_ready")
		}
		var m types.FeishuLegacyManifest
		if e = json.Unmarshal(a.Manifest, &m); e != nil {
			return e
		}
		h, e := LegacyManifestHash(m)
		if e != nil || h != m.SHA256 {
			return fmt.Errorf(
				"feishu candidate manifest integrity mismatch: %w",
				legacyFence("manifest_integrity_mismatch"),
			)
		}
		var kb types.KnowledgeBase
		if e = tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&kb,
			"id=? AND tenant_id=?",
			r.KnowledgeBaseID,
			r.TenantID).Error; e != nil {
			return e
		}
		if kb.EmbeddingModelID != m.EmbeddingModelID || LegacyKBConfigHash(&kb) != m.KBConfigSHA256 ||
			kb.VectorStoreID != nil ||
			kb.Type != types.KnowledgeBaseTypeDocument {
			return fmt.Errorf(
				"feishu KB configuration or model binding changed: %w",
				legacyFence("kb_configuration_changed"),
			)
		}
		var stamp string
		if e = tx.Raw("SELECT md5(row_to_json(m)::text) FROM models m WHERE id=? AND deleted_at "+
			"IS NULL FOR SHARE", m.EmbeddingModelID).Scan(&stamp).Error; e != nil {
			return e
		}
		if stamp == "" || stamp != m.ModelStamp {
			return fmt.Errorf(
				"feishu embedding model configuration changed: %w",
				legacyFence("model_configuration_changed"),
			)
		}
		if m.VLMModelID != "" {
			var vlmStamp string
			e = tx.Raw("SELECT md5(row_to_json(m)::text) FROM models m WHERE id=? "+
				"AND (tenant_id=? OR is_builtin=true) AND deleted_at IS NULL FOR SHARE",
				m.VLMModelID, r.TenantID).Scan(&vlmStamp).Error
			if e != nil {
				return e
			}
			if vlmStamp == "" || vlmStamp != m.VLMModelStamp {
				return legacyFence("image_model_configuration_changed")
			}
		}
		if m.StorageBackendID != "" {
			var currentStorage string
			if e = tx.Raw("SELECT md5(row_to_json(b)::text) FROM storage_backends b WHERE id=? AND "+
				"tenant_id=? AND provider='local' AND status='active' AND deleted_at IS "+
				"NULL FOR SHARE", m.StorageBackendID, r.TenantID).Scan(&currentStorage).Error; e != nil {
				return e
			}
			if currentStorage == "" || currentStorage != m.StorageStamp {
				return legacyFence("storage_configuration_changed")
			}
		}
		var old types.Knowledge
		if e = validateLegacyAssets(tx, r, a, &m, true); e != nil {
			return e
		}
		e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&old, "id=? AND tenant_id=?", r.KnowledgeID, r.TenantID).
			Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if old.FileHash != a.ExpectedHash {
			return legacyFence("public_file_hash_changed")
		}
		indexed, e := legacyIndexedChunks(m.Chunks)
		if e != nil {
			return e
		}
		var newStorage int64
		for _, chunk := range indexed {
			chunk.ContextHeader = m.ChunkHeaders[chunk.ID]
			newStorage += pgretrieve.EstimateIndexStorageSize(
				buildLegacyIndexContent(m.Knowledge.Title, chunk.EmbeddingContent()),
				m.Dimension,
			)
		}
		var tenant types.Tenant
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&tenant, "id=?", r.TenantID).Error; e != nil {
			return e
		}
		if tenant.Status != "active" {
			return legacyFence("tenant_inactive")
		}
		delta := newStorage - old.StorageSize
		updatedStorage := tenant.StorageUsed + delta
		if updatedStorage < 0 {
			updatedStorage = 0
		}
		if delta > 0 && tenant.StorageQuota > 0 && updatedStorage > tenant.StorageQuota {
			return errors.New("Feishu publication exceeds workspace storage quota")
		}
		if e = tx.Model(&types.Tenant{}).Where("id=?",
			r.TenantID).UpdateColumn("storage_used",
			updatedStorage).Error; e != nil {
			return e
		}
		var n int64
		var usableIndexes int64
		if e = tx.Raw(`SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid
 JOIN pg_am am ON am.oid=(SELECT relam FROM pg_class WHERE oid=i.indexrelid)
 WHERE c.relname='embeddings' AND i.indisvalid AND am.amname='hnsw'
 AND pg_get_indexdef(i.indexrelid) LIKE ?`,
			"%halfvec("+fmt.Sprint(m.Dimension)+")%",
		).Scan(&usableIndexes).Error; e != nil {
			return e
		}
		if usableIndexes < 1 {
			return fmt.Errorf(
				"feishu candidate vector index dimension %d unavailable: %w",
				m.Dimension,
				legacyFence("vector_index_unavailable"),
			)
		}
		if e = tx.Raw("SELECT count(*) FROM feishu_legacy_index_candidates WHERE attempt"+
			"_id=? AND dimension=?", a.ID, m.Dimension).Scan(&n).Error; e != nil {
			return e
		}
		if n != int64(len(indexed)) {
			return legacyFence("candidate_count_mismatch")
		}
		// Both trigger checks and writes are under the same resource lock.
		if e = tx.Exec("SELECT set_config('knowhub.feishu_attempt',?,true),set_config('kn"+
			"owhub.feishu_nonce',?,true)", a.ID, a.OwnerNonce).Error; e != nil {
			return e
		}
		if e = tx.Exec("DELETE FROM embeddings WHERE knowledge_id=? AND knowledge_base_id=?",
			r.KnowledgeID,
			r.KnowledgeBaseID).Error; e != nil {
			return e
		}
		if e = tx.Where("knowledge_id=? AND tenant_id=?",
			r.KnowledgeID,
			r.TenantID).Delete(&types.Chunk{}).Error; e != nil {
			return e
		}
		k := m.Knowledge
		k.StorageSize = newStorage
		k.CreatedAt = old.CreatedAt
		if k.CreatedAt.IsZero() {
			k.CreatedAt = time.Now().UTC()
		}
		k.ParseStatus = types.ParseStatusCompleted
		k.EnableStatus = "enabled"
		k.PendingSubtasksCount = 0
		k.UpdatedAt = time.Now().UTC()
		k.ProcessedAt = &k.UpdatedAt
		if old.ID == "" {
			e = tx.Create(&k).Error
		} else {
			e = tx.Model(&types.Knowledge{}).Where("id=? AND tenant_id=?",
				k.ID,
				k.TenantID).Select("title",
				"source",
				"channel",
				"file_name",
				"file_path",
				"file_hash",
				"file_size",
				"file_type",
				"storage_size",
				"metadata",
				"parse_status",
				"enable_status",
				"embedding_model_id",
				"updated_at",
				"processed_at",
				"description",
				"error_message",
				"pending_subtasks_count",
				"summary_status",
				"profile").Updates(&k).Error
		}
		if e != nil {
			return e
		}
		if handle, registered := types.ParseResourcePath(k.FilePath); registered {
			var file types.StoredResource
			if e = tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&file,
				"handle=? AND tenant_id=? AND state='active'",
				handle,
				r.TenantID).Error; e != nil {
				return e
			}
			if file.Provider != "local" || file.StorageBackendID != m.StorageBackendID ||
				!strings.Contains(file.PhysicalPath, "/"+a.ID+"/") {
				return legacyFence("stored_file_binding_changed")
			}
			binding := &types.ResourceBinding{
				ResourceID: file.ID,
				TenantID:   r.TenantID,
				OwnerType:  types.ResourceOwnerKnowledge,
				OwnerID:    r.KnowledgeID,
				Relation:   types.ResourceRelationSourceFile,
			}
			if e = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(binding).Error; e != nil {
				return e
			}
		}
		for _, c := range m.Chunks {
			c.ContextHeader = m.ChunkHeaders[c.ID]
			c.SourceContent = c.Content
			c.SeqID = 0
			if e = tx.Select("*").Create(c).Error; e != nil {
				return e
			}
		}
		if e = tx.Exec(`INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,
content,dimension,embedding,is_enabled)
 SELECT chunk_id,?,chunk_id,?,?,content,dimension,embedding,true
 FROM feishu_legacy_index_candidates WHERE attempt_id=?`,
			int(types.ChunkSourceType), r.KnowledgeID, r.KnowledgeBaseID, a.ID,
		).Error; e != nil {
			return e
		}
		if e = tx.Model(&types.Chunk{}).Where("knowledge_id=? AND tenant_id=?",
			r.KnowledgeID,
			r.TenantID).Count(&n).Error; e != nil {
			return e
		}
		if n != int64(len(m.Chunks)) {
			return legacyFence("published_chunk_count_mismatch")
		}
		var vectors int64
		if e = tx.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND "+
			"knowledge_base_id=? AND dimension=?",
			r.KnowledgeID,
			r.KnowledgeBaseID,
			m.Dimension).Scan(&vectors).Error; e != nil {
			return e
		}
		if vectors != int64(len(indexed)) {
			return legacyFence("published_vector_count_mismatch")
		}
		receipt = types.FeishuLegacyReceipt{
			Created:        old.ID == "",
			ResourceID:     r.ID,
			AttemptID:      a.ID,
			Generation:     a.Generation,
			KnowledgeID:    r.KnowledgeID,
			ManifestSHA256: m.SHA256,
			Chunks:         n,
			ParentChunks:   n - int64(len(indexed)),
			IndexedChunks:  int64(len(indexed)),
			Vectors:        vectors,
			BM25Rows:       vectors,
			Dimension:      m.Dimension,
			Assets:         len(m.Assets),
			PublishedAt:    time.Now().UTC(),
		}
		b, _ := json.Marshal(receipt)
		if e = tx.Model(a).Updates(map[string]any{
			"status":  "published",
			"receipt": json.RawMessage(b),
		}).Error; e != nil {
			return e
		}
		return tx.Model(r).
			Updates(map[string]any{"active_attempt": a.ID, "state": "active", "updated_at": receipt.PublishedAt}).
			Error
	})
	if err != nil {
		return nil, fmt.Errorf("Feishu atomic publication unconfirmed: %w", err)
	}
	return &receipt, nil
}

// Reconcile a lost acknowledgement by the durable receipt and live public set;
// never repeat destructive publication merely because the client timed out.
func (s *FeishuLegacyStore) Receipt(
	ctx context.Context,
	r *types.FeishuLegacyResource,
	a *types.FeishuLegacyAttempt,
) (*types.FeishuLegacyReceipt, error) {
	var out types.FeishuLegacyReceipt
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var live types.FeishuLegacyResource
		if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&live, "id=?", r.ID).Error; e != nil {
			return e
		}
		if live.State != "active" || live.ActiveAttempt != a.ID {
			return legacyFence("receipt_not_active")
		}
		var current types.FeishuLegacyAttempt
		if e := tx.First(&current, "id=? AND status='published'", a.ID).Error; e != nil {
			return e
		}
		if e := json.Unmarshal(current.Receipt, &out); e != nil {
			return e
		}
		if out.KnowledgeID != r.KnowledgeID || out.AttemptID != a.ID {
			return legacyFence("receipt_identity_mismatch")
		}
		var manifest types.FeishuLegacyManifest
		if e := json.Unmarshal(current.Manifest, &manifest); e != nil {
			return e
		}
		manifestHash, e := LegacyManifestHash(manifest)
		if e != nil || manifestHash != manifest.SHA256 || manifestHash != out.ManifestSHA256 {
			return legacyFence("receipt_manifest_mismatch")
		}
		indexed, e := legacyIndexedChunks(manifest.Chunks)
		if e != nil {
			return e
		}
		if out.Assets != len(manifest.Assets) {
			return legacyFence("receipt_asset_count_mismatch")
		}
		if e := validateLegacyAssets(tx, &live, &current, &manifest, false); e != nil {
			return e
		}
		var public []*types.Chunk
		if e := tx.Where("tenant_id=? AND knowledge_id=?", r.TenantID, r.KnowledgeID).Find(&public).Error; e != nil {
			return e
		}
		publicIndexed, e := legacyIndexedChunks(public)
		if e != nil {
			return e
		}
		if len(manifest.Assets) > 0 {
			publicManifest := manifest
			publicManifest.Chunks = public
			if e := validateLegacyAssets(tx, &live, &current, &publicManifest, false); e != nil {
				return e
			}
		}
		chunks := int64(len(public))
		indexedCount := int64(len(indexed))
		parents := int64(len(manifest.Chunks)) - indexedCount
		if out.IndexedChunks == 0 && out.ParentChunks == 0 {
			// Old receipts are revalidated against their immutable manifest and
			// the live graph, never repaired by trusting loose count inequalities.
			out.IndexedChunks, out.ParentChunks = indexedCount, parents
		}
		if out.Chunks != int64(len(manifest.Chunks)) || out.Chunks != out.ParentChunks+out.IndexedChunks ||
			out.ParentChunks != parents || out.IndexedChunks != indexedCount ||
			int64(len(publicIndexed)) != indexedCount || out.Vectors != indexedCount || out.BM25Rows != indexedCount {
			return legacyFence("receipt_graph_count_mismatch")
		}
		manifestByID := make(map[string]*types.Chunk, len(manifest.Chunks))
		for _, chunk := range manifest.Chunks {
			manifestByID[chunk.ID] = chunk
		}
		for _, chunk := range public {
			owned := manifestByID[chunk.ID]
			if owned == nil || owned.ChunkType != chunk.ChunkType || owned.ParentChunkID != chunk.ParentChunkID {
				return legacyFence("receipt_public_graph_mismatch")
			}
		}
		var vectors int64
		if e := tx.Raw("SELECT count(*) FROM embeddings WHERE knowledge_base_id=? AND "+
			"knowledge_id=? AND dimension=?",
			r.KnowledgeBaseID,
			r.KnowledgeID,
			out.Dimension).Scan(&vectors).Error; e != nil {
			return e
		}
		if chunks != out.Chunks || vectors != out.Vectors {
			return legacyFence("receipt_public_count_mismatch")
		}
		return nil
	})
	return &out, err
}

func (s *FeishuLegacyStore) Abandon(ctx context.Context, owner *types.FeishuLegacyAttempt) error {
	// Never changes resource ownership or public data. Expiring only our exact
	// nonce/generation permits safe retry without waiting for an entire lease.
	return s.db.WithContext(ctx).Model(&types.FeishuLegacyAttempt{}).
		Where("id=? AND generation=? AND owner_nonce=? AND status IN ('staging','ready')",
			owner.ID,
			owner.Generation,
			owner.OwnerNonce).
		Update("lease_until", gorm.Expr("clock_timestamp()")).Error
}

func validateLegacyCursorReceipts(tx *gorm.DB, ctx context.Context) error {
	receipts := types.LegacyReceipts(ctx)
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ResourceID < receipts[j].ResourceID })
	for _, receipt := range receipts {
		if receipt.ResourceID == "" || receipt.Chunks < 1 {
			return legacyFence("cursor_receipt_invalid")
		}
		var r types.FeishuLegacyResource
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&r,
			"id=?",
			receipt.ResourceID).Error; err != nil {
			return err
		}
		if r.State != "active" || r.ActiveAttempt != receipt.AttemptID || r.KnowledgeID != receipt.KnowledgeID {
			return legacyFence("cursor_receipt_not_active")
		}
		var a types.FeishuLegacyAttempt
		if err := tx.First(&a,
			"id=? AND generation=? AND status='published'",
			receipt.AttemptID,
			receipt.Generation).Error; err != nil {
			return err
		}
		var actual types.FeishuLegacyReceipt
		if err := json.Unmarshal(a.Receipt, &actual); err != nil {
			return err
		}
		if actual.ManifestSHA256 != receipt.ManifestSHA256 {
			return legacyFence("cursor_manifest_mismatch")
		}
	}
	return nil
}
