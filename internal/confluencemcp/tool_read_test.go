package confluencemcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

func TestParseConfluenceURL_PageOnly(t *testing.T) {
	info, err := parseConfluenceURL("https://company.atlassian.net/wiki/spaces/DEV/pages/4125687814/Page+Title")
	assert.NoError(t, err)
	assert.Equal(t, "4125687814", info.pageID)
	assert.Equal(t, "DEV", info.spaceKey)
	assert.Equal(t, "", info.commentID)
}

func TestParseConfluenceURL_WithCommentID(t *testing.T) {
	info, err := parseConfluenceURL("https://company.atlassian.net/wiki/spaces/DEV/pages/4125687814/Title?focusedCommentId=42506")
	assert.NoError(t, err)
	assert.Equal(t, "4125687814", info.pageID)
	assert.Equal(t, "42506", info.commentID)
}

func TestParseConfluenceURL_InvalidURL(t *testing.T) {
	_, err := parseConfluenceURL("not-a-url")
	assert.Error(t, err)
}

func TestParseConfluenceURL_NoPageID(t *testing.T) {
	_, err := parseConfluenceURL("https://company.atlassian.net/wiki/spaces/DEV")
	assert.Error(t, err)
}

func TestParseSections(t *testing.T) {
	md := "# Introduction\n\nSome intro text.\n\n## Details\n\nDetail content here.\n\n## Conclusion\n\nFinal thoughts."
	sections := parseSections(md)

	assert.Len(t, sections, 3)
	assert.Equal(t, "Introduction", sections[0].Heading)
	assert.Equal(t, 1, sections[0].Level)
	assert.Equal(t, "Details", sections[1].Heading)
	assert.Equal(t, 2, sections[1].Level)
	assert.Equal(t, "Conclusion", sections[2].Heading)
	assert.Equal(t, 2, sections[2].Level)
}

func TestParseSections_NoHeadings(t *testing.T) {
	md := "Just plain text with no headings."
	sections := parseSections(md)
	assert.Len(t, sections, 0)
}

func TestExtractSection(t *testing.T) {
	md := "# Introduction\n\nSome intro text.\n\n## Details\n\nDetail content here.\n\n## Conclusion\n\nFinal thoughts."
	sections := parseSections(md)

	result := extractSection(md, sections, "Details")
	assert.Contains(t, result, "Detail content here.")
	assert.NotContains(t, result, "Final thoughts.")
	assert.NotContains(t, result, "Some intro text.")
}

func TestExtractSection_NotFound(t *testing.T) {
	md := "# Introduction\n\nSome text."
	sections := parseSections(md)

	result := extractSection(md, sections, "Nonexistent")
	assert.Equal(t, "", result)
}

func TestBuildTOC(t *testing.T) {
	sections := []section{
		{Heading: "Introduction", Level: 1},
		{Heading: "Details", Level: 2},
		{Heading: "Sub-detail", Level: 3},
		{Heading: "Conclusion", Level: 2},
	}
	toc := buildTOC(sections)
	assert.Contains(t, toc, "- Introduction")
	assert.Contains(t, toc, "  - Details")
	assert.Contains(t, toc, "    - Sub-detail")
	assert.Contains(t, toc, "  - Conclusion")
}

// helper to extract text from the first content item
func firstText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content item is not *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestHandleRead_PageByID(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:    "123",
					Title: "Test Page",
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: "<p>Hello world</p>",
					}},
					Version: confluence.PageVersion{Number: 5},
				}, nil
			},
		},
	}

	args := ReadArgs{PageIDs: []string{"123"}}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "Hello world")
	assert.Contains(t, text, "123") // page ID in output
}

func TestHandleRead_PageByURL(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				assert.Equal(t, "4125687814", id)
				return &confluence.Page{
					ID:    "4125687814",
					Title: "Test Page",
					Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Content</p>"}},
				}, nil
			},
		},
	}

	args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/4125687814/Title"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestHandleRead_URLWithCommentID_PageNotCached(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:    "123",
					Title: "Test Page",
					Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Page content</p>"}},
				}, nil
			},
			GetCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
				return &confluence.Comment{
					ID:   "456",
					Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Comment text</p>"}},
				}, nil
			},
		},
	}

	args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	text := firstText(t, result)
	// Both page and comment should be present
	assert.Contains(t, text, "Comment text")
	assert.Contains(t, text, "Page content")
}

func TestHandleRead_URLWithCommentID_PageCached(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			// GetPageFn intentionally NOT set — should not be called
			GetCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
				return &confluence.Comment{
					ID:   "456",
					Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Comment text</p>"}},
				}, nil
			},
		},
	}
	// Pre-populate cache
	h.cache.put(&cachedPage{pageID: "123", markdown: "# Cached page", fetchedAt: time.Now()})

	args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	text := firstText(t, result)
	// Only comment should be present (page was cached, not re-fetched)
	assert.Contains(t, text, "Comment text")
	assert.NotContains(t, text, "Cached page")
}

func TestHandleRead_SearchCQL(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			SearchContentFn: func(ctx context.Context, cql string, opts *confluence.ListOptions) (*confluence.SearchResult, error) {
				assert.Equal(t, "type=page AND space=DEV", cql)
				return &confluence.SearchResult{
					Results:   []confluence.SearchResultItem{{Title: "Found Page", Content: confluence.SearchContent{ID: "1"}}},
					TotalSize: 1,
				}, nil
			},
		},
	}

	args := ReadArgs{CQL: "type=page AND space=DEV"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	text := firstText(t, result)
	assert.Contains(t, text, "Found Page")
}

func TestHandleRead_ListSpaces(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetSpacesFn: func(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error) {
				return []confluence.Space{{ID: "1", Key: "DEV", Name: "Development"}}, "", nil
			},
		},
	}

	args := ReadArgs{Resource: "spaces"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	text := firstText(t, result)
	assert.Contains(t, text, "DEV")
	assert.Contains(t, text, "Development")
}

func TestHandleRead_MutualExclusion(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{PageIDs: []string{"1"}, CQL: "type=page"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleRead_NoArgs(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleRead_LongPageChunked(t *testing.T) {
	// Create a page body that exceeds maxPageSize when converted to Markdown
	longContent := "<h1>Introduction</h1><p>" + strings.Repeat("word ", 5000) + "</p>"
	longContent += "<h2>Middle Section</h2><p>" + strings.Repeat("more ", 5000) + "</p>"
	longContent += "<h2>Final Section</h2><p>end</p>"

	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:    "123",
					Title: "Long Page",
					Body:  confluence.PageBody{Storage: confluence.StorageBody{Value: longContent}},
				}, nil
			},
		},
	}

	args := ReadArgs{PageIDs: []string{"123"}}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	text := firstText(t, result)
	// Should contain TOC with all headings
	assert.Contains(t, text, "Introduction")
	assert.Contains(t, text, "Middle Section")
	assert.Contains(t, text, "Final Section")
	// Should contain hint about section parameter
	assert.Contains(t, text, "section")
}

func TestHandleRead_SectionFromCache(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	// Pre-populate cache with a page that has sections
	md := "# Introduction\n\nIntro text.\n\n## Details\n\nDetail content.\n\n## Conclusion\n\nFinal."
	h.cache.put(&cachedPage{
		pageID:    "123",
		markdown:  md,
		sections:  parseSections(md),
		fetchedAt: time.Now(),
	})

	args := ReadArgs{PageID: "123", Section: "Details"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	text := firstText(t, result)
	assert.Contains(t, text, "Detail content.")
	assert.NotContains(t, text, "Final.")
}

func TestHandleRead_ListChildren(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageChildrenFn: func(ctx context.Context, id string, opts *confluence.ListOptions) ([]confluence.Page, string, error) {
				assert.Equal(t, "123", id)
				return []confluence.Page{
					{ID: "456", Title: "Child One"},
					{ID: "789", Title: "Child Two"},
				}, "", nil
			},
		},
	}

	args := ReadArgs{Resource: "children", PageID: "123"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "Child One")
	assert.Contains(t, text, "Child Two")
	assert.Contains(t, text, "456")
}

func TestHandleRead_ListChildren_MissingPageID(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{Resource: "children"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "page_id")
}

func TestHandleRead_ListComments(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				assert.Equal(t, "123", pageID)
				return []confluence.Comment{
					{ID: "c1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>First comment</p>"}}},
					{ID: "c2", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Second comment</p>"}}},
				}, "", nil
			},
		},
	}

	args := ReadArgs{Resource: "comments", PageID: "123"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "First comment")
	assert.Contains(t, text, "Second comment")
	assert.Contains(t, text, "c1")
}

func TestHandleRead_ListComments_MissingPageID(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{Resource: "comments"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "page_id")
}

func TestHandleRead_ListLabels(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageLabelsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Label, string, error) {
				assert.Equal(t, "123", pageID)
				return []confluence.Label{
					{ID: "l1", Name: "important"},
					{ID: "l2", Name: "reviewed"},
				}, "", nil
			},
		},
	}

	args := ReadArgs{Resource: "labels", PageID: "123"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "important")
	assert.Contains(t, text, "reviewed")
}

func TestHandleRead_ListLabels_MissingPageID(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{Resource: "labels"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "page_id")
}

func TestHandleRead_UnknownResource(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{Resource: "invalid"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "unknown resource")
}

func TestHandleRead_CacheEvictionOnExpiry(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:    "123",
					Title: "Fresh Page",
					Body:  confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Fresh</p>"}},
				}, nil
			},
		},
	}
	// Put an expired entry
	h.cache.put(&cachedPage{
		pageID:    "123",
		markdown:  "# Stale",
		fetchedAt: time.Now().Add(-2 * cacheTTL),
	})

	args := ReadArgs{PageIDs: []string{"123"}}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	text := firstText(t, result)
	assert.Contains(t, text, "Fresh") // fetched fresh, not stale cache
}

func TestProcessPage_CachesRegistry(t *testing.T) {
	h := &handlers{
		client: &mockClient{},
	}

	page := &confluence.Page{
		ID:    "p1",
		Title: "Test",
		Body: confluence.PageBody{Storage: confluence.StorageBody{
			Value: `<p>Text.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Note.</p></ac:rich-text-body></ac:structured-macro>`,
		}},
		Version: confluence.PageVersion{Number: 1},
	}

	_ = h.processPage(context.Background(), page)

	cached, ok := h.cache.get("p1")
	require.True(t, ok)
	require.NotNil(t, cached.macros)
	assert.Len(t, cached.macros.Entries, 1)
	assert.Equal(t, "info", cached.macros.Entries[0].Name)
}

func TestProcessPage_StorageFormat(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	storageBody := `<h1>Title</h1><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Note.</p></ac:rich-text-body></ac:structured-macro>`
	page := &confluence.Page{
		ID:    "p1",
		Title: "Test",
		Body: confluence.PageBody{Storage: confluence.StorageBody{
			Value: storageBody,
		}},
		Version: confluence.PageVersion{Number: 3},
	}

	result := h.processPageRaw(page)

	// Should contain raw XHTML, not Markdown
	assert.Contains(t, result, "ac:structured-macro")
	assert.Contains(t, result, `ac:name="info"`)
	assert.Contains(t, result, "**Page ID:** p1")
	assert.NotContains(t, result, "<!-- macro:")
}

func TestReadByIDs_StorageFormat(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID: id, Title: "Test",
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: `<ac:structured-macro ac:name="toc"></ac:structured-macro><p>Content</p>`,
					}},
					Version: confluence.PageVersion{Number: 1},
				}, nil
			},
		},
	}

	result, _, _ := h.readByIDs(context.Background(), ReadArgs{
		PageIDs: []string{"p1"},
		Format:  "storage",
	})

	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "**Page ID:** p1")
	assert.Contains(t, text, "ac:structured-macro")
	assert.NotContains(t, text, "<!-- macro:")
}

// extractTokenTrailer returns the base64url token emitted in the chunked
// response trailer (line starting with `next_page_token:`). The helper keeps
// the continuation tests readable by hiding the stringy parsing.
func extractTokenTrailer(t *testing.T, text string) string {
	t.Helper()
	marker := "next_page_token: "
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	tail := text[idx+len(marker):]
	// Token is wrapped in Go-style double quotes by %q.
	if !strings.HasPrefix(tail, `"`) {
		t.Fatalf("expected quoted token, got %q", tail[:min(40, len(tail))])
	}
	end := strings.Index(tail[1:], `"`)
	if end < 0 {
		t.Fatalf("unterminated token in response: %q", tail[:min(60, len(tail))])
	}
	return tail[1 : 1+end]
}

func TestChunkingEmitsNextPageToken(t *testing.T) {
	// Five H2 sections each ~5KB so total > 20KB and the first chunk
	// (prologue + first H2) is well under maxPageSize.
	var body strings.Builder
	body.WriteString("<h1>Intro</h1><p>Welcome.</p>")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&body, "<h2>Section %d</h2>", i)
		body.WriteString("<p>")
		body.WriteString(strings.Repeat("section body ", 400))
		body.WriteString("</p>")
	}

	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:      "p-chunked",
					Title:   "Five-section fixture",
					Version: confluence.PageVersion{Number: 1},
					Body:    confluence.PageBody{Storage: confluence.StorageBody{Value: body.String()}},
				}, nil
			},
		},
	}

	// First call — full page by ID.
	result, _, err := h.handleRead(context.Background(), nil, ReadArgs{PageIDs: []string{"p-chunked"}})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	first := firstText(t, result)
	// Body is everything before the first "---" separator; the TOC trailer
	// legitimately lists every heading so we don't check against that.
	firstBody, _, _ := strings.Cut(first, "\n---\n")
	assert.Contains(t, firstBody, "## Section 1", "first chunk body must include Section 1")
	assert.NotContains(t, firstBody, "## Section 3", "first chunk body must not leak later sections")
	token1 := extractTokenTrailer(t, first)
	require.NotEmpty(t, token1, "first chunk must carry next_page_token")

	// Walk continuations until the token is empty. The page has 5 H2s so we
	// expect 4 continuation calls (sections 2..5) plus the initial call.
	seenSections := map[string]bool{"Section 1": true}
	nextToken := token1
	for step := 0; step < 10 && nextToken != ""; step++ {
		result, _, err = h.handleRead(context.Background(), nil, ReadArgs{NextPageToken: nextToken})
		require.NoError(t, err)
		require.False(t, result.IsError, "continuation call failed at step %d", step)
		text := firstText(t, result)

		// Record which Section N appears as the dominant content.
		for i := 1; i <= 5; i++ {
			label := fmt.Sprintf("Section %d", i)
			headingMarker := "## " + label
			if strings.Contains(text, headingMarker) && !seenSections[label] {
				seenSections[label] = true
			}
		}
		nextToken = extractTokenTrailer(t, text)
	}

	for i := 1; i <= 5; i++ {
		assert.Truef(t, seenSections[fmt.Sprintf("Section %d", i)],
			"expected Section %d to appear across continuation chunks", i)
	}
	assert.Empty(t, nextToken, "final chunk must have no next_page_token")
}

func TestChunkingTokenRoundtripSingleLongSection(t *testing.T) {
	// One H2 with a body larger than maxPageSize (20_000). Force byte-offset
	// chunking because there is no second H2 to cut at.
	var body strings.Builder
	body.WriteString("<h2>Only section</h2>")
	// Each <p> adds ~40 bytes; 1000 paragraphs ≈ 40 KB of markdown.
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&body, "<p>paragraph %04d content</p>", i)
	}

	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:      "p-single",
					Title:   "Single long section",
					Version: confluence.PageVersion{Number: 1},
					Body:    confluence.PageBody{Storage: confluence.StorageBody{Value: body.String()}},
				}, nil
			},
		},
	}

	// First call.
	result, _, err := h.handleRead(context.Background(), nil, ReadArgs{PageIDs: []string{"p-single"}})
	require.NoError(t, err)
	first := firstText(t, result)
	token := extractTokenTrailer(t, first)
	require.NotEmpty(t, token, "long single-section page must emit a byte-offset token")

	// Decode and verify the token is in "offset" mode.
	cur, err := decodeChunkToken(token)
	require.NoError(t, err)
	assert.Equal(t, "offset", cur.Mode, "single-section page should emit offset-mode token")
	assert.Equal(t, "p-single", cur.PageID)
	assert.Greater(t, cur.Offset, 0)

	// Second call with the token — should continue and eventually complete.
	seenFirstParagraph := strings.Contains(first, "paragraph 0000")
	assert.True(t, seenFirstParagraph, "first chunk should include early paragraphs")

	// Walk through remaining chunks.
	nextToken := token
	var last string
	for step := 0; step < 10 && nextToken != ""; step++ {
		result, _, err = h.handleRead(context.Background(), nil, ReadArgs{NextPageToken: nextToken})
		require.NoError(t, err)
		require.False(t, result.IsError, "continuation failed at step %d", step)
		last = firstText(t, result)
		nextToken = extractTokenTrailer(t, last)
	}

	assert.Contains(t, last, "paragraph 0999", "final chunk must include the last paragraph")
	assert.Empty(t, nextToken, "must terminate with no token")
}

func TestReadNextChunk_InvalidToken(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	result, _, err := h.readNextChunk(context.Background(), "not-valid-base64!!!")
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "invalid next_page_token")
}

func TestReadNextChunk_TokenMissingPageID(t *testing.T) {
	token, err := encodeChunkToken(chunkCursor{Mode: "section"})
	require.NoError(t, err)

	h := &handlers{client: &mockClient{}}
	result, _, err := h.readNextChunk(context.Background(), token)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "missing page_id")
}

func TestReadNextChunk_CacheMiss_RefetchError(t *testing.T) {
	token, err := encodeChunkToken(chunkCursor{PageID: "p1", Mode: "section"})
	require.NoError(t, err)

	h := &handlers{client: &mockClient{
		GetPageFn: func(_ context.Context, _ string) (*confluence.Page, error) {
			return nil, assert.AnError
		},
	}}
	result, _, err := h.readNextChunk(context.Background(), token)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "error fetching page")
}

func TestReadNextChunk_CacheMiss_SilentRefetch(t *testing.T) {
	var body strings.Builder
	body.WriteString("<h2>S1</h2><p>one</p>")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&body, "<p>padding %04d</p>", i)
	}
	body.WriteString("<h2>S2</h2><p>two</p>")

	h := &handlers{client: &mockClient{
		GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
			return &confluence.Page{
				ID: id, Title: "T",
				Version: confluence.PageVersion{Number: 1},
				Body:    confluence.PageBody{Storage: confluence.StorageBody{Value: body.String()}},
			}, nil
		},
	}}

	token, err := encodeChunkToken(chunkCursor{PageID: "p1", Mode: "section", SectionIdx: 1})
	require.NoError(t, err)

	result, _, err := h.readNextChunk(context.Background(), token)
	require.NoError(t, err)
	assert.False(t, result.IsError, "cache-miss continuation should silently refetch and succeed")
	text := firstText(t, result)
	assert.Contains(t, text, "continuation")
}

func TestChunkToken_DecodeBadBase64(t *testing.T) {
	_, err := decodeChunkToken("!!!not-base64!!!")
	assert.Error(t, err)
}

func TestChunkToken_DecodeBadJSON(t *testing.T) {
	// Valid base64, invalid JSON payload.
	bad := "bm90LWpzb24" // "not-json" base64 raw-url encoded
	_, err := decodeChunkToken(bad)
	assert.Error(t, err)
}

func TestChunkToken_Roundtrip(t *testing.T) {
	orig := chunkCursor{PageID: "p1", Mode: "offset", Offset: 12345}
	token, err := encodeChunkToken(orig)
	require.NoError(t, err)
	decoded, err := decodeChunkToken(token)
	require.NoError(t, err)
	assert.Equal(t, orig, decoded)
}

func TestCache_MacroTTL(t *testing.T) {
	c := &pageCache{}

	// Page without macros — should expire after 60s.
	c.put(&cachedPage{
		pageID:    "no-macros",
		markdown:  "plain",
		fetchedAt: time.Now().Add(-90 * time.Second),
	})
	_, ok := c.get("no-macros")
	assert.False(t, ok, "non-macro page should have expired after 90s")

	// Page with macros — should still be valid within 5min TTL.
	c.put(&cachedPage{
		pageID:    "has-macros",
		markdown:  "with macros",
		macros:    &mdconv.MacroRegistry{Entries: []mdconv.MacroEntry{{ID: "m1"}}},
		fetchedAt: time.Now().Add(-90 * time.Second),
	})
	_, ok = c.get("has-macros")
	assert.True(t, ok, "macro page should still be valid within 5-minute TTL")
}
