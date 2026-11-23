package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// A native PDF converter may ignore cancellation. Keep its slot until it
// actually returns, even when its source execution has stopped waiting. This
// bounds abandoned PDF calls without blocking the source's XLSX reparses.
var feishuPDFReaderSlot = make(chan struct{}, 1)

type feishuPDFReadResult struct {
	result *types.ReadResult
	err    error
}

func (s *knowledgeService) readFeishuCandidate(
	ctx context.Context, reader interfaces.DocReader, request *types.ReadRequest,
	renew func(context.Context) error,
) (*types.ReadResult, error) {
	if request.FileType != "pdf" {
		return s.callDocReaderWithTimeout(ctx, reader, request)
	}
	limit := 30 * time.Minute
	if s.config != nil && s.config.KnowledgeBase != nil && s.config.KnowledgeBase.DocReaderCallTimeout > 0 {
		limit = s.config.KnowledgeBase.DocReaderCallTimeout
	}
	return readBoundedFeishuPDF(ctx, limit, time.Minute, renew, func(work context.Context) (*types.ReadResult, error) {
		return s.callDocReaderWithTimeout(work, reader, request)
	})
}

func readBoundedFeishuPDF(
	ctx context.Context, limit, renewalInterval time.Duration, renew func(context.Context) error,
	read func(context.Context) (*types.ReadResult, error),
) (*types.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case feishuPDFReaderSlot <- struct{}{}:
	default:
		return nil, &feishuStagingRefusal{reason: "parser_busy"}
	}
	work, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	if err := renew(work); err != nil {
		<-feishuPDFReaderSlot
		return nil, feishuPDFRenewalError(ctx, work, err)
	}
	done := make(chan feishuPDFReadResult, 1)
	go func() {
		var out feishuPDFReadResult
		defer func() {
			if recover() != nil {
				out = feishuPDFReadResult{err: errors.New("feishu PDF parser failed")}
			}
			<-feishuPDFReaderSlot
			done <- out // Buffered: an abandoned native result cannot block or publish.
		}()
		out.result, out.err = read(work)
	}()
	ticker := time.NewTicker(renewalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-work.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, &feishuStagingRefusal{reason: "parser_timeout"}
		case <-ticker.C:
			if err := renew(work); err != nil {
				return nil, feishuPDFRenewalError(ctx, work, err)
			}
		case out := <-done:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if work.Err() != nil {
				return nil, &feishuStagingRefusal{reason: "parser_timeout"}
			}
			return out.result, out.err
		}
	}
}

func feishuPDFRenewalError(parent, work context.Context, err error) error {
	if parent.Err() == nil && errors.Is(work.Err(), context.DeadlineExceeded) &&
		errors.Is(err, context.DeadlineExceeded) {
		return &feishuStagingRefusal{reason: "parser_timeout"}
	}
	return err
}
