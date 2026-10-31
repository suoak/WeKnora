package repository

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrPersonalFence reports personal fence.
	ErrPersonalFence = errors.New("personal_attempt_fence_lost")
	// ErrPersonalBusy reports an import attempt with a live lease.
	ErrPersonalBusy = errors.New("personal_attempt_leased")
	// ErrPersonalIdentity reports changed resource or authorization identity.
	ErrPersonalIdentity = errors.New("personal_attempt_identity_changed")
)

// PersonalRecoveryRepository shares task_pending_ops with knowledge dispatch.
// None of its transactions include file, Feishu, Redis or model requests.
type PersonalRecoveryRepository struct {
	db                        *gorm.DB
	FirstCreateOnly           bool
	ApplicationGrantValidator func(*gorm.DB, *types.FeishuPersonalImport, *types.FeishuPersonalAttempt) error
}

// InTransaction reuses the same protocol checks in a caller-owned write
// transaction. Mutate uses a nested savepoint; its locks survive until the
// outer transaction commits. It does not mint ownership or extend a lease.
func (r *PersonalRecoveryRepository) InTransaction(tx *gorm.DB) *PersonalRecoveryRepository {
	transactionRepo := *r
	transactionRepo.db = tx
	return &transactionRepo
}

var (
	// ErrPersonalUpdateDisabled reports personal update disabled.
	ErrPersonalUpdateDisabled = errors.New("personal_document_update_not_open")
	// ErrPersonalAlreadyImported rejects another importer for the same canonical KB resource.
	ErrPersonalAlreadyImported = errors.New("personal_document_already_imported_by_another_user")
	// ErrPersonalReviewRequired requires review before resuming partial public ingestion.
	ErrPersonalReviewRequired = errors.New("personal_first_create_requires_review")
	// ErrPersonalSourceManaged fences first-create after datasource handoff.
	ErrPersonalSourceManaged = errors.New("personal_document_managed_by_source")
)

// NewPersonalRecoveryRepository constructs PersonalRecoveryRepository.
func NewPersonalRecoveryRepository(db *gorm.DB) *PersonalRecoveryRepository {
	return &PersonalRecoveryRepository{db: db}
}

// Prepare provides the prepare operation.
// Prepare's caller must supply an already canonical resource and a fresh
// authorization snapshot. The five-field DB constraint is authoritative.
// On an ambiguous commit, replaying this operation uses the same DB identity.
func (r *PersonalRecoveryRepository) Prepare(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) (*types.FeishuPersonalResource, *types.FeishuPersonalAttempt, error) {
	var resource types.FeishuPersonalResource
	var accepted types.FeishuPersonalAttempt
	committing := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if r.FirstCreateOnly {
			// Canonical resource lock spans users but carries no user's token.
			// All MVP admissions take this lock before inserting the user mapping.
			h := fnv.New64a()
			key, _ := json.Marshal(
				[]string{"feishu-mvp-resource", m.AppID, m.KBID, m.ResourceType, m.DocumentToken},
			)
			_, _ = h.Write(key)
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(h.Sum64())).Error; err != nil {
				return err
			}
			var previous []types.FeishuPersonalImport
			if err := tx.Where(
				"app_id = ? AND kb_id = ? AND resource_type = ? AND document_token = ?",
				m.AppID,
				m.KBID,
				m.ResourceType,
				m.DocumentToken,
			).
				Limit(
					2,
				).
				Find(
					&previous,
				).
				Error; err != nil {
				return err
			}
			for _, prior := range previous {
				if types.FeishuAuthMode(prior.AuthMode) != types.FeishuAuthMode(m.AuthMode) {
					return ErrPersonalIdentity
				}
				if prior.UserID != m.UserID {
					return ErrPersonalAlreadyImported
				}
				if prior.Status == "source_managed" {
					return ErrPersonalSourceManaged
				}
				if prior.KnowledgeID != "" && prior.SourceModified != a.Revision {
					return ErrPersonalUpdateDisabled
				}
				var current types.FeishuPersonalAttempt
				err := tx.Where("import_id = ? AND status <> ?", prior.ID, "obsolete").
					Order("created_at DESC").
					First(&current).
					Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err == nil && current.Revision != a.Revision {
					return ErrPersonalUpdateDisabled
				}
				if err == nil && prior.KnowledgeID != "" &&
					(current.GrantEpoch != a.GrantEpoch || current.AppConfigVersion != a.AppConfigVersion) {
					return ErrPersonalIdentity
				}
			}
		}
		if a.Revision == "" || a.AppConfigVersion == "" {
			return ErrPersonalIdentity
		}
		if types.FeishuAuthMode(m.AuthMode) != types.FeishuAuthMode(a.AuthMode) {
			return ErrPersonalIdentity
		}
		if err := r.lockPrincipal(tx, m, a); err != nil {
			return err
		}

		openID := m.OpenID
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(m).Error; err != nil {
			return err
		}
		user,
			app,
			kb,
			kind,
			token,
			mode,
			application := m.UserID,
			m.AppID,
			m.KBID,
			m.ResourceType,
			m.DocumentToken,
			types.FeishuAuthMode(
				m.AuthMode,
			), m.ApplicationID
		*m = types.FeishuPersonalImport{
			UserID:        user,
			AppID:         app,
			KBID:          kb,
			ResourceType:  kind,
			DocumentToken: token,
			AuthMode:      mode,
			ApplicationID: application,
		}
		if err := tx.Where(
			"user_id = ? AND app_id = ? AND kb_id = ? AND resource_type = ? AND document_token = ?",
			m.UserID, m.AppID, m.KBID, m.ResourceType, m.DocumentToken,
		).First(m).Error; err != nil {
			return err
		}
		if types.FeishuAuthMode(m.AuthMode) != mode || m.ApplicationID != application ||
			m.OpenID != openID ||
			(mode == types.FeishuUserOAuth && m.OpenID == "") {
			return ErrPersonalIdentity
		}
		// Recover legacy K1 by its explicit user/resource/open-id ownership.
		// A content hash is never an ownership key. Ambiguity stops admission.
		if m.KnowledgeID == "" && types.FeishuAuthMode(m.AuthMode) == types.FeishuUserOAuth {
			var legacy []types.Knowledge
			err := tx.Where("knowledge_base_id = ? AND channel = ? AND "+
				"metadata->>'feishu_imported_by' = ? AND "+
				"metadata->>'feishu_resource_id' = ? AND metadata->>'feishu_open_id' = "+
				"?", m.KBID, "feishu_personal", m.UserID, m.ResourceType+":"+m.DocumentToken, m.OpenID).
				Limit(2).
				Find(&legacy).
				Error
			if err != nil {
				return err
			}
			if len(legacy) > 1 {
				return ErrPersonalIdentity
			}
			if len(legacy) == 1 {
				m.KnowledgeID = legacy[0].ID
				if err = tx.Model(m).Update("knowledge_id", m.KnowledgeID).Error; err != nil {
					return err
				}
			}
		}
		logical := m.KnowledgeID
		if logical == "" {
			logical = uuid.NewString()
		}
		resource = types.FeishuPersonalResource{
			ImportID:           m.ID,
			LogicalKnowledgeID: logical,
			ActiveVersionID:    m.KnowledgeID,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&resource).Error; err != nil {
			return err
		}
		if err := tx.Clauses(
			clause.Locking{
				Strength: "UPDATE",
			},
		).
			Where(
				"import_id = ?",
				m.ID,
			).
			First(
				&resource,
			).
			Error; err != nil {
			return err
		}
		err := tx.Where("import_id = ? AND revision = ? AND app_config_version = ? AND "+
			"grant_epoch = ?", m.ID, a.Revision, a.AppConfigVersion, a.GrantEpoch).
			First(&accepted).
			Error
		if err == nil {
			if resource.CurrentAttemptID != accepted.ID {
				return ErrPersonalIdentity
			}
			// Only a fresh explicit import (after live resource authorization)
			// resumes attention. Periodic reconciliation never grants permission.
			if accepted.Status == "needs_attention" {
				if r.FirstCreateOnly && m.KnowledgeID != "" {
					var k types.Knowledge
					if err := tx.Clauses(
						clause.Locking{
							Strength: "UPDATE",
						},
					).
						Where(
							"id = ?",
							m.KnowledgeID,
						).
						First(
							&k,
						).
						Error; err != nil {
						return err
					}
					var chunks, indexes int64
					if err := tx.Model(
						&types.Chunk{},
					).
						Where(
							"knowledge_id = ?",
							k.ID,
						).
						Count(
							&chunks,
						).
						Error; err != nil {
						return err
					}
					if chunks > 0 || k.EnableStatus == "enabled" {
						return ErrPersonalReviewRequired
					}
					if err := tx.Table("embeddings").Where("knowledge_id = ?", k.ID).Count(&indexes).Error; err != nil {
						return err
					}
					if indexes > 0 {
						return ErrPersonalReviewRequired
					}
					if err := tx.Model(
						&types.TaskPendingOp{},
					).
						Where(
							"scope = ? AND scope_id = ?",
							types.KnowledgeDispatchScope,
							k.ID,
						).
						Update(
							"op",
							"pending",
						).
						Error; err != nil {
						return err
					}
					if err := tx.Model(
						&types.Knowledge{},
					).
						Where(
							"id = ?",
							k.ID,
						).
						Updates(
							map[string]any{
								"parse_status":  types.ParseStatusPending,
								"error_message": "",
							},
						).
						Error; err != nil {
						return err
					}
				}
				accepted.Status = "pending"
				accepted.LastErrorCategory = ""
				accepted.OwnerNonce = ""
				accepted.LeaseUntil = nil
				accepted.RetryAfter = nil
				resource.Generation++
				if err := tx.Save(&accepted).Error; err != nil {
					return err
				}
				if err := tx.Save(&resource).Error; err != nil {
					return err
				}
				if err := tx.Model(
					&types.TaskPendingOp{},
				).
					Where(
						"scope = ? AND scope_id = ?",
						types.KnowledgeDispatchScope,
						accepted.CandidateVersionID,
					).
					Update(
						"op",
						"pending",
					).
					Error; err != nil {
					return err
				}
				if err := tx.Model(m).Update("status", "accepted").Error; err != nil {
					return err
				}
				if r.FirstCreateOnly && m.KnowledgeID != "" {
					accepted.Status = "index_pending"
					if err := tx.Save(&accepted).Error; err != nil {
						return err
					}
					if err := tx.Model(m).Update("status", "index_pending").Error; err != nil {
						return err
					}
					if err := tx.Model(
						&types.TaskPendingOp{},
					).
						Where(
							"scope = ? AND scope_id = ?",
							types.KnowledgeDispatchScope,
							accepted.CandidateVersionID,
						).
						Update(
							"op",
							"completed",
						).
						Error; err != nil {
						return err
					}
				}
			}
			committing = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		accepted = *a
		accepted.ID = uuid.NewString()
		accepted.ImportID = m.ID
		accepted.LogicalKnowledgeID = resource.LogicalKnowledgeID
		accepted.CandidateVersionID = uuid.NewString()
		accepted.ExpectedActiveVersion = resource.ActiveVersionID
		accepted.Status = "pending"
		accepted.OwnerNonce = ""
		accepted.Fence = 0
		accepted.LeaseUntil = nil
		if resource.CurrentAttemptID != "" {
			if err := tx.Model(
				&types.FeishuPersonalAttempt{},
			).
				Where(
					"id = ?",
					resource.CurrentAttemptID,
				).
				Updates(
					map[string]any{
						"status":      "obsolete",
						"owner_nonce": "",
						"lease_until": nil,
					},
				).
				Error; err != nil {
				return err
			}
			if err := tx.Model(
				&types.TaskPendingOp{},
			).
				Where(
					"scope = ? AND scope_id = ?",
					types.KnowledgeDispatchScope,
					resource.CandidateVersionID,
				).
				Update(
					"op",
					"obsolete",
				).
				Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&accepted).Error; err != nil {
			return err
		}
		resource.CurrentAttemptID = accepted.ID
		resource.CandidateVersionID = accepted.CandidateVersionID
		resource.Generation++
		if err := tx.Save(&resource).Error; err != nil {
			return err
		}
		op, err := personalDispatch(m, &accepted)
		if err != nil {
			return err
		}
		if err = tx.Create(op).Error; err != nil {
			return err
		}
		if err = tx.Model(m).Updates(map[string]any{"status": "accepted", "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		committing = true
		return nil
	})
	if err != nil {
		if committing {
			read, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			var committed types.FeishuPersonalAttempt
			query := r.db.WithContext(read)
			if reconcileErr := query.Where("import_id = ? AND revision = ? AND app_config_version = ? AND "+
				"grant_epoch = ?",
				m.ID,
				a.Revision,
				a.AppConfigVersion,
				a.GrantEpoch).First(&committed).Error; reconcileErr == nil {

				var fixed types.FeishuPersonalResource
				if reconcileErr = query.Where(
					"import_id = ? AND current_attempt_id = ?",
					m.ID,
					committed.ID,
				).
					First(
						&fixed,
					).
					Error; reconcileErr == nil {
					return &fixed, &committed, nil
				}

			}
		}
		return nil, nil, &types.KnowledgeDispatchAdmissionError{Cause: err, CommitUnknown: committing}
	}
	return &resource, &accepted, nil
}

func personalDispatch(
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) (*types.TaskPendingOp, error) {
	task := types.DocumentProcessPayload{
		PersonalAttemptID:  a.ID,
		CandidateVersionID: a.CandidateVersionID,
		DispatchID:         "personal-" + a.ID,
		TenantID:           a.CallerTenantID,
		KnowledgeID:        a.CandidateVersionID,
		KnowledgeBaseID:    m.KBID,
		Attempt:            1,
	}
	version := 2
	taskType := types.TypeDocumentProcess
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		version = 4
		taskType = types.TypeFeishuApplicationProcess
		task.FeishuAuthMode = m.AuthMode
		task.FeishuApplicationID = m.ApplicationID
	}
	payload, err := json.Marshal(
		types.KnowledgeDispatch{
			PayloadVersion: version,
			SourceID:       m.ID,
			ExternalID:     m.ResourceType + ":" + m.DocumentToken,
			AttemptID:      a.ID,
			Revision:       a.Revision,
			Task:           task,
		},
	)
	if err != nil {
		return nil, err
	}
	return &types.TaskPendingOp{
		TenantID:   a.CallerTenantID,
		TaskType:   taskType,
		Scope:      types.KnowledgeDispatchScope,
		ScopeID:    a.CandidateVersionID,
		DedupKey:   task.DispatchID,
		Op:         "pending",
		Payload:    payload,
		EnqueuedAt: time.Now(),
	}, nil
}

// Load provides the load operation.
func (r *PersonalRecoveryRepository) Load(
	ctx context.Context,
	id string,
) (*types.FeishuPersonalImport, *types.FeishuPersonalResource, *types.FeishuPersonalAttempt, error) {
	var a types.FeishuPersonalAttempt
	var m types.FeishuPersonalImport
	var resource types.FeishuPersonalResource
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&a).Error
	if err == nil {
		err = r.db.WithContext(ctx).Where("id = ?", a.ImportID).First(&m).Error
	}
	if err == nil {
		err = r.db.WithContext(ctx).Where("import_id = ?", a.ImportID).First(&resource).Error
	}

	if err == nil && resource.CurrentAttemptID == a.ID &&
		(resource.LogicalKnowledgeID != a.LogicalKnowledgeID ||
			resource.CandidateVersionID != a.CandidateVersionID ||
			resource.ActiveVersionID != a.ExpectedActiveVersion) {
		published, receiptErr := r.exactPublishedIdentity(ctx, &m, &resource, &a)
		if receiptErr != nil {
			err = receiptErr
		} else if !published {
			err = ErrPersonalIdentity
		}
	}
	return &m, &resource, &a, err
}

// Recognize a consumed first-create claim without changing its immutable
// expected-active version. This is identity reconciliation only, never a read
// grant or permission to claim again. Claim/Mutate retain their original CAS.
func (r *PersonalRecoveryRepository) exactPublishedIdentity(
	ctx context.Context, m *types.FeishuPersonalImport, resource *types.FeishuPersonalResource,
	a *types.FeishuPersonalAttempt,
) (bool, error) {
	if a.Status != "indexed" || a.OwnerNonce != "" || a.LeaseUntil != nil || a.ExpectedActiveVersion != "" ||
		a.CandidateVersionID == "" || a.CandidateVersionID == a.LogicalKnowledgeID ||
		resource.ActiveVersionID != a.CandidateVersionID || resource.CandidateVersionID != a.CandidateVersionID ||
		resource.LogicalKnowledgeID != a.LogicalKnowledgeID || resource.Generation != a.Fence ||
		m.KnowledgeID != a.LogicalKnowledgeID {
		return false, nil
	}
	var rows []types.TaskPendingOp
	if err := r.db.WithContext(ctx).Where("scope=? AND scope_id=? AND tenant_id=?",
		types.PersonalExactReceiptScope, a.LogicalKnowledgeID, a.CallerTenantID).
		Limit(2).Find(&rows).Error; err != nil {
		return false, err
	}
	var receipt types.PersonalExactReceipt
	if len(rows) != 1 || rows[0].Op != "published" || json.Unmarshal(rows[0].Payload, &receipt) != nil {
		return false, nil
	}
	return receipt.Version == 1 && receipt.ImportID == m.ID && receipt.AttemptID == a.ID &&
		receipt.KnowledgeID == a.LogicalKnowledgeID && receipt.KnowledgeBaseID == m.KBID &&
		receipt.TenantID == a.CallerTenantID && receipt.CandidateVersionID == a.CandidateVersionID &&
		receipt.ExpectedActive == a.ExpectedActiveVersion && receipt.Generation == a.Fence &&
		receipt.ManifestSHA256 == a.ManifestHash && receipt.GrantEpoch == a.GrantEpoch &&
		receipt.AppConfigVersion == a.AppConfigVersion && receipt.Revision == a.Revision &&
		len(receipt.FileSHA256) == 64 && len(receipt.ParserOutputSHA256) == 64 &&
		len(receipt.ChunksSHA256) == 64 && len(receipt.EmbeddingsSHA256) == 64 &&
		len(receipt.DerivedSHA256) == 64 && receipt.TextChunks > 0 && receipt.VectorRows > 0, nil
}

// Lock ordering is always resource then attempt, including renew and commit.
func (r *PersonalRecoveryRepository) locked(
	ctx context.Context,
	id string,
	fn func(*gorm.DB, *types.FeishuPersonalResource, *types.FeishuPersonalAttempt, time.Time) error,
) error {
	var ref types.FeishuPersonalAttempt
	if err := r.db.WithContext(ctx).Select("import_id").Where("id = ?", id).First(&ref).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var resource types.FeishuPersonalResource
		var a types.FeishuPersonalAttempt
		if err := tx.Clauses(
			clause.Locking{
				Strength: "UPDATE",
			},
		).
			Where(
				"import_id = ?",
				ref.ImportID,
			).
			First(
				&resource,
			).
			Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&a).Error; err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		return fn(tx, &resource, &a, now)
	})
}

// Claim provides the claim operation.
func (r *PersonalRecoveryRepository) Claim(
	ctx context.Context,
	id string,
	lease time.Duration,
) (*types.FeishuPersonalAttempt, error) {
	return r.claim(ctx, id, lease, nil, nil)
}

// ClaimObserved binds a first-create claim to the complete preflight observation.
// A changed protocol or generation is rejected before ownership is minted.
func (r *PersonalRecoveryRepository) ClaimObserved(
	ctx context.Context, observed *types.FeishuPersonalAttempt, lease time.Duration,
	validate func(*gorm.DB, *types.FeishuPersonalAttempt) error,
) (*types.FeishuPersonalAttempt, error) {
	if observed == nil || validate == nil {
		return nil, ErrPersonalFence
	}
	return r.claim(ctx, observed.ID, lease, observed, validate)
}

func (r *PersonalRecoveryRepository) claim(
	ctx context.Context, id string, lease time.Duration, observed *types.FeishuPersonalAttempt,
	validate func(*gorm.DB, *types.FeishuPersonalAttempt) error,
) (*types.FeishuPersonalAttempt, error) {
	var claimed types.FeishuPersonalAttempt
	err := r.locked(
		ctx,
		id,
		func(tx *gorm.DB, resource *types.FeishuPersonalResource, a *types.FeishuPersonalAttempt, now time.Time) error {
			if observed != nil {
				if !reflect.DeepEqual(a, observed) || resource.Generation != observed.Fence ||
					resource.CandidateVersionID != observed.CandidateVersionID ||
					observed.Status != "index_pending" || observed.OwnerNonce != "" || observed.LeaseUntil != nil {
					return ErrPersonalFence
				}
				var m types.FeishuPersonalImport
				if err := tx.Where("id=?", a.ImportID).First(&m).Error; err != nil {
					return err
				}
				if err := r.lockPrincipal(tx, &m, a); err != nil {
					return err
				}
			}
			if resource.CurrentAttemptID != a.ID || resource.ActiveVersionID != a.ExpectedActiveVersion {
				return ErrPersonalFence
			}
			if a.Status == "awaiting_index_isolation" || a.Status == "needs_attention" ||
				a.Status == "obsolete" {
				return ErrPersonalFence
			}
			if a.LeaseUntil != nil && a.LeaseUntil.After(now) {
				return ErrPersonalBusy
			}
			if a.RetryAfter != nil && a.RetryAfter.After(now) {
				return ErrPersonalBusy
			}
			resource.Generation++
			a.Fence = resource.Generation
			a.OwnerNonce = uuid.NewString()
			until := now.Add(lease)
			a.LeaseUntil = &until
			a.Status = "processing"
			if err := tx.Save(resource).Error; err != nil {
				return err
			}
			if err := tx.Save(a).Error; err != nil {
				return err
			}
			if validate != nil {
				if err := validate(tx, a); err != nil {
					return err
				}
			}
			claimed = *a
			return nil
		},
	)
	return &claimed, err
}

// Mutate is the sole commit boundary for a worker. Expired owners cannot
// publish even if their network request ignored cancellation and returned late.
func (r *PersonalRecoveryRepository) Mutate(
	ctx context.Context,
	owner *types.FeishuPersonalAttempt,
	lease time.Duration,
	fn func(*gorm.DB, *types.FeishuPersonalAttempt) error,
) error {
	return r.locked(
		ctx,
		owner.ID,
		func(tx *gorm.DB, resource *types.FeishuPersonalResource, a *types.FeishuPersonalAttempt, now time.Time) error {
			if resource.CurrentAttemptID != owner.ID || resource.Generation != owner.Fence ||
				a.Fence != owner.Fence ||
				a.OwnerNonce != owner.OwnerNonce ||
				a.LeaseUntil == nil ||
				!a.LeaseUntil.After(now) ||
				resource.ActiveVersionID != owner.ExpectedActiveVersion ||
				a.Status != "processing" {
				return ErrPersonalFence
			}
			var m types.FeishuPersonalImport
			if err := tx.Where("id = ?", a.ImportID).First(&m).Error; err != nil {
				return err
			}
			if err := r.lockPrincipal(tx, &m, a); err != nil {
				return err
			}

			if lease > 0 {
				until := now.Add(lease)
				a.LeaseUntil = &until
			}
			if fn != nil {
				if err := fn(tx, a); err != nil {
					return err
				}
			}
			a.UpdatedAt = now
			return tx.Save(a).Error
		},
	)
}

// Invalidate also fences a worker already inside a remote call. It never
// deletes active Knowledge, candidate files or an ambiguous manifest.
func (r *PersonalRecoveryRepository) Invalidate(ctx context.Context, id, category string) error {
	return r.locked(
		ctx,
		id,
		func(tx *gorm.DB, resource *types.FeishuPersonalResource, a *types.FeishuPersonalAttempt, _ time.Time) error {
			if resource.CurrentAttemptID != id {
				return nil
			}
			resource.Generation++
			a.Status = "needs_attention"
			a.LastErrorCategory = category
			a.OwnerNonce = ""
			a.LeaseUntil = nil
			if err := tx.Save(resource).Error; err != nil {
				return err
			}
			if err := tx.Save(a).Error; err != nil {
				return err
			}
			if err := tx.Model(
				&types.FeishuPersonalImport{},
			).
				Where(
					"id = ?",

					a.ImportID,
				).
				Update(
					"status",

					"needs_attention",
				).
				Error; err != nil {
				return err
			}
			return tx.Model(&types.TaskPendingOp{}).
				Where("scope = ? AND scope_id = ?", types.KnowledgeDispatchScope, a.CandidateVersionID).
				Update("op", "attention").
				Error
		},
	)
}

// ReconcileIntent repairs a missing queue intent using only persisted ownership.
// Lease expiry permits redelivery, never a new candidate or a new logical ID.
func (r *PersonalRecoveryRepository) ReconcileIntent(ctx context.Context, id string) error {
	return r.locked(
		ctx,
		id,
		func(tx *gorm.DB, resource *types.FeishuPersonalResource, a *types.FeishuPersonalAttempt, now time.Time) error {
			if resource.CurrentAttemptID != id {
				return nil
			}
			if a.Status == "needs_attention" || a.Status == "obsolete" {
				return nil
			}
			var m types.FeishuPersonalImport
			if err := tx.Where("id = ?", a.ImportID).First(&m).Error; err != nil {
				return err
			}
			op, err := personalDispatch(&m, a)
			if err != nil {
				return err
			}
			if a.Status == "awaiting_index_isolation" || (r.FirstCreateOnly && m.KnowledgeID != "") {
				op.Op = "completed"
			}
			var existing types.TaskPendingOp
			err = tx.Where("scope = ? AND scope_id = ?", types.KnowledgeDispatchScope, a.CandidateVersionID).
				First(&existing).
				Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return tx.Create(op).Error
			}
			if err != nil {
				return err
			}
			if existing.DedupKey != op.DedupKey || existing.TenantID != op.TenantID {
				return ErrPersonalIdentity
			}
			if op.Op == "completed" {
				return tx.Model(&existing).Update("op", "completed").Error
			}
			if a.LeaseUntil != nil && a.LeaseUntil.After(now) {
				return nil
			}
			if a.RetryAfter != nil && a.RetryAfter.After(now) {
				return nil
			}
			return tx.Model(&existing).Update("op", "recovery").Error
		},
	)
}

// List returns the requesting user's imports within the authorized KB.
func (r *PersonalRecoveryRepository) List(
	ctx context.Context,
	after string,
	limit int,
) ([]types.FeishuPersonalAttempt, error) {
	var rows []types.FeishuPersonalAttempt
	err := r.db.WithContext(ctx).
		Where("id > ? AND status <> ?", after, "obsolete").
		Order("id").
		Limit(limit).
		Find(&rows).
		Error
	return rows, err
}

func (r *PersonalRecoveryRepository) lockPrincipal(
	tx *gorm.DB,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) error {
	if types.FeishuAuthMode(m.AuthMode) != types.FeishuAuthMode(a.AuthMode) {
		return ErrPersonalIdentity
	}
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		if m.OpenID != "" || a.GrantEpoch != "" || m.ApplicationID != m.AppID ||
			a.ApplicationID != m.AppID ||
			r.ApplicationGrantValidator == nil {
			return ErrPersonalIdentity
		}
		return r.ApplicationGrantValidator(tx, m, a)
	}
	if types.FeishuAuthMode(m.AuthMode) != types.FeishuUserOAuth || m.ApplicationID != "" ||
		a.ApplicationID != "" ||
		a.GrantEpoch == "" {
		return ErrPersonalIdentity
	}
	var connection types.FeishuConnection
	if err := tx.Clauses(
		clause.Locking{
			Strength: "SHARE",
		},
	).
		Where(
			"user_id = ? AND app_id = ? AND open_id = ?",

			m.UserID,

			m.AppID,

			m.OpenID,
		).
		First(
			&connection,
		).
		Error; err != nil {
		return err
	}
	var grant types.FeishuPersonalGrantEpoch
	if err := tx.Clauses(
		clause.Locking{
			Strength: "SHARE",
		},
	).
		Where(
			"user_id = ? AND epoch = ?",

			m.UserID,

			a.GrantEpoch,
		).
		First(
			&grant,
		).
		Error; err != nil {
		return ErrPersonalIdentity
	}
	return nil
}
