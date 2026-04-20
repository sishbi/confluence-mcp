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
4. resource — List a resource: "spaces", "children" (needs page_id), "comments" (needs page_id), "labels" (needs page_id).

Options: format ("markdown" default, or "storage" for raw XHTML), section (extract a heading section from a fetched page), limit (default 100), next_page_token.
Long pages are automatically chunked — if truncated, a table of contents is shown with section names you can request individually.
Use format=storage when you need to add or modify Confluence macros directly.`,
}

var writeTool = &mcp.Tool{
	Name: "confluence_write",
	Description: `Modify Confluence data. Batch-first: pass an array of items even for single operations.

Actions:
- create: Create pages. Each item needs: space_id, title. Optional: body (Markdown), parent_id, status (current/draft).
- update: Update pages. Each item needs: page_id, title, version_number. Optional: body (Markdown), status. Replaces the full body.
- append: Insert or replace a fragment in an existing page WITHOUT sending the full body. The server fetches the current storage, splices the fragment in place, and writes the merged result — the agent only sends the small fragment. For typical edits this is ~100× smaller than update and measurably faster. Each item needs: page_id, body (Markdown by default; storage if format="storage"). Optional: position (one of "end" (default), "after_heading", "replace_section"), heading (required for after_heading / replace_section; exact, case-sensitive match), version_number (optional optimistic concurrency). Dry-run returns a structured preview including the storage fragment, boundary info, and size delta. On success the response reports fragment bytes and base→merged body bytes.
- delete: Delete pages. Each item needs: page_id.
- comment: Add footer comments. Each item needs: page_id, body (Markdown).
- edit_comment: Edit comments. Each item needs: comment_id, body (Markdown), version_number.
- add_label: Add labels. Each item needs: page_id, label.
- remove_label: Remove labels. Each item needs: page_id, label.

All actions support dry_run=true to preview without executing. Body fields accept Markdown by default (auto-converted to Confluence storage format).
Set format="storage" on an item to pass raw Confluence XHTML directly — use this when adding or modifying macros.
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

			// Extract tool name from the request params.
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

			// Log result summary.
			attrs := []any{
				"tool", toolName,
				"duration", duration,
			}
			if ctr, ok := result.(*mcp.CallToolResult); ok {
				attrs = append(attrs, "is_error", ctr.IsError)
				// Sum content text length for size indication.
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

- confluence_read: Get pages by ID/URL, search via CQL, list spaces/children/comments/labels.
- confluence_write: Create, update, delete pages; add/edit comments; manage labels.
  Supports batch. Always has dry_run option.

Workflow tips:
1. Use confluence_read resource=spaces to discover available spaces before writing.
2. Use confluence_read with CQL for flexible searches.
3. All confluence_write actions support dry_run=true to preview changes.
4. Descriptions and comments accept Markdown — auto-converted to Confluence storage format.
5. When updating a page, version_number is required (get it from confluence_read first).
6. Use format="storage" to read/write raw Confluence XHTML when you need to add or modify macros directly.`
