package repository

import (
	"context"
	"errors"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type mcpEndpointRepository struct {
	db *gorm.DB
}

// NewMCPEndpointRepository creates a GORM-backed MCP endpoint repository.
func NewMCPEndpointRepository(db *gorm.DB) interfaces.MCPEndpointRepository {
	return &mcpEndpointRepository{db: db}
}

func (r *mcpEndpointRepository) Create(ctx context.Context, ep *types.MCPEndpoint) error {
	return endpointWriteError(r.quietDB(ctx).Create(ep).Error)
}

// Credential predicates must not be interpolated into ORM error logs. Tests
// collect parameter-free Statement.SQL and RowsAffected via callbacks instead.
func (r *mcpEndpointRepository) quietDB(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)})
}

func endpointWriteError(err error) error {
	if err != nil {
		// Driver errors may contain bound parameters. Keep the stable error class
		// without propagating the raw SQL/provider text to handlers or logs.
		return apperrors.NewInternalServerError("mcp endpoint storage unavailable")
	}
	return nil
}

func (r *mcpEndpointRepository) GetByID(ctx context.Context, id string) (*types.MCPEndpoint, error) {
	var ep types.MCPEndpoint
	err := r.quietDB(ctx).Where("id = ?", id).First(&ep).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, endpointWriteError(err)
	}
	return &ep, nil
}

func (r *mcpEndpointRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*types.MCPEndpoint, error) {
	if tokenHash == "" {
		return nil, nil
	}
	var ep types.MCPEndpoint
	err := r.quietDB(ctx).Where("token_hash = ?", tokenHash).First(&ep).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, endpointWriteError(err)
	}
	return &ep, nil
}

func (r *mcpEndpointRepository) ListByTenant(ctx context.Context, tenantID uint64) ([]*types.MCPEndpoint, error) {
	var rows []*types.MCPEndpoint
	err := r.quietDB(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&rows).Error
	return rows, endpointWriteError(err)
}

func (r *mcpEndpointRepository) Update(ctx context.Context, ep *types.MCPEndpoint) error {
	// ep is the loaded snapshot with requested config fields applied. UpdatedAt
	// and TokenHash remain the expected version, not writable config fields.
	if ep == nil || ep.ID == "" || ep.TenantID == 0 || ep.UpdatedAt.IsZero() {
		return endpointConflict()
	}
	next := nextEndpointVersion(ep.UpdatedAt)
	result := r.expectedVersion(ctx, ep).Updates(map[string]interface{}{
		"name": ep.Name, "description": ep.Description, "enabled": ep.Enabled,
		"knowledge_base_ids": endpointArray(ep.KnowledgeBaseIDs), "tools": endpointArray(ep.Tools),
		"default_agent_id": ep.DefaultAgentID, "rate_limit_per_minute": ep.RateLimitPerMinute,
		"updated_at": next,
	})
	if err := endpointMutationResult(result); err != nil {
		return err
	}
	ep.UpdatedAt = next
	return nil
}

func endpointArray(value types.StringArray) types.StringArray {
	if value == nil {
		return types.StringArray{}
	}
	return value
}

// Increment beyond the loaded timestamp even within one clock tick or after
// clock rollback. Microsecond alignment survives PostgreSQL timestamptz(6).
// All config/credential writers use this CAS; last_used_at intentionally does not.
func nextEndpointVersion(previous time.Time) time.Time {
	floor := previous.UTC().Truncate(time.Microsecond).Add(time.Microsecond)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if now.Before(floor) {
		return floor
	}
	return now
}

func endpointConflict() error {
	return apperrors.NewConflictError("mcp endpoint changed or no longer exists; reload before retrying")
}

func (r *mcpEndpointRepository) expectedVersion(ctx context.Context, ep *types.MCPEndpoint) *gorm.DB {
	return r.quietDB(ctx).Model(&types.MCPEndpoint{}).
		Where("tenant_id = ? AND id = ? AND deleted_at IS NULL AND updated_at = ? AND token_hash = ?",
			ep.TenantID, ep.ID, ep.UpdatedAt, ep.TokenHash)
}

func endpointMutationResult(result *gorm.DB) error {
	if err := endpointWriteError(result.Error); err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return endpointConflict()
	}
	return nil
}

func (r *mcpEndpointRepository) RotateToken(ctx context.Context, ep *types.MCPEndpoint, hash, hint string) error {
	if ep == nil || ep.ID == "" || ep.TenantID == 0 || ep.UpdatedAt.IsZero() || hash == "" {
		return endpointConflict()
	}
	next := nextEndpointVersion(ep.UpdatedAt)
	result := r.expectedVersion(ctx, ep).Updates(map[string]interface{}{
		"token_hash": hash, "token_hint": hint, "updated_at": next,
	})
	if err := endpointMutationResult(result); err != nil {
		return err
	}
	ep.TokenHash, ep.TokenHint, ep.UpdatedAt = hash, hint, next
	return nil
}

func (r *mcpEndpointRepository) Delete(ctx context.Context, tenantID uint64, id string) error {
	result := r.quietDB(ctx).
		Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, id).
		Delete(&types.MCPEndpoint{})
	return endpointMutationResult(result)
}

func (r *mcpEndpointRepository) TouchLastUsed(ctx context.Context, id string) error {
	now := time.Now()
	return endpointWriteError(r.quietDB(ctx).
		Model(&types.MCPEndpoint{}).
		Where("id = ?", id).
		UpdateColumn("last_used_at", now).Error)
}
