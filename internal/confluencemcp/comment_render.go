package confluencemcp

import (
	"context"

	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

// commentRenderer returns a closure that converts a comment's storage-format
// body to Markdown with user mentions and page references resolved. The
// closure shares a single pageResolver so callers rendering multiple comments
// on the same page pay at most one /user lookup per unique account id.
func (h *handlers) commentRenderer(ctx context.Context, pageID string) func(storage string) string {
	resolver := newPageResolver(ctx, h.client, h.client.BaseURL(), pageID)
	return func(storage string) string {
		md, _, _ := mdconv.ToMarkdownWithMacrosResolved(storage, resolver)
		return md
	}
}
