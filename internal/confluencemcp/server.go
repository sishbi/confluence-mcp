package confluencemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sishbi/confluence-mcp/internal/confluence"
)

var readTool = &mcp.Tool{
	Name: "confluence_read",
	Description: `Fetch Confluence data. Modes (provide exactly one):

1. page_ids — Fetch pages by ID.
2. url — Fetch a page by Confluence URL. Supports focusedCommentId query parameter.
3. cql — Search via CQL query.
4. resource — List a resource: "spaces", "children" (needs page_id), "comments" (needs page_id), "inline_comments" (needs page_id), "labels" (needs page_id).

Options: format ("markdown" default, or "storage" for raw XHTML), section (extract a heading section, fetching the page itself on a cold cache — accepts page_id or a single-element page_ids; needs exactly one page id), limit (default 100), next_page_token (accepts page_id or page_ids alongside it, but they must name the same page the token already carries — a mismatch or more than one id is rejected; a url naming the same page also continues the read, but a focusedCommentId permalink does not), resolution_status (resource: "inline_comments" only — filter by "open", "resolved", "dangling", or "reopened").
Long pages are automatically chunked — if truncated, a table of contents is shown with section names you can request individually.
Use format=storage when you need to add or modify Confluence macros directly.`,
}

var writeTool = &mcp.Tool{
	Name: "confluence_write",
	Description: `Modify Confluence data. Batch-first: pass an array of items even for single operations.

Actions:
- create: Create pages. Each item needs: space_id, title. Optional: body (Markdown), parent_id, status (current/draft), page_id (source page whose macro registry to reuse when body carries <!-- macro:mN --> sentinels).
- update: Update pages. Each item needs: page_id, title, version_number. Optional: body (Markdown), status. Replaces the full body.
- append: Insert or replace a fragment in an existing page WITHOUT sending the full body — the server splices it into the current storage and writes the merged result. ~100× smaller than update for typical edits.
  Required: page_id, body (Markdown by default; storage if format="storage"). Optional: position (default "end"), version_number (optimistic concurrency), heading, include_subsections, new_heading.
  heading is required for after_heading / end_of_section / replace_section (exact, case-sensitive) and REJECTED for every other position, including "end".
  Heading-scoped positions:
    after_heading — insert at the TOP of the section, above its existing content.
    end_of_section — insert at the BOTTOM, before the next same-or-higher heading. Use this for a new sibling section; after_heading would displace the target's body into it. A leading heading in your fragment is kept, creating that new section.
    replace_section — replace the section's content up to the next heading of ANY level, so subsections survive and your fragment need not repeat them; include_subsections=true (valid only here) replaces them too. Keeps the target heading and strips a matching leading heading from your fragment. Preview and response name the subsections replaced or preserved.
    new_heading (replace_section only) — rename the target heading in place (plain text, level unchanged) while replacing its content. Rejected if the new text already names another heading on the page, or if the heading holds a mention, macro, or emoticon (bold/code is fine, and is replaced along with the words). Breaks links anchored to the old text; the response names the on-page ones, but cannot see other pages.
  Structure-scoped positions — both REJECT heading, include_subsections, and new_heading:
    start — insert at the page start, or just past the FIRST layout-cell's opening tag on a layout-wrapped page. Works on a page with no headings.
    replace_preamble — replace everything before the first heading; the heading and all that follows are untouched. Unlike replace_section, a leading heading in your fragment is NOT stripped.
  start and end are deliberately asymmetric: end writes into the LAST layout-cell, start into the FIRST — on a two-column page, the right cell and the left cell respectively.
  Failures, all of which mean "use action update instead": no_heading_on_page — the container (the first layout-cell on a layout-wrapped page, else the whole page) has no locatable heading, which is not the same as the page having none; section_boundary_unbalanced / preamble_boundary_unbalanced — a plain wrapper element opens inside the range and closes outside it, so replacing would orphan its closing tag.
  Dry-run returns a preview with the storage fragment, boundary info, and size delta; on success the response reports fragment bytes and base→merged body bytes, naming what it replaced.
- delete: Delete pages. Each item needs: page_id.
- comment: Add footer comments. Each item needs: page_id, body (Markdown).
- edit_comment: Edit comments. Each item needs: comment_id, body (Markdown), version_number.
- reply_comment: Reply to an existing footer or inline comment. Each item needs: parent_comment_id (the comment being replied to), comment_type ("footer" or "inline" — read this from the confluence_read output's "(type: ...)" label next to the comment ID; do not guess it), body (Markdown by default; storage if format="storage"). Does not accept page_id — Confluence's reply create models reject page_id and parent_comment_id together.
- add_label: Add labels. Each item needs: page_id, label.
- remove_label: Remove labels. Each item needs: page_id, label.

All actions support dry_run=true to preview without executing. Body fields accept Markdown by default (auto-converted to Confluence storage format).
Set format="storage" on a create, update, append, or reply_comment item to pass raw Confluence XHTML directly — use this when adding or modifying macros; comment, edit_comment, delete, add_label, and remove_label reject format.
When updating a page, version_number is required — get it from confluence_read first.
Prefer "append" over "update" whenever you are adding or replacing a section, not rewriting the whole page: it avoids re-sending the full body (typically ~100× smaller payload, faster end-to-end, lower token cost) and preserves macros exactly.`,
}

func NewServer(client ConfluenceClient, currentUser *confluence.User, logger *slog.Logger) *mcp.Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	inst := serverInstructions
	if currentUser != nil {
		inst += fmt.Sprintf("\n\nCurrent user: %s (accountId: %s)", currentUser.DisplayName, currentUser.AccountID)
	}

	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    "confluence-mcp",
			Version: "0.1.0",
		},
		&mcp.ServerOptions{
			Instructions: inst,
			Logger:       logger,
		},
	)

	h := &handlers{
		client: client,
		log:    logger.With("component", "handlers"),
	}

	mcp.AddTool(s, readTool, h.handleRead)
	mcp.AddTool(s, writeTool, h.handleWrite)

	s.AddReceivingMiddleware(toolCallLoggingMiddleware(logger))

	return s
}

// toolCallLoggingMiddleware returns middleware that logs every tools/call request.
func toolCallLoggingMiddleware(logger *slog.Logger) mcp.Middleware {
	log := logger.With("component", "middleware")
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}

			var toolName string
			var args json.RawMessage
			if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				toolName = p.Name
				args = p.Arguments
			}

			start := time.Now()
			log.InfoContext(ctx, "tool_call",
				"tool", toolName,
			)
			log.DebugContext(ctx, "tool_call_args",
				"tool", toolName,
				"args", string(args),
			)

			result, err := next(ctx, method, req)
			duration := time.Since(start)

			if err != nil {
				log.ErrorContext(ctx, "tool_error",
					"tool", toolName,
					"duration", duration,
					"error", err.Error(),
				)
				return result, err
			}

			attrs := []any{
				"tool", toolName,
				"duration", duration,
			}
			if ctr, ok := result.(*mcp.CallToolResult); ok {
				attrs = append(attrs, "is_error", ctr.IsError)
				totalLen := 0
				for _, c := range ctr.Content {
					if tc, ok := c.(*mcp.TextContent); ok {
						totalLen += len(tc.Text)
					}
				}
				attrs = append(attrs, "content_length", totalLen)
			}
			log.InfoContext(ctx, "tool_result", attrs...)

			return result, nil
		}
	}
}

const serverInstructions = `Confluence MCP Server — interact with Confluence Cloud via these tools:

- confluence_read: Get pages by ID/URL, search via CQL, list spaces/children/comments/inline comments/labels.
- confluence_write: Create, update, delete pages; add/edit/reply to comments; manage labels.
  Supports batch. Always has dry_run option.

Workflow tips:
1. Use confluence_read resource=spaces to discover available spaces before writing.
2. Use confluence_read with CQL for flexible searches.
3. All confluence_write actions support dry_run=true to preview changes.
4. Descriptions and comments accept Markdown — auto-converted to Confluence storage format.
5. When updating a page, version_number is required (get it from confluence_read first).
6. Use format="storage" to read/write raw Confluence XHTML when you need to add or modify macros directly — accepted on create, update, append, or reply_comment; comment, edit_comment, delete, add_label, and remove_label reject format.`
