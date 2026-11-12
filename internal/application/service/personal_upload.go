package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type (
	personalFileAdmissionKey struct{}
	personalFileAdmission    struct{ ID, Hash string }
)

var personalUploadLocks [64]sync.Mutex

// PersonalUploadRequest contains file bytes and an idempotent creation identity.
type PersonalUploadRequest struct{ KBID, Filename, ContentBase64, IdempotencyKey string }

// withUploadLock serializes storage + INSERT admission across replicas using
// PostgreSQL transaction advisory locks. No schema upgrade or business row lock.
func (s *PersonalContent) withUploadLock(
	ctx context.Context,
	id string,
	fn func() (*types.Knowledge, error),
) (*types.Knowledge, error) {
	sum := sha256.Sum256([]byte(id))
	var row *types.Knowledge
	var err error
	if s.DB.Name() == "sqlite" {
		lock := &personalUploadLocks[sum[0]%64]
		lock.Lock()
		defer lock.Unlock()
		return fn()
	}
	if s.DB.Name() != "postgres" {
		return nil, access.ErrForbidden
	}
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Do not hold an admission connection indefinitely when an AI client retries.
		if e := tx.Exec("SET LOCAL lock_timeout='5s'").Error; e != nil {
			return e
		}
		if e := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(binary.BigEndian.Uint64(sum[:8]))).Error; e != nil {
			return e
		}
		row, err = fn()
		return err
	})
	if e != nil {
		return row, e
	}
	return row, err
}

// Upload validates file bytes and admits them into the existing asynchronous pipeline.
func (s *PersonalContent) Upload(ctx context.Context, req PersonalUploadRequest) (created *types.Knowledge, err error) {
	defer func() { s.auditContent(ctx, req.KBID, created, err) }()
	if len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 128 ||
		len(req.ContentBase64) > base64.StdEncoding.EncodedLen(PersonalUploadMaxBytes) {
		return nil, ErrPersonalUpload
	}
	if _, _, err = s.authorize(ctx, req.KBID); err != nil {
		return nil, err
	}
	// Strict canonical base64 avoids ignored whitespace and multiple encodings.
	data, err := base64.StdEncoding.Strict().DecodeString(req.ContentBase64)
	if err != nil || base64.StdEncoding.EncodeToString(data) != req.ContentBase64 {
		return nil, ErrPersonalUpload
	}
	if err = validatePersonalFile(req.Filename, data); err != nil {
		return nil, err
	}
	if s.Scanner != nil {
		if err = s.Scanner.Scan(ctx, data); err != nil {
			return nil, ErrPersonalUpload
		}
	}
	scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
	sum := sha256.Sum256(data)
	fingerprint := sha256.Sum256([]byte(req.KBID + "\x00" + req.Filename + "\x00" + hex.EncodeToString(sum[:])))
	hash := hex.EncodeToString(fingerprint[:])
	id := uuid.NewSHA1(uuid.NameSpaceOID,
		[]byte(fmt.Sprintf("knowhub:personal-content:%d:%s", scope.KeyID, req.IdempotencyKey))).
		String()
	return s.withUploadLock(ctx, id, func() (*types.Knowledge, error) {
		// Scanning and waiting on another replica cannot preserve old authorization.
		scoped, kb, e := s.authorize(ctx, req.KBID)
		if e != nil {
			return nil, e
		}
		old, e := s.contentReceipt(scoped, id)
		if e == nil {
			if old.Hash != hash || old.KBID != kb.ID || old.KeyID != fmt.Sprint(scope.KeyID) {
				return nil, ErrPersonalContentConflict
			}
			return old.knowledge(), nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, e
		}
		// Tombstones reserve the idempotency key; the PK rejects reuse after delete.
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, e := writer.CreateFormFile("file", req.Filename)
		if e != nil {
			return nil, e
		}
		if _, e = part.Write(data); e != nil {
			return nil, e
		}
		if e = writer.Close(); e != nil {
			return nil, e
		}
		// The bounded quarantine remains in memory; no temporary pathname is
		// accepted or persisted. RemoveAll also cleans any library spill fallback.
		reader := multipart.NewReader(bytes.NewReader(body.Bytes()), writer.Boundary())
		form, e := reader.ReadForm(PersonalUploadMaxBytes)
		if e != nil {
			return nil, e
		}
		defer func() { _ = form.RemoveAll() }()
		files := form.File["file"]
		if len(files) != 1 {
			return nil, ErrPersonalUpload
		}
		scoped = context.WithValue(scoped, personalFileAdmissionKey{}, personalFileAdmission{ID: id, Hash: hash})
		metadata := map[string]string{"creation_request_hash": hash, "mcp_key_id": fmt.Sprint(scope.KeyID)}
		row, e := s.Knowledge.CreateKnowledgeFromFile(
			scoped,
			kb.ID,
			files[0],
			metadata,
			nil,
			"",
			nil,
			types.ChannelAPI,
			nil,
		)
		return row, e
	})
}

type contentReceipt struct {
	ID                                           string
	TenantID                                     uint64
	KBID, ParseStatus, ErrorMessage, Hash, KeyID string
}

func (r contentReceipt) knowledge() *types.Knowledge {
	return &types.Knowledge{
		ID:              r.ID,
		TenantID:        r.TenantID,
		KnowledgeBaseID: r.KBID,
		ParseStatus:     r.ParseStatus,
		ErrorMessage:    r.ErrorMessage,
	}
}

func (s *PersonalContent) contentReceipt(ctx context.Context, id string) (contentReceipt, error) {
	var row contentReceipt
	hash, key := "metadata->>'creation_request_hash'", "metadata->>'mcp_key_id'"
	if s.DB.Name() == "sqlite" {
		hash = "json_extract(metadata,'$.creation_request_hash')"
		key = "json_extract(metadata,'$.mcp_key_id')"
	}
	err := s.DB.WithContext(ctx).
		Table("knowledges").
		Select("id,tenant_id,knowledge_base_id AS kb_id,parse_status,error_message,"+hash+" AS hash,"+key+" AS key_id").
		Where("id=? AND deleted_at IS NULL", id).
		Take(&row).
		Error
	return row, err
}

// Status is a receipt-only read for documents created by this exact Key. It
// rechecks all write admission facts and never discloses a document body/path.
func (s *PersonalContent) Status(ctx context.Context, id string) (*types.Knowledge, error) {
	key, e := s.credential(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = uuid.Parse(id); e != nil {
		return nil, access.ErrForbidden
	}
	row, e := s.contentReceipt(ctx, id)
	if e != nil || row.KeyID != fmt.Sprint(key.ID) {
		return nil, access.ErrForbidden
	}
	if _, _, e = s.authorize(ctx, row.KBID); e != nil {
		return nil, e
	}
	return row.knowledge(), nil
}

func (s *PersonalContent) auditContent(ctx context.Context, kb string, row *types.Knowledge, result error) {
	if s.Audit == nil {
		return
	}
	scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
	caller := types.CallerFromContext(ctx)
	outcome := types.AuditOutcomeAccepted
	id := ""
	tenant := caller.TenantID
	if result != nil {
		outcome = types.AuditOutcomeDenied
	}
	if row != nil {
		id = row.ID
		tenant = row.TenantID
		if row.ParseStatus == "failed" {
			outcome = types.AuditOutcomeFailed
		}
	}
	details, _ := json.Marshal(map[string]any{"key_id": scope.KeyID})
	audit, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = s.Audit.Log(
		audit,
		&types.AuditLog{
			TenantID:    tenant,
			ActorUserID: caller.UserID,
			Action:      "mcp.file_added",
			ScopeType:   "knowledge_base",
			ScopeID:     kb,
			TargetType:  "knowledge",
			TargetID:    id,
			Outcome:     outcome,
			Details:     types.JSON(details),
		},
	)
}

// ResolveContentSelector accepts an exact writable ID or an unambiguous name.
func (s *PersonalContent) ResolveContentSelector(ctx context.Context, selector string) (string, error) {
	rows, e := s.ListWritable(ctx)
	if e != nil {
		return "", e
	}
	match := ""
	for _, row := range rows {
		if row.ID == selector {
			return row.ID, nil
		}
		if strings.EqualFold(row.Name, strings.TrimSpace(selector)) {
			if match != "" {
				return "", errors.New("ambiguous knowledge base name; select from list_writable_knowledge_bases")
			}
			match = row.ID
		}
	}
	if match == "" {
		return "", access.ErrForbidden
	}
	return match, nil
}
