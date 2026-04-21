package confluencemcp

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

type confluenceURLInfo struct {
	pageID    string
	spaceKey  string
	commentID string
}

var pageIDRegex = regexp.MustCompile(`/pages/(\d+)`)
var spaceKeyRegex = regexp.MustCompile(`/spaces/([^/]+)`)

func parseConfluenceURL(rawURL string) (*confluenceURLInfo, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	match := pageIDRegex.FindStringSubmatch(u.Path)
	if match == nil {
		return nil, fmt.Errorf("no page ID found in URL path: %s", u.Path)
	}

	info := &confluenceURLInfo{pageID: match[1]}

	if sm := spaceKeyRegex.FindStringSubmatch(u.Path); sm != nil {
		info.spaceKey = sm[1]
	}

	if cid := u.Query().Get("focusedCommentId"); cid != "" {
		info.commentID = cid
	}

	return info, nil
}

// parseSections scans Markdown for ATX headings and returns their positions.
func parseSections(md string) []section {
	var sections []section
	lines := strings.Split(md, "\n")
	pos := 0 // byte position

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, ch := range trimmed {
				if ch == '#' {
					level++
				} else {
					break
				}
			}
			if level > 0 && level <= 6 {
				heading := strings.TrimSpace(trimmed[level:])
				if heading != "" {
					sections = append(sections, section{
						Heading: heading,
						Level:   level,
						Start:   pos,
					})
				}
			}
		}
		pos += len(line) + 1 // +1 for newline
	}

	// Set End offsets: each section ends where the next one starts (or EOF)
	for i := range sections {
		if i+1 < len(sections) {
			sections[i].End = sections[i+1].Start
		} else {
			sections[i].End = len(md)
		}
	}

	return sections
}

// extractSection returns the content of a named section.
func extractSection(md string, sections []section, heading string) string {
	heading = strings.ToLower(heading)
	for _, s := range sections {
		if strings.ToLower(s.Heading) == heading {
			return strings.TrimSpace(md[s.Start:s.End])
		}
	}
	return ""
}

// buildTOC generates an indented table of contents.
func buildTOC(sections []section) string {
	var sb strings.Builder
	for _, s := range sections {
		indent := strings.Repeat("  ", s.Level-1)
		sb.WriteString(indent)
		sb.WriteString("- ")
		sb.WriteString(s.Heading)
		sb.WriteString("\n")
	}
	return sb.String()
}

// textResult creates a text-only tool result.
func textResult(msg string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: isError,
	}
}

// ReadArgs holds the arguments for the confluence_read tool.
type ReadArgs struct {
	PageIDs       []string `json:"page_ids,omitempty"`
	URL           string   `json:"url,omitempty"`
	CQL           string   `json:"cql,omitempty"`
	Resource      string   `json:"resource,omitempty"`
	PageID        string   `json:"page_id,omitempty"`
	Section       string   `json:"section,omitempty"`
	Format        string   `json:"format,omitempty"` // "markdown" (default) or "storage"
	Limit         int      `json:"limit,omitempty"`
	NextPageToken string   `json:"next_page_token,omitempty"`
}

// handleRead dispatches to the appropriate read sub-method based on the args.
func (h *handlers) handleRead(ctx context.Context, _ *mcp.CallToolRequest, args ReadArgs) (*mcp.CallToolResult, any, error) {
	// Section extraction from cache takes priority.
	if args.Section != "" && args.PageID != "" {
		return h.readSectionFromCache(args)
	}

	// Chunk continuation: token alone, no other primary mode.
	if args.NextPageToken != "" && len(args.PageIDs) == 0 && args.URL == "" &&
		args.CQL == "" && args.Resource == "" {
		return h.readNextChunk(ctx, args.NextPageToken)
	}

	// Count active modes.
	modes := 0
	if len(args.PageIDs) > 0 {
		modes++
	}
	if args.URL != "" {
		modes++
	}
	if args.CQL != "" {
		modes++
	}
	if args.Resource != "" {
		modes++
	}

	if modes == 0 {
		return textResult("provide exactly one of: page_ids, url, cql, resource", true), nil, nil
	}
	if modes > 1 {
		return textResult("page_ids, url, cql, and resource are mutually exclusive — provide exactly one", true), nil, nil
	}

	switch {
	case len(args.PageIDs) > 0:
		return h.readByIDs(ctx, args)
	case args.URL != "":
		return h.readByURL(ctx, args)
	case args.CQL != "":
		return h.readByCQL(ctx, args)
	default:
		return h.readResource(ctx, args)
	}
}

// readSectionFromCache extracts a named section from a cached page.
func (h *handlers) readSectionFromCache(args ReadArgs) (*mcp.CallToolResult, any, error) {
	cached, ok := h.cache.get(args.PageID)
	if !ok {
		return textResult(fmt.Sprintf("page %s not in cache — fetch it first with page_ids or url", args.PageID), true), nil, nil
	}
	content := extractSection(cached.markdown, cached.sections, args.Section)
	if content == "" {
		return textResult(fmt.Sprintf("section %q not found in page %s", args.Section, args.PageID), true), nil, nil
	}
	return textResult(content, false), nil, nil
}

// readByIDs fetches one or more pages by ID.
func (h *handlers) readByIDs(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	var sb strings.Builder
	for _, id := range args.PageIDs {
		h.logger().DebugContext(ctx, "fetch_page", "page_id", id)
		page, err := h.client.GetPage(ctx, id)
		if err != nil {
			return textResult(fmt.Sprintf("error fetching page %s: %v", id, err), true), nil, nil
		}
		var result string
		if args.Format == "storage" {
			result = h.processPageRaw(page)
		} else {
			result = h.processPage(ctx, page)
		}
		sb.WriteString(result)
		if len(args.PageIDs) > 1 {
			sb.WriteString("\n\n---\n\n")
		}
	}
	return textResult(sb.String(), false), nil, nil
}

// processPage converts a page to Markdown, caches it, and applies adaptive chunking.
func (h *handlers) processPage(ctx context.Context, page *confluence.Page) string {
	resolver := newPageResolver(ctx, h.client, h.client.BaseURL(), page.ID)
	markdown, registry, convLog := mdconv.ToMarkdownWithMacrosResolved(page.Body.Storage.Value, resolver)
	sections := parseSections(markdown)

	h.cache.put(&cachedPage{
		pageID:    page.ID,
		markdown:  markdown,
		sections:  sections,
		macros:    registry,
		fetchedAt: time.Now(),
	})

	if convLog != nil {
		h.logger().DebugContext(ctx, "convert_page",
			"page_id", page.ID,
			"input_bytes", convLog.InputBytes,
			"output_bytes", convLog.OutputBytes,
			"elements", convLog.Elements,
			"macros", convLog.Macros,
			"skipped", convLog.Skipped,
			"errors", convLog.Errors,
		)
	}

	header := fmt.Sprintf("**Page ID:** %s | **Title:** %s | **Version:** %d\n\n",
		page.ID, page.Title, page.Version.Number)

	if len(markdown) < maxPageSize {
		return header + markdown
	}

	return header + renderChunk(markdown, sections, page.ID, nil)
}

// renderChunk wraps chunkPage with the TOC + continuation hint suffix used
// when a page exceeds maxPageSize.
func renderChunk(markdown string, sections []section, pageID string, cursor *chunkCursor) string {
	chunk, nextToken := chunkPage(markdown, sections, pageID, cursor)

	var sb strings.Builder
	sb.WriteString(chunk)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString("**Table of Contents:**\n\n")
	sb.WriteString(buildTOC(sections))
	if nextToken != "" {
		sb.WriteString("\nPage truncated. Continue with `next_page_token` to read the next section, ")
		sb.WriteString("or use `page_id` + `section` to jump to a specific heading.\n\n")
		fmt.Fprintf(&sb, "next_page_token: %q", nextToken)
	} else {
		sb.WriteString("\nEnd of page.")
	}
	return sb.String()
}

// readNextChunk resumes a chunked page read using a base64url-encoded cursor.
// The page is served from cache when possible; otherwise it is re-fetched and
// re-cached transparently.
func (h *handlers) readNextChunk(ctx context.Context, token string) (*mcp.CallToolResult, any, error) {
	cursor, err := decodeChunkToken(token)
	if err != nil {
		return textResult(fmt.Sprintf("invalid next_page_token: %v", err), true), nil, nil
	}
	if cursor.PageID == "" {
		return textResult("next_page_token is missing page_id", true), nil, nil
	}

	cached, ok := h.cache.get(cursor.PageID)
	if !ok {
		h.logger().DebugContext(ctx, "cache_miss", "page_id", cursor.PageID, "type", "chunk_cursor")
		page, err := h.client.GetPage(ctx, cursor.PageID)
		if err != nil {
			return textResult(fmt.Sprintf("error fetching page %s: %v", cursor.PageID, err), true), nil, nil
		}
		// Populate cache.
		_ = h.processPage(ctx, page)
		cached, ok = h.cache.get(cursor.PageID)
		if !ok {
			return textResult(fmt.Sprintf("unable to cache page %s for chunk continuation", cursor.PageID), true), nil, nil
		}
	}

	body := renderChunk(cached.markdown, cached.sections, cursor.PageID, &cursor)
	header := fmt.Sprintf("**Page ID:** %s (continuation)\n\n", cursor.PageID)
	return textResult(header+body, false), nil, nil
}

// processPageRaw returns the page's storage format (raw XHTML) without Markdown conversion.
// Note: does not populate the page cache — section extraction is unavailable after a storage read.
func (h *handlers) processPageRaw(page *confluence.Page) string {
	header := fmt.Sprintf("**Page ID:** %s | **Title:** %s | **Version:** %d\n\n",
		page.ID, page.Title, page.Version.Number)
	return header + page.Body.Storage.Value
}

// readByURL fetches a page by Confluence URL, handling focusedCommentId.
func (h *handlers) readByURL(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	h.logger().DebugContext(ctx, "resolve_url", "url", args.URL)
	info, err := parseConfluenceURL(args.URL)
	if err != nil {
		return textResult(fmt.Sprintf("invalid Confluence URL: %v", err), true), nil, nil
	}

	if info.commentID == "" {
		// No comment — delegate to readByIDs.
		return h.readByIDs(ctx, ReadArgs{PageIDs: []string{info.pageID}, Format: args.Format})
	}

	// Fetch comment.
	comment, err := h.client.GetComment(ctx, info.commentID)
	if err != nil {
		return textResult(fmt.Sprintf("error fetching comment %s: %v", info.commentID, err), true), nil, nil
	}
	commentMD := h.commentRenderer(ctx, info.pageID)(comment.Body.Storage.Value)

	// Check if page is cached.
	_, cached := h.cache.get(info.pageID)
	if cached {
		// Return only the comment.
		return textResult(fmt.Sprintf("**Comment ID:** %s\n\n%s", comment.ID, commentMD), false), nil, nil
	}

	// Fetch page too.
	page, err := h.client.GetPage(ctx, info.pageID)
	if err != nil {
		return textResult(fmt.Sprintf("error fetching page %s: %v", info.pageID, err), true), nil, nil
	}
	var pageContent string
	if args.Format == "storage" {
		pageContent = h.processPageRaw(page)
	} else {
		pageContent = h.processPage(ctx, page)
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "**Comment ID:** %s\n\n%s", comment.ID, commentMD)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString(pageContent)
	return textResult(sb.String(), false), nil, nil
}

// readByCQL searches content using a CQL query.
func (h *handlers) readByCQL(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	opts := &confluence.ListOptions{Limit: args.Limit, Cursor: args.NextPageToken}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	result, err := h.client.SearchContent(ctx, args.CQL, opts)
	if err != nil {
		return textResult(fmt.Sprintf("CQL search error: %v", err), true), nil, nil
	}

	if len(result.Results) == 0 {
		return textResult("no results found", false), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Found %d result(s):**\n\n", result.TotalSize)
	for _, item := range result.Results {
		fmt.Fprintf(&sb, "- **%s** (ID: %s, type: %s)\n", item.Title, item.Content.ID, item.Content.Type)
		if item.Excerpt != "" {
			fmt.Fprintf(&sb, "  %s\n", item.Excerpt)
		}
	}

	if result.Links.Next != "" {
		fmt.Fprintf(&sb, "\n*More results available — pass `next_page_token: %q` with the same CQL to continue.*", result.Links.Next)
	}

	return textResult(sb.String(), false), nil, nil
}

// readResource dispatches to the appropriate resource listing.
func (h *handlers) readResource(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	switch args.Resource {
	case "spaces":
		return h.readSpaces(ctx, args)
	case "children":
		if args.PageID == "" {
			return textResult("page_id is required for resource=children", true), nil, nil
		}
		return h.readChildren(ctx, args)
	case "comments":
		if args.PageID == "" {
			return textResult("page_id is required for resource=comments", true), nil, nil
		}
		return h.readComments(ctx, args)
	case "labels":
		if args.PageID == "" {
			return textResult("page_id is required for resource=labels", true), nil, nil
		}
		return h.readLabels(ctx, args)
	default:
		return textResult(fmt.Sprintf("unknown resource %q — use: spaces, children, comments, labels", args.Resource), true), nil, nil
	}
}

func (h *handlers) readSpaces(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	opts := &confluence.ListOptions{Limit: args.Limit, Cursor: args.NextPageToken}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	spaces, nextToken, err := h.client.GetSpaces(ctx, opts)
	if err != nil {
		return textResult(fmt.Sprintf("error listing spaces: %v", err), true), nil, nil
	}

	var sb strings.Builder
	sb.WriteString("**Spaces:**\n\n")
	for _, s := range spaces {
		fmt.Fprintf(&sb, "- **%s** (%s) — %s\n", s.Name, s.Key, s.Type)
	}

	if nextToken != "" {
		fmt.Fprintf(&sb, "\n*More spaces available — pass `next_page_token: %q` with `resource: \"spaces\"` to continue.*", nextToken)
	}

	return textResult(sb.String(), false), nil, nil
}

func (h *handlers) readChildren(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	opts := &confluence.ListOptions{Limit: args.Limit, Cursor: args.NextPageToken}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	pages, nextToken, err := h.client.GetPageChildren(ctx, args.PageID, opts)
	if err != nil {
		return textResult(fmt.Sprintf("error listing children: %v", err), true), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Child pages of %s:**\n\n", args.PageID)
	for _, p := range pages {
		fmt.Fprintf(&sb, "- **%s** (ID: %s)\n", p.Title, p.ID)
	}

	if nextToken != "" {
		fmt.Fprintf(&sb, "\n*More children available — pass `next_page_token: %q` with `resource: \"children\"` and `page_id: %q` to continue.*", nextToken, args.PageID)
	}

	return textResult(sb.String(), false), nil, nil
}

func (h *handlers) readComments(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	opts := &confluence.ListOptions{Limit: args.Limit, Cursor: args.NextPageToken}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	comments, nextToken, err := h.client.GetPageComments(ctx, args.PageID, opts)
	if err != nil {
		return textResult(fmt.Sprintf("error listing comments: %v", err), true), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Comments on page %s:**\n\n", args.PageID)
	render := h.commentRenderer(ctx, args.PageID)
	for _, c := range comments {
		body := render(c.Body.Storage.Value)
		fmt.Fprintf(&sb, "---\n**Comment ID:** %s\n\n%s\n\n", c.ID, body)
	}

	if nextToken != "" {
		fmt.Fprintf(&sb, "\n*More comments available — pass `next_page_token: %q` with `resource: \"comments\"` and `page_id: %q` to continue.*", nextToken, args.PageID)
	}

	return textResult(sb.String(), false), nil, nil
}

func (h *handlers) readLabels(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	opts := &confluence.ListOptions{Limit: args.Limit, Cursor: args.NextPageToken}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	labels, nextToken, err := h.client.GetPageLabels(ctx, args.PageID, opts)
	if err != nil {
		return textResult(fmt.Sprintf("error listing labels: %v", err), true), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Labels on page %s:**\n\n", args.PageID)
	for _, l := range labels {
		fmt.Fprintf(&sb, "- %s\n", l.Name)
	}

	if nextToken != "" {
		fmt.Fprintf(&sb, "\n*More labels available — pass `next_page_token: %q` with `resource: \"labels\"` and `page_id: %q` to continue.*", nextToken, args.PageID)
	}

	return textResult(sb.String(), false), nil, nil
}
