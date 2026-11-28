package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

const sourceReparseFailureScope = "feishu_reparse_failure_v1"

type sourceReparseFailureKey struct{}

type sourceReparseFailureFacts struct {
	Fingerprint          string `json:"fingerprint"`
	ErrorType            string `json:"error_type"`
	FingerprintTruncated bool   `json:"fingerprint_truncated,omitempty"`
	SQLState             string `json:"sql_state,omitempty"`
	Timeout              bool   `json:"timeout,omitempty"`
	Canceled             bool   `json:"canceled,omitempty"`
	BudgetBusy           bool   `json:"budget_busy,omitempty"`
}

// WithSourceDocumentReparseFailure captures content-free diagnostic facts only.
// It grants no execution authority and never changes retry or progress decisions.
// Raw error text, SQL, provider bodies and credentials are not persisted.
func WithSourceDocumentReparseFailure(ctx context.Context, err error, budgetBusy bool) context.Context {
	if err == nil {
		return ctx
	}
	message := err.Error()
	truncated := len(message) > 64<<10
	if truncated {
		message = message[:64<<10]
	}
	digest := sha256.Sum256([]byte(message))
	facts := sourceReparseFailureFacts{
		Fingerprint: hex.EncodeToString(digest[:]), ErrorType: fmt.Sprintf("%T", err),
		Timeout:  errors.Is(err, context.DeadlineExceeded),
		Canceled: errors.Is(err, context.Canceled), BudgetBusy: budgetBusy,
		FingerprintTruncated: truncated,
	}
	if len(facts.ErrorType) > 200 {
		facts.ErrorType = "unclassified_error"
	}
	for _, r := range facts.ErrorType {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit := r >= '0' && r <= '9'
		if !letter && !digit && r != '_' && r != '.' && r != '*' && r != '[' && r != ']' {
			facts.ErrorType = "unclassified_error"
			break
		}
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		value := state.SQLState()
		valid := len(value) == 5
		for _, r := range value {
			valid = valid && (r >= '0' && r <= '9' || r >= 'A' && r <= 'Z')
		}
		if valid {
			facts.SQLState = value
		}
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) {
		facts.Timeout = facts.Timeout || timeout.Timeout()
	}
	return context.WithValue(ctx, sourceReparseFailureKey{}, facts)
}

// Called only after the existing execution/SQL request claim has been checked,
// inside the same transaction as its failure state update. A separate recorded
// scope survives delivery/exhaustion errors without changing request payloads or
// becoming a task that older application versions could dispatch on rollback.
func recordSourceDocumentReparseFailure(
	ctx context.Context, tx *gorm.DB, ds *types.DataSource, request *types.FeishuDocumentReparse,
	code string, busy bool,
) error {
	facts, present := ctx.Value(sourceReparseFailureKey{}).(sourceReparseFailureFacts)
	if !present || busy {
		return nil
	}
	validCode := len(code) > 0 && len(code) <= 96
	for _, r := range code {
		validCode = validCode && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	}
	if !validCode {
		code = "unclassified_processing_error"
	}
	const taskType = "feishu:reparse_failure_audit"
	key := "attempt:" + strconv.Itoa(request.Attempts)
	var count int64
	if err := tx.Model(&types.TaskPendingOp{}).Where(
		"tenant_id=? AND task_type=? AND scope=? AND scope_id=? AND dedup_key=?",
		ds.TenantID, taskType, sourceReparseFailureScope, request.ID, key,
	).Count(&count).Error; err != nil || count != 0 {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"request_id": request.ID, "data_source_id": ds.ID, "knowledge_id": request.KnowledgeID,
		"attempt": request.Attempts, "execution_id": request.ExecutionID,
		"execution_generation": request.Generation, "failure_code": code, "facts": facts,
	})
	if err != nil {
		return err
	}
	return tx.Create(&types.TaskPendingOp{
		TenantID: ds.TenantID, TaskType: taskType, Scope: sourceReparseFailureScope,
		ScopeID: request.ID, DedupKey: key, Op: "recorded", Payload: payload,
		EnqueuedAt: time.Now().UTC(),
	}).Error
}
