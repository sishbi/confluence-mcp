package confluencemcp

import (
	"context"

	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

// commentKind labels a rendered comment thread as originating from the
// footer comment API or the inline comment API. Typed (rather than a bare
// string) so an omitted or swapped value is a compile error instead of a
// silently blank or mislabelled "(type: ...)" line — that label is the
// comment_type an agent feeds back to reply_comment (D3). Lives here rather
// than in tool_read.go (its only current caller) so tool_write.go's
// reply_comment validation (D6) can use the same type without an import
// cycle between the two tool_*.go files.
type commentKind string

const (
	commentFooter commentKind = "footer"
	commentInline commentKind = "inline"
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
