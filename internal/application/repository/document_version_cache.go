package repository

import (
	"container/list"
	"sync"
	"time"
)

// Cache only the canonical content-version calculation, never an authorization
// decision. Every lookup follows a fresh read of the document and its database
// trigger-maintained epoch in the same read-only snapshot. Policies, credentials,
// membership, Portal state and source denials are deliberately not cached.
type documentVersionStamp struct {
	ID, KBID, Incarnation string
	TenantID              uint64
	Generation            int64
	UpdatedAt             time.Time
}

type documentVersionEntry struct {
	stamp   documentVersionStamp
	version string
}

type documentVersionCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	order    list.List
}

func newDocumentVersionCache(capacity int) *documentVersionCache {
	return &documentVersionCache{capacity: capacity, entries: make(map[string]*list.Element)}
}

func (c *documentVersionCache) get(stamp documentVersionStamp) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[stamp.ID]
	if entry == nil || entry.Value.(documentVersionEntry).stamp != stamp {
		return "", false
	}
	c.order.MoveToFront(entry)
	return entry.Value.(documentVersionEntry).version, true
}

func (c *documentVersionCache) put(stamp documentVersionStamp, version string) {
	if c == nil || c.capacity <= 0 || version == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	value := documentVersionEntry{stamp: stamp, version: version}
	if entry := c.entries[stamp.ID]; entry != nil {
		entry.Value = value
		c.order.MoveToFront(entry)
		return
	}
	c.entries[stamp.ID] = c.order.PushFront(value)
	if c.order.Len() > c.capacity {
		old := c.order.Back()
		delete(c.entries, old.Value.(documentVersionEntry).stamp.ID)
		c.order.Remove(old)
	}
}
