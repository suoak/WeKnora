package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

// SetSourceDocumentReparseRecovery installs an independent, bounded wake-up sweep.
// Generic document dispatch recovery does not depend on this Feishu callback.
func (h *HousekeepingService) SetSourceDocumentReparseRecovery(recoverDocuments func(context.Context) error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sourceDocumentRecoveryRegistered {
		return nil
	}
	_, err := h.cron.AddFunc("0 * * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := recoverDocuments(ctx); err != nil {
			logger.Warnf(ctx, "[Housekeeping] source document reparse recovery deferred")
		}
	})
	if err == nil {
		h.sourceDocumentRecoveryRegistered = true
	}
	return err
}
