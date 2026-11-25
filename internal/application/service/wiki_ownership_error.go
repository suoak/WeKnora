package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Error() is safe even when Asynq or an API logs it verbatim. Keep the cause
// available to explicit internal diagnostics, without changing errors.Is-based
// queue/ownership policy: this branch previously returned an opaque error.
type wikiOwnershipReadError struct {
	cause error
	class string
}

func (e *wikiOwnershipReadError) Error() string {
	return "wiki first-add post-processing ownership read failed [" + e.class + "]"
}
func (e *wikiOwnershipReadError) Cause() error { return e.cause }

func wikiOwnershipErrorClass(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, sql.ErrNoRows), errors.Is(err, repository.ErrKnowledgeNotFound):
		return "not_found"
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, sql.ErrConnDone):
		return "database_connection"
	}
	var connection *net.OpError
	if errors.As(err, &connection) {
		return "database_connection"
	}
	var connect *pgconn.ConnectError
	if errors.As(err, &connect) {
		return "database_connection"
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && (strings.HasPrefix(state.SQLState(), "08") || state.SQLState() == "57P01" || state.SQLState() == "57P02" || state.SQLState() == "57P03") {
		return "database_connection"
	}
	return "query_error"
}

func wikiDiagnosticID(id string) string {
	if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id {
		return id
	}
	return "invalid"
}

func wikiOwnershipFailure(ctx context.Context, kb string, op WikiPendingOp, cause error) error {
	class := wikiOwnershipErrorClass(cause)
	logger.Errorf(ctx, "[WikiOwnership] stage=first_add_postprocess operation_id=%d kb=%s knowledge=%s class=%s", op.dbID, wikiDiagnosticID(kb), wikiDiagnosticID(op.KnowledgeID), class)
	return &wikiOwnershipReadError{cause: cause, class: class}
}

// No settling, slot decrement or publication is allowed after a failed read.
func (s *wikiIngestService) firstAddOwnership(ctx context.Context, kb string, op WikiPendingOp) (bool, error) {
	first, err := s.isMVPWikiKnowledge(ctx, op.KnowledgeID)
	if err != nil {
		return false, wikiOwnershipFailure(ctx, kb, op, err)
	}
	return first, nil
}
