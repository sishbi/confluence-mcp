package confluencemcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

// pageResolver implements mdconv.Resolver for a single in-flight page
// conversion. It is bound to one pageID at construction time so the
// `children` macro (which Confluence always evaluates relative to its
// enclosing page) resolves to the correct parent. User lookups are cached
// per-conversion to avoid hammering the /user endpoint when a page mentions
// the same account repeatedly. The cache lives for the resolver's lifetime
// only; each new page conversion builds a fresh resolver.
type pageResolver struct {
	client  ConfluenceClient
	ctx     context.Context
	baseURL string
	pageID  string

	mu    sync.Mutex
	users map[string]string // accountID -> displayName ("" when not found)
}

// newPageResolver returns a resolver bound to pageID. baseURL should be the
// Confluence host (without trailing slash) used to build child-page URLs;
// when empty, child links render as title-only bullets.
func newPageResolver(ctx context.Context, client ConfluenceClient, baseURL, pageID string) *pageResolver {
	return &pageResolver{
		client:  client,
		ctx:     ctx,
		baseURL: strings.TrimRight(baseURL, "/"),
		pageID:  pageID,
		users:   make(map[string]string),
	}
}

// ResolveUser satisfies mdconv.Resolver. Returns the cached display name on a
// hit, or queries the Confluence /user endpoint and caches the result. A
// failed lookup is cached as the empty string so subsequent mentions of the
// same account don't retry.
func (r *pageResolver) ResolveUser(accountID string) (string, bool) {
	if r == nil || accountID == "" {
		return "", false
	}
	r.mu.Lock()
	if name, ok := r.users[accountID]; ok {
		r.mu.Unlock()
		return name, name != ""
	}
	r.mu.Unlock()

	user, err := r.client.GetUser(r.ctx, accountID)
	name := ""
	if err == nil && user != nil {
		name = user.DisplayName
	}

	r.mu.Lock()
	r.users[accountID] = name
	r.mu.Unlock()

	return name, name != ""
}

// ListChildren satisfies mdconv.Resolver. parentPageID is accepted for
// interface compatibility but ignored — the children macro always resolves
// relative to the page the macro lives on, which the resolver already knows.
// depth is capped at 3 to avoid pathological recursion.
func (r *pageResolver) ListChildren(parentPageID string, depth int) ([]mdconv.ChildPage, error) {
	if r == nil {
		return nil, fmt.Errorf("nil resolver")
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	target := parentPageID
	if target == "" {
		target = r.pageID
	}
	return r.listChildrenRecursive(target, depth)
}

func (r *pageResolver) listChildrenRecursive(pageID string, depth int) ([]mdconv.ChildPage, error) {
	pages, _, err := r.client.GetPageChildren(r.ctx, pageID, &confluence.ListOptions{Limit: 100})
	if err != nil {
		return nil, err
	}
	out := make([]mdconv.ChildPage, 0, len(pages))
	for _, p := range pages {
		child := mdconv.ChildPage{
			ID:    p.ID,
			Title: p.Title,
			URL:   r.pageURL(p.ID),
		}
		if depth > 1 {
			sub, err := r.listChildrenRecursive(p.ID, depth-1)
			if err == nil {
				child.Children = sub
			}
		}
		out = append(out, child)
	}
	return out, nil
}

func (r *pageResolver) pageURL(pageID string) string {
	if r.baseURL == "" || pageID == "" {
		return ""
	}
	return r.baseURL + "/wiki/pages/viewpage.action?pageId=" + pageID
}
