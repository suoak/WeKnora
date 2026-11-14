package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type wikiBrowserSnapshotKey struct{}

type wikiBrowserSnapshot struct {
	kb   string
	base context.Context
}

// Discard an inconsistent result and rebuild the entire browser read proof
// once. Never reuse its page allowlist, suppress a proof failure, or retry a
// mutation, API-key request or cross-workspace shared read.
func readWikiBrowserSnapshot[T any](
	c *gin.Context, h *WikiPageHandler, read func(context.Context) (T, error),
) (T, error) {
	var zero T
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.Request.Context().Err(); err != nil {
			return zero, err
		}
		result, err := read(c.Request.Context())
		if err == nil {
			if err := c.Request.Context().Err(); err != nil {
				return zero, err
			}
			return result, nil
		}
		snapshot, verified := c.Request.Context().Value(wikiBrowserSnapshotKey{}).(wikiBrowserSnapshot)
		if attempt == 1 || !errors.Is(err, repository.ErrWikiMCPInputsChanged) ||
			c.Request.Method != http.MethodGet || !verified || snapshot.kb != c.Param("kb_id") {
			return zero, err
		}
		if err := snapshot.base.Err(); err != nil {
			return zero, err
		}
		// Drop the old proof before even looking up the KB again. Reusing it
		// could fail that lookup or retain a stale page allowlist.
		c.Request = c.Request.WithContext(snapshot.base)
		if _, _, err = h.validateWikiKB(c); err != nil {
			return zero, err
		}
		fresh, verified := c.Request.Context().Value(wikiBrowserSnapshotKey{}).(wikiBrowserSnapshot)
		if !verified || fresh.kb != snapshot.kb {
			return zero, types.ErrLegacySourceRestricted
		}
	}
	return zero, repository.ErrWikiMCPInputsChanged
}

func writeWikiReadError(c *gin.Context, fallback int, err error) {
	if errors.Is(err, repository.ErrWikiMCPInputsChanged) && c.Request.Method == http.MethodGet {
		c.Header("Retry-After", "1")
		c.JSON(http.StatusConflict, gin.H{
			"error": "Wiki content changed during this read. Please retry.",
			"code":  "wiki_snapshot_changed", "retryable": true,
		})
		return
	}
	var app *apperrors.AppError
	if errors.As(err, &app) && app.HTTPCode >= 400 && app.HTTPCode <= 599 {
		fallback = app.HTTPCode
	}
	c.JSON(fallback, gin.H{"error": err.Error()})
}
