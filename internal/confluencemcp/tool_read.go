package confluencemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
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
	// ResolutionStatus filters inline comment threads by resolution state.
	// Applies to resource: "inline_comments" only. No other field on this
	// struct carries a jsonschema tag today — added here because the
	// generated schema otherwise renders this property as a bare "array of
	// string" with no description and no enum, so an agent has no way to
	// discover the field or its valid values from the tool schema alone.
	ResolutionStatus []string `json:"resolution_status,omitempty" jsonschema:"Filters inline comment threads by resolution state (resource: \"inline_comments\" only). Valid values: open, resolved, dangling, reopened."`
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

	// Fetch comment, falling back footer -> inline on a 404 (D7).
	id, kind, body, err := h.resolveFocusedComment(ctx, info.commentID)
	if err != nil {
		return textResult(fmt.Sprintf("error fetching comment %s: %v", info.commentID, err), true), nil, nil
	}
	commentMD := h.commentRenderer(ctx, info.pageID)(body)
	header := commentIDLine(id, kind)

	// Check if page is cached.
	_, cached := h.cache.get(info.pageID)
	if cached {
		// Return only the comment.
		return textResult(fmt.Sprintf("%s\n\n%s", header, commentMD), false), nil, nil
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
	_, _ = fmt.Fprintf(&sb, "%s\n\n%s", header, commentMD)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString(pageContent)
	return textResult(sb.String(), false), nil, nil
}

// resolveFocusedComment resolves a focusedCommentId permalink to its ID,
// kind (footer/inline), and storage-format body. GetFooterComment is
// footer-only, but a permalink is the most likely way an agent arrives at an
// inline comment, so a footer 404 falls back to GetInlineComment (D7). The
// 404 is detected by unwrapping to *confluence.APIError rather than string
// matching — shouldRetry whitelists only 429/502/503, so a 404 always exits
// the client's retry loop on attempt 1 as a genuine *APIError{StatusCode:404}.
// Branches on the error, never on the returned pointer: both client methods
// return a non-nil comment alongside a non-nil error on failure.
//
// When the footer fetch 404s and the inline fetch also fails, the terse
// "not found" wording is used only if the inline failure is itself a 404.
// Any other inline failure (e.g. an expired token surfacing as 401, or a
// 500) is wrapped and surfaced verbatim — reporting "not found" for those
// would send the caller down the wrong recovery path.
func (h *handlers) resolveFocusedComment(ctx context.Context, commentID string) (id string, kind commentKind, body string, err error) {
	footerComment, footerErr := h.client.GetFooterComment(ctx, commentID)
	if footerErr == nil {
		return footerComment.ID, commentFooter, footerComment.Body.Storage.Value, nil
	}

	var apiErr *confluence.APIError
	if !errors.As(footerErr, &apiErr) || apiErr.StatusCode != 404 {
		return "", "", "", footerErr
	}

	inlineComment, inlineErr := h.client.GetInlineComment(ctx, commentID)
	if inlineErr != nil {
		var inlineAPIErr *confluence.APIError
		if errors.As(inlineErr, &inlineAPIErr) && inlineAPIErr.StatusCode == 404 {
			return "", "", "", errors.New("not found as footer or inline comment")
		}
		return "", "", "", fmt.Errorf("comment not found as a footer comment; inline lookup failed: %w", inlineErr)
	}
	return inlineComment.ID, commentInline, inlineComment.Body.Storage.Value, nil
}

// commentIDLine renders the "**Comment ID:** ...  (type: ...)" line shared by
// every comment-rendering path (threaded lists and permalink resolution).
// The type label is load-bearing — it is the comment_type value an agent
// feeds back to reply_comment (D3) — so this is the only place the line is
// built, which makes omitting the type impossible to express.
func commentIDLine(id string, kind commentKind) string {
	return fmt.Sprintf("**Comment ID:** %s  (type: %s)", id, kind)
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
		if r := rejectResolutionStatusFor(args); r != nil {
			return r, nil, nil
		}
		return h.readSpaces(ctx, args)
	case "children":
		if args.PageID == "" {
			return textResult("page_id is required for resource=children", true), nil, nil
		}
		if r := rejectResolutionStatusFor(args); r != nil {
			return r, nil, nil
		}
		return h.readChildren(ctx, args)
	case "comments":
		if args.PageID == "" {
			return textResult("page_id is required for resource=comments", true), nil, nil
		}
		if r := rejectResolutionStatusFor(args); r != nil {
			return r, nil, nil
		}
		return h.readComments(ctx, args)
	case "inline_comments":
		if args.PageID == "" {
			return textResult("page_id is required for resource=inline_comments", true), nil, nil
		}
		return h.readInlineComments(ctx, args)
	case "labels":
		if args.PageID == "" {
			return textResult("page_id is required for resource=labels", true), nil, nil
		}
		if r := rejectResolutionStatusFor(args); r != nil {
			return r, nil, nil
		}
		return h.readLabels(ctx, args)
	default:
		return textResult(fmt.Sprintf("unknown resource %q — use: spaces, children, comments, inline_comments, labels", args.Resource), true), nil, nil
	}
}

// rejectResolutionStatusFor returns an error result when resolution_status is
// set on a resource other than inline_comments — the only resource whose
// underlying API accepts the filter. Silently dropping the field would leave
// the caller believing an unsupported filter was applied.
func rejectResolutionStatusFor(args ReadArgs) *mcp.CallToolResult {
	if len(args.ResolutionStatus) == 0 {
		return nil
	}
	return textResult(fmt.Sprintf("resolution_status is only valid with resource=inline_comments, not resource=%q", args.Resource), true)
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

// maxChildFetchThreads caps how many top-level comment threads get their
// replies fetched in a single read. Confluence's children endpoints have no
// batch form, so expanding replies costs one extra HTTP call per thread —
// uncapped, a default limit=100 read could issue 100 sequential calls.
const maxChildFetchThreads = 25

// maxRepliesPerThread caps how many replies are fetched for a single comment
// thread. A thread with more replies than this gets a truncation notice
// rather than a silently incomplete list.
const maxRepliesPerThread = 100

// resolutionStatusValues lists the resolution-status values the Confluence
// v2 API accepts for inline comment filtering. This is the complete enum —
// do not invent others.
var resolutionStatusValues = []string{
	confluence.ResolutionOpen,
	confluence.ResolutionResolved,
	confluence.ResolutionDangling,
	confluence.ResolutionReopened,
}

// validateResolutionStatus checks each value against resolutionStatusValues,
// returning an error naming the valid options on the first unknown value.
func validateResolutionStatus(values []string) error {
	for _, v := range values {
		if !slices.Contains(resolutionStatusValues, v) {
			return fmt.Errorf("invalid resolution_status %q — valid values: %s", v, strings.Join(resolutionStatusValues, ", "))
		}
	}
	return nil
}

// commentThreadReply is one reply rendered beneath a parent comment thread.
type commentThreadReply struct {
	id   string
	body string
}

// renderThread renders one comment thread as a "---"-separated block:
// extraHeaderLines (resource-specific lines such as **On:**/**Status:**,
// rendered ahead of the comment ID line), the "**Comment ID:** ...
// (type: ...)" line built from id/kind, the body, then each reply
// under its own "**Reply ID:**" line, indented two spaces, and finally an
// optional per-thread notice (child-fetch failure or reply-cursor
// truncation). Shared by readComments and readInlineComments so their
// output shape cannot drift apart. Taking id/kind as their own
// parameters — rather than folding the "**Comment ID:**" line into
// extraHeaderLines — makes omitting the type label impossible to express.
func renderThread(extraHeaderLines []string, id string, kind commentKind, body string, replies []commentThreadReply, notice string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	for _, line := range extraHeaderLines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString(commentIDLine(id, kind))
	sb.WriteString("\n")
	sb.WriteString("\n")
	sb.WriteString(body)
	sb.WriteString("\n")
	for _, r := range replies {
		sb.WriteString("\n  **Reply ID:** ")
		sb.WriteString(r.id)
		sb.WriteString("\n")
		for _, line := range strings.Split(strings.TrimRight(r.body, "\n"), "\n") {
			sb.WriteString("  ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	if notice != "" {
		sb.WriteString("\n")
		sb.WriteString(notice)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// threadRenderer holds the resource-specific pieces renderAll needs to turn
// a list of top-level comments (confluence.Comment or confluence.InlineComment)
// into "---"-separated thread blocks, single-sourcing the cap/truncation
// policy shared by readComments and readInlineComments: child-reply fetches
// stop after maxChildFetchThreads threads (the parent comment still renders;
// only its replies are left uncollected), and truncationNotice — the
// resource's already-formatted notice text — is appended verbatim when that
// happens. idOf/bodyOf/extraHeaderLinesOf adapt the resource-specific
// comment type for the renderer; fetchChildren is the resource's
// reply-listing method.
type threadRenderer[T any] struct {
	kind               commentKind
	idOf               func(T) string
	bodyOf             func(T) string
	extraHeaderLinesOf func(T) []string
	fetchChildren      func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]T, string, error)
	render             func(string) string
	truncationNotice   string
}

// collectReplies fetches and renders the replies for one parent comment
// thread, single-sourcing the policies that must not drift between the
// footer and inline comment resources:
//   - a child-fetch error never aborts the read (a transient failure on one
//     thread must not discard every other thread already rendered) — instead
//     it is surfaced as a per-thread notice with no replies;
//   - a non-empty next cursor (more than 100 replies) is never silently
//     dropped — it is surfaced as its own per-thread notice.
func (tr threadRenderer[T]) collectReplies(ctx context.Context, commentID string) (replies []commentThreadReply, notice string) {
	children, cursor, err := tr.fetchChildren(ctx, commentID, &confluence.ListOptions{Limit: maxRepliesPerThread})
	if err != nil {
		return nil, fmt.Sprintf("*Replies could not be loaded: %v*", err)
	}
	for _, child := range children {
		replies = append(replies, commentThreadReply{id: tr.idOf(child), body: tr.render(tr.bodyOf(child))})
	}
	if cursor != "" {
		notice = fmt.Sprintf("*Not all replies are shown — this thread has more than %d replies.*", maxRepliesPerThread)
	}
	return replies, notice
}

// renderAll renders every top-level comment thread in comments as
// "---"-separated blocks. This is the single call site for collectReplies,
// so its policy cannot drift between resources.
func (tr threadRenderer[T]) renderAll(ctx context.Context, comments []T) string {
	var sb strings.Builder
	truncated := false
	for i, c := range comments {
		id := tr.idOf(c)
		body := tr.render(tr.bodyOf(c))

		var replies []commentThreadReply
		var notice string
		if i < maxChildFetchThreads {
			replies, notice = tr.collectReplies(ctx, id)
		} else {
			truncated = true
			notice = fmt.Sprintf("*Replies not expanded — this thread is past the per-read cap of %d.*", maxChildFetchThreads)
		}

		sb.WriteString(renderThread(tr.extraHeaderLinesOf(c), id, tr.kind, body, replies, notice))
	}

	if truncated {
		sb.WriteString(tr.truncationNotice)
	}

	return sb.String()
}

func (h *handlers) readComments(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	opts := &confluence.ListOptions{Limit: args.Limit, Cursor: args.NextPageToken}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	comments, nextToken, err := h.client.GetPageFooterComments(ctx, args.PageID, opts)
	if err != nil {
		return textResult(fmt.Sprintf("error listing comments: %v", err), true), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Comments on page %s:**\n\n", args.PageID)
	tr := threadRenderer[confluence.Comment]{
		kind:               commentFooter,
		idOf:               func(c confluence.Comment) string { return c.ID },
		bodyOf:             func(c confluence.Comment) string { return c.Body.Storage.Value },
		extraHeaderLinesOf: func(c confluence.Comment) []string { return nil },
		fetchChildren:      h.client.GetFooterCommentChildren,
		render:             h.commentRenderer(ctx, args.PageID),
		truncationNotice: fmt.Sprintf(
			"\n*Replies not expanded beyond the first %d threads — re-read with a smaller `limit` to see the rest.*\n",
			maxChildFetchThreads,
		),
	}
	sb.WriteString(tr.renderAll(ctx, comments))

	if nextToken != "" {
		fmt.Fprintf(&sb, "\n*More comments available — pass `next_page_token: %q` with `resource: \"comments\"` and `page_id: %q` to continue.*", nextToken, args.PageID)
	}

	return textResult(sb.String(), false), nil, nil
}

func (h *handlers) readInlineComments(ctx context.Context, args ReadArgs) (*mcp.CallToolResult, any, error) {
	if err := validateResolutionStatus(args.ResolutionStatus); err != nil {
		return textResult(err.Error(), true), nil, nil
	}

	opts := &confluence.ListOptions{Limit: args.Limit, Cursor: args.NextPageToken, ResolutionStatus: args.ResolutionStatus}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	comments, nextToken, err := h.client.GetPageInlineComments(ctx, args.PageID, opts)
	if err != nil {
		return textResult(fmt.Sprintf("error listing inline comments: %v", err), true), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Inline comments on page %s:**\n\n", args.PageID)
	tr := threadRenderer[confluence.InlineComment]{
		kind:   commentInline,
		idOf:   func(c confluence.InlineComment) string { return c.ID },
		bodyOf: func(c confluence.InlineComment) string { return c.Body.Storage.Value },
		extraHeaderLinesOf: func(c confluence.InlineComment) []string {
			var lines []string
			if c.Properties.InlineOriginalSelection != "" {
				lines = append(lines, fmt.Sprintf("**On:** %q", c.Properties.InlineOriginalSelection))
			}
			if c.ResolutionStatus != "" {
				lines = append(lines, fmt.Sprintf("**Status:** %s", c.ResolutionStatus))
			}
			return lines
		},
		fetchChildren: h.client.GetInlineCommentChildren,
		render:        h.commentRenderer(ctx, args.PageID),
		truncationNotice: fmt.Sprintf(
			"\n*Replies not expanded beyond the first %d threads — re-read with a narrower `resolution_status` or smaller `limit` to see the rest.*\n",
			maxChildFetchThreads,
		),
	}
	sb.WriteString(tr.renderAll(ctx, comments))

	if nextToken != "" {
		fmt.Fprintf(&sb, "\n*More inline comments available — pass `next_page_token: %q` with `resource: \"inline_comments\"` and `page_id: %q`", nextToken, args.PageID)
		if len(args.ResolutionStatus) > 0 {
			// json.Marshal of a []string cannot fail.
			statusJSON, _ := json.Marshal(args.ResolutionStatus)
			fmt.Fprintf(&sb, " and `resolution_status: %s`", statusJSON)
		}
		sb.WriteString(" to continue.*")
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
