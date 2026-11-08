package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// MCPUsageRecorder durably hands off privacy-filtered final invocations to
// the existing ledger. Files contain no credentials, arguments or responses.
// Mount MCP_USAGE_SPOOL_DIR on persistent local storage, one writer per replica.
type MCPUsageRecorder struct {
	svc   *usageAnalyticsService
	dir   string
	mu    sync.Mutex
	count int
	wake  chan struct{}
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

type mcpUsageEnvelope struct {
	Event    *types.MCPUsageEvent `json:"event"`
	Report   types.MCPUsageReport `json:"resources"`
	Attempts int                  `json:"attempts"`
}

// NewMCPUsageRecorder opens the durable bounded spool and starts delivery.
func NewMCPUsageRecorder(repo interfaces.UsageAnalyticsRepository) *MCPUsageRecorder {
	dir := os.Getenv("MCP_USAGE_SPOOL_DIR")
	if dir == "" {
		dir = filepath.Join("data", "mcp-usage-spool")
	}
	r := newMCPUsageRecorder(repo, dir)
	go r.run()
	return r
}

func newMCPUsageRecorder(repo interfaces.UsageAnalyticsRepository, dir string) *MCPUsageRecorder {
	r := &MCPUsageRecorder{
		svc:  &usageAnalyticsService{repo: repo},
		dir:  dir,
		wake: make(chan struct{}, 1),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Errorf(context.Background(), "[mcp-usage] spool unavailable; collection degraded")
	}
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".active") {
			active := filepath.Join(dir, f.Name())
			final := strings.TrimSuffix(active, ".active") + ".json"
			if _, err := os.Stat(final); err == nil {
				_ = os.Remove(active)
				continue
			}
			var item mcpUsageEnvelope
			if b, err := os.ReadFile(active); err == nil && json.Unmarshal(b, &item) == nil && item.Event != nil {
				item.Event.Success = false
				item.Event.ErrorCode = "interrupted"
				item.Event.LatencyMs = 0
				if writeMCPEnvelope(final, item) == nil {
					_ = os.Remove(active)
				}
			}
		}
		if !f.IsDir() {
			r.count++
		}
	}
	return r
}

// Record returns only a local spool error. Collection never changes tool behavior.
func (r *MCPUsageRecorder) Record(ctx context.Context, report types.MCPUsageReport) error {
	return r.spool(ctx, report, false)
}

// Begin persists an admission marker before execution for interruption recovery.
func (r *MCPUsageRecorder) Begin(ctx context.Context, report types.MCPUsageReport) error {
	return r.spool(ctx, report, true)
}

func (r *MCPUsageRecorder) spool(ctx context.Context, report types.MCPUsageReport, active bool) error {
	if r == nil {
		return nil
	}
	event, err := prepareInboundMCPEvent(ctx, report)
	if err != nil {
		return err
	}
	// The envelope needs only resource IDs; identity is frozen in Event.
	resources := types.MCPUsageReport{
		KnowledgeBaseIDs: report.KnowledgeBaseIDs, KnowledgeIDs: report.KnowledgeIDs,
		TargetKnowledgeBaseIDs: report.TargetKnowledgeBaseIDs, HitKnowledgeBaseIDs: report.HitKnowledgeBaseIDs,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	digest := sha256.Sum256([]byte(event.EventKey))
	base := filepath.Join(r.dir, fmt.Sprintf("%x", digest))
	_, existing := os.Stat(base + ".active")
	if existing != nil {
		_, existing = os.Stat(base + ".json")
	}
	if r.count >= 10000 && existing != nil {
		return fmt.Errorf("MCP usage spool full")
	}
	name := base + ".json"
	if active {
		name = base + ".active"
	}
	if err := writeMCPEnvelope(name, mcpUsageEnvelope{Event: event, Report: resources}); err != nil {
		return err
	}
	if existing != nil {
		r.count++
	}
	if active {
		return nil
	}
	_ = os.Remove(base + ".active")
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return nil
}

func writeMCPEnvelope(name string, item mcpUsageEnvelope) error {
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(name+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name + ".tmp")
		return err
	}
	if err = os.Rename(name+".tmp", name); err != nil {
		_ = os.Remove(name + ".tmp")
		return err
	}
	if dir, openErr := os.Open(filepath.Dir(name)); openErr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (r *MCPUsageRecorder) drain() {
	files, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}
	processed := 0
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		if processed >= 100 {
			break
		}
		processed++
		name := filepath.Join(r.dir, f.Name())
		b, err := os.ReadFile(name)
		var item mcpUsageEnvelope
		if err == nil {
			err = json.Unmarshal(b, &item)
		}
		if err == nil && item.Event != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = r.svc.recordMCPEvent(ctx, item.Event, item.Report)
			cancel()
		} else {
			err = fmt.Errorf("invalid spool record")
		}
		if err != nil {
			item.Attempts++
			logger.Errorf(
				context.Background(),
				"[mcp-usage] delivery failed; attempt=%d pending_file=%s",
				item.Attempts,
				f.Name(),
			)
			if item.Attempts >= 5 || item.Event == nil {
				_ = os.Rename(name, name+".dead")
			} else {
				_ = writeMCPEnvelope(name, item)
			}
			continue
		}
		if os.Remove(name) == nil {
			r.mu.Lock()
			r.count--
			r.mu.Unlock()
		}
	}
}

func (r *MCPUsageRecorder) run() {
	defer close(r.done)
	r.drain()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.stop:
			return // queued events survive shutdown; no unbounded flush
		case <-r.wake:
			r.drain()
		case <-tick.C:
			r.drain()
		}
	}
}

// Close stops delivery with a bounded wait while preserving pending records.
func (r *MCPUsageRecorder) Close() error {
	r.once.Do(func() { close(r.stop) })
	select {
	case <-r.done:
		return nil
	case <-time.After(6 * time.Second):
		return fmt.Errorf("MCP usage collector stopping; pending events retained")
	}
}
