package confluencemcp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

const maxPageSize = 20_000
const cacheTTL = 60 * time.Second
const macrosCacheTTL = 5 * time.Minute

type handlers struct {
	client ConfluenceClient
	cache  pageCache
	log    *slog.Logger
}

// logger returns h.log if set, otherwise slog.Default().
func (h *handlers) logger() *slog.Logger {
	if h.log != nil {
		return h.log
	}
	return slog.New(slog.DiscardHandler)
}

type section struct {
	Heading string
	Level   int
	Start   int
	End     int
}

type cachedPage struct {
	pageID    string
	markdown  string
	sections  []section
	macros    *mdconv.MacroRegistry
	fetchedAt time.Time
}

type pageCache struct {
	mu      sync.Mutex
	entries map[string]*cachedPage
}

func (c *pageCache) get(pageID string) (*cachedPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return nil, false
	}
	entry, ok := c.entries[pageID]
	if !ok {
		return nil, false
	}
	ttl := cacheTTL
	if entry.macros != nil && len(entry.macros.Entries) > 0 {
		ttl = macrosCacheTTL
	}
	if time.Since(entry.fetchedAt) > ttl {
		delete(c.entries, pageID)
		return nil, false
	}
	return entry, true
}

func (c *pageCache) getMacroRegistry(pageID string) *mdconv.MacroRegistry {
	entry, ok := c.get(pageID)
	if !ok {
		return nil
	}
	return entry.macros
}

func (c *pageCache) put(entry *cachedPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*cachedPage)
	}
	c.entries[entry.pageID] = entry
}

func (c *pageCache) evict(pageID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, pageID)
}

// ensureMacroRegistry returns the cached MacroRegistry for a page, fetching
// and processing the page if it is not already in cache.
func (h *handlers) ensureMacroRegistry(ctx context.Context, pageID string) *mdconv.MacroRegistry {
	if reg := h.cache.getMacroRegistry(pageID); reg != nil {
		h.logger().DebugContext(ctx, "cache_hit", "page_id", pageID, "type", "macro_registry")
		return reg
	}
	h.logger().DebugContext(ctx, "cache_miss", "page_id", pageID, "type", "macro_registry")
	// Cache miss — re-fetch and re-extract.
	page, err := h.client.GetPage(ctx, pageID)
	if err != nil {
		return nil
	}
	_ = h.processPage(ctx, page)
	return h.cache.getMacroRegistry(pageID)
}
