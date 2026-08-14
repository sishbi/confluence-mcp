package confluencemcp

import (
	"context"
	"fmt"
	"reflect"
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
			GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
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
			GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
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

// TestHandleRead_FocusedInlineComment covers D7: a focusedCommentId permalink
// to an inline comment 404s against GetFooterComment (footer-only), so
// readByURL must fall back to GetInlineComment and label the resolved type.
func TestHandleRead_FocusedInlineComment(t *testing.T) {
	notFound := &confluence.APIError{StatusCode: 404, Body: "not found"}

	t.Run("footer 404s, inline succeeds, page not cached", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
					// Real client always returns a non-nil pointer alongside
					// the error — the fallback must branch on err, not on
					// comment != nil.
					return &confluence.Comment{}, notFound
				},
				GetInlineCommentFn: func(ctx context.Context, commentID string) (*confluence.InlineComment, error) {
					assert.Equal(t, "456", commentID)
					return &confluence.InlineComment{
						ID:   "456",
						Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Inline comment text</p>"}},
					}, nil
				},
				GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
					return &confluence.Page{
						ID:    "123",
						Title: "Test Page",
						Body:  confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Page content</p>"}},
					}, nil
				},
			},
		}

		args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "Inline comment text")
		assert.Contains(t, text, "(type: inline)")
		assert.Contains(t, text, "**Comment ID:** 456  (type: inline)")
		assert.Contains(t, text, "Page content")
	})

	t.Run("footer 404s, inline succeeds, page cached", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				// GetPageFn intentionally NOT set — must not be called.
				GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
					return &confluence.Comment{}, notFound
				},
				GetInlineCommentFn: func(ctx context.Context, commentID string) (*confluence.InlineComment, error) {
					return &confluence.InlineComment{
						ID:   "456",
						Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Inline comment text</p>"}},
					}, nil
				},
			},
		}
		h.cache.put(&cachedPage{pageID: "123", markdown: "# Cached page", fetchedAt: time.Now()})

		args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "Inline comment text")
		assert.Contains(t, text, "(type: inline)")
		assert.NotContains(t, text, "Cached page")
	})

	t.Run("footer succeeds — inline arm never called", func(t *testing.T) {
		inlineCalls := 0
		h := &handlers{
			client: &mockClient{
				GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
					return &confluence.Comment{
						ID:   "456",
						Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Footer comment text</p>"}},
					}, nil
				},
				GetInlineCommentFn: func(ctx context.Context, commentID string) (*confluence.InlineComment, error) {
					inlineCalls++
					return &confluence.InlineComment{}, nil
				},
			},
		}
		h.cache.put(&cachedPage{pageID: "123", markdown: "# Cached page", fetchedAt: time.Now()})

		args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "Footer comment text")
		assert.Contains(t, text, "(type: footer)")
		assert.Equal(t, 0, inlineCalls, "footer success must not trigger the inline fallback")
	})

	t.Run("footer 500 does not fall back to inline", func(t *testing.T) {
		serverErr := &confluence.APIError{StatusCode: 500, Body: "boom"}
		inlineCalls := 0
		h := &handlers{
			client: &mockClient{
				GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
					return &confluence.Comment{}, serverErr
				},
				GetInlineCommentFn: func(ctx context.Context, commentID string) (*confluence.InlineComment, error) {
					inlineCalls++
					return &confluence.InlineComment{}, nil
				},
			},
		}

		args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.True(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "456")
		assert.Contains(t, text, serverErr.Error())
		assert.Equal(t, 0, inlineCalls, "a non-404 footer error must not trigger the inline fallback")
	})

	t.Run("both footer and inline fail — one clear error naming the comment ID", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
					return &confluence.Comment{}, notFound
				},
				GetInlineCommentFn: func(ctx context.Context, commentID string) (*confluence.InlineComment, error) {
					return &confluence.InlineComment{}, notFound
				},
			},
		}

		args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.True(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "456")
		assert.Equal(t, 1, strings.Count(text, "456"), "the failure must surface as one clear error, not two stacked errors")
		assert.NotContains(t, text, "confluence API error 404", "a bare 404 must not leak through when both fetches fail")
	})

	t.Run("footer 404s, inline 500s — the inline failure is surfaced, not a false \"not found\"", func(t *testing.T) {
		inlineServerErr := &confluence.APIError{StatusCode: 500, Body: "boom"}
		h := &handlers{
			client: &mockClient{
				GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
					return &confluence.Comment{}, notFound
				},
				GetInlineCommentFn: func(ctx context.Context, commentID string) (*confluence.InlineComment, error) {
					return &confluence.InlineComment{}, inlineServerErr
				},
			},
		}

		args := ReadArgs{URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.True(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, inlineServerErr.Error(), "the underlying inline failure must be visible, not swallowed")
		assert.NotContains(t, text, "not found as footer or inline comment",
			"a non-404 inline failure must not be misreported as the comment not existing")
	})
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

// TestHandleRead_SectionHonouredForBothPageIDSpellings covers the bug where
// section was silently ignored whenever the caller used page_ids (the
// documented primary mode) instead of the singular page_id — dispatch fell
// through to readByIDs, which does not know about Section at all.
func TestHandleRead_SectionHonouredForBothPageIDSpellings(t *testing.T) {
	md := "# Introduction\n\nIntro text.\n\n## Details\n\nDetail content.\n\n## Conclusion\n\nFinal."

	cases := []struct {
		name string
		args ReadArgs
	}{
		{name: "page_id only", args: ReadArgs{PageID: "123", Section: "Details"}},
		{name: "page_ids single", args: ReadArgs{PageIDs: []string{"123"}, Section: "Details"}},
		{name: "page_ids single + page_id same", args: ReadArgs{PageIDs: []string{"123"}, PageID: "123", Section: "Details"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &handlers{client: &mockClient{}}
			h.cache.put(&cachedPage{
				pageID:    "123",
				markdown:  md,
				sections:  parseSections(md),
				fetchedAt: time.Now(),
			})

			result, _, err := h.handleRead(context.Background(), nil, tc.args)
			require.NoError(t, err)
			require.False(t, result.IsError)
			text := firstText(t, result)
			assert.Contains(t, text, "Detail content.")
			assert.NotContains(t, text, "Final.")
			assert.NotContains(t, text, "Table of Contents", "a section hit must not fall through to the chunked/TOC response")
		})
	}
}

// TestHandleRead_SectionRejectsUnservableArgs covers combinations that
// section cannot serve — more than one resolved page id, or a section
// request paired with cql or resource, which each already claim
// next_page_token/dispatch for themselves. Each must surface an explicit
// error, never a silent fall-through to a different mode.
func TestHandleRead_SectionRejectsUnservableArgs(t *testing.T) {
	cases := []struct {
		name string
		args ReadArgs
		want string
	}{
		{
			name: "section + two page_ids",
			args: ReadArgs{Section: "Details", PageIDs: []string{"1", "2"}},
			want: "page_ids",
		},
		{
			name: "section + cql",
			args: ReadArgs{Section: "Details", PageID: "1", CQL: "type=page"},
			want: "cql",
		},
		{
			name: "section + resource",
			args: ReadArgs{Section: "Details", PageID: "1", Resource: "children"},
			want: "resource",
		},
		{
			name: "section + url",
			args: ReadArgs{Section: "Details", URL: "https://company.atlassian.net/wiki/spaces/DEV/pages/1/Title"},
			want: "url",
		},
		{
			name: "bare section with no page id",
			args: ReadArgs{Section: "Details"},
			want: "exactly one page id",
		},
		{
			name: "section + next_page_token",
			args: ReadArgs{Section: "Details", PageID: "1", NextPageToken: "sometoken"},
			want: "next_page_token",
		},
		{
			name: "section + format=storage",
			args: ReadArgs{Section: "Details", PageID: "1", Format: "storage"},
			want: `format="storage"`,
		},
		{
			name: "section + limit",
			args: ReadArgs{Section: "Details", PageID: "1", Limit: 1},
			want: "limit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A servable client, deliberately: the cases that reach the
			// client without their guard (section + format/limit) must fail
			// on the assertion below rather than on a nil-mock panic, so the
			// test pins the rejection and not merely "something went wrong".
			fetched := 0
			h := &handlers{
				client: &mockClient{
					GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
						fetched++
						return &confluence.Page{
							ID:    id,
							Title: "T",
							Body:  confluence.PageBody{Storage: confluence.StorageBody{Value: "<h1>Details</h1><p>Body.</p>"}},
						}, nil
					},
				},
			}
			result, _, err := h.handleRead(context.Background(), nil, tc.args)
			require.NoError(t, err)
			require.True(t, result.IsError, "an unservable section combination must be a hard error, not a silent fall-through")
			assert.Zero(t, fetched, "an unservable combination must be rejected before any page is fetched")
			text := firstText(t, result)
			assert.Contains(t, text, "section")
			assert.Contains(t, text, tc.want)
		})
	}
}

// TestHandleRead_SectionRejectsPageIDCollisionNamingBothSpellings covers I4:
// when section collides with two disagreeing page ids supplied under
// different spellings (page_ids vs page_id), the error must name both real
// spellings rather than hard-coding "page_ids and section".
func TestHandleRead_SectionRejectsPageIDCollisionNamingBothSpellings(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	args := ReadArgs{Section: "Details", PageIDs: []string{"1"}, PageID: "2"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.True(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "page_ids", "the collision is page_ids vs page_id, not section vs page_ids")
	assert.Contains(t, text, "page_id", "the collision is page_ids vs page_id, not section vs page_ids")
}

// TestHandleRead_PageIDsAndPageIDDisagree covers I3: page_ids and a
// disagreeing page_id used to be silently resolved by ignoring page_id and
// fetching only the ids in page_ids.
func TestHandleRead_PageIDsAndPageIDDisagree(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	args := ReadArgs{PageIDs: []string{"A"}, PageID: "B"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.True(t, result.IsError, "a disagreeing page_id must be rejected, not silently dropped")
	text := firstText(t, result)
	assert.Contains(t, text, "A")
	assert.Contains(t, text, "B")
}

// TestHandleRead_SectionFetchesOnCacheMiss covers the secondary defect: a
// cold page_id + section call used to error with "not in cache — fetch it
// first" even though the handler holds a client that can fetch the page
// itself.
func TestHandleRead_SectionFetchesOnCacheMiss(t *testing.T) {
	calls := 0
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
				calls++
				assert.Equal(t, "123", id)
				return &confluence.Page{
					ID:    id,
					Title: "Test",
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: "<h1>Introduction</h1><p>Intro.</p><h2>Details</h2><p>Detail content.</p>",
					}},
					Version: confluence.PageVersion{Number: 1},
				}, nil
			},
		},
	}

	args := ReadArgs{PageID: "123", Section: "Details"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "Detail content.")
	assert.Equal(t, 1, calls, "cache miss must fetch the page exactly once")

	// A section genuinely absent from the fetched page must still 404.
	args2 := ReadArgs{PageID: "123", Section: "Nonexistent"}
	result2, _, err := h.handleRead(context.Background(), nil, args2)
	require.NoError(t, err)
	require.True(t, result2.IsError)
	assert.Contains(t, firstText(t, result2), `section "Nonexistent" not found`)
	assert.Equal(t, 1, calls, "a second section lookup must be served from the now-warm cache, not refetched")
}

// TestHandleRead_NextPageTokenWithPageIDs covers the bug where a
// next_page_token was silently dropped whenever page_ids was also set,
// re-fetching chunk 1 instead of the requested continuation. It also
// verifies the guard's widening did not accidentally break cql's own
// legitimate use of next_page_token as its pagination cursor.
func TestHandleRead_NextPageTokenWithPageIDs(t *testing.T) {
	t.Run("page_ids + token returns the chunk the token addresses", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		md := "# S1\n\nOne.\n\n# S2\n\nTwo."
		h.cache.put(&cachedPage{pageID: "123", markdown: md, sections: parseSections(md), fetchedAt: time.Now()})
		token, err := encodeChunkToken(chunkCursor{PageID: "123", Mode: "section", SectionIdx: 1})
		require.NoError(t, err)

		args := ReadArgs{PageIDs: []string{"123"}, NextPageToken: token}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "continuation", "token must route to chunk continuation, not re-fetch chunk 1")
		assert.Contains(t, text, "S2")
		assert.NotContains(t, text, "One.", "the addressed chunk must not re-include the first section's body")
	})

	t.Run("token + cql is cql's own pagination cursor, not chunk continuation", func(t *testing.T) {
		var receivedCursor string
		h := &handlers{
			client: &mockClient{
				SearchContentFn: func(ctx context.Context, cql string, opts *confluence.ListOptions) (*confluence.SearchResult, error) {
					receivedCursor = opts.Cursor
					return &confluence.SearchResult{}, nil
				},
			},
		}
		args := ReadArgs{CQL: "type=page", NextPageToken: "cursor-abc"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError, "cql + next_page_token is a legitimate CQL continuation, not an error")
		assert.Equal(t, "cursor-abc", receivedCursor, "widening chunk continuation to page_ids must not hijack cql's own cursor")
	})

	t.Run("token + resource=children is children's own pagination cursor, not chunk continuation", func(t *testing.T) {
		var receivedCursor string
		h := &handlers{
			client: &mockClient{
				GetPageChildrenFn: func(ctx context.Context, id string, opts *confluence.ListOptions) ([]confluence.Page, string, error) {
					receivedCursor = opts.Cursor
					return nil, "", nil
				},
			},
		}
		args := ReadArgs{Resource: "children", PageID: "123", NextPageToken: "cursor-xyz"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError, "resource=children + next_page_token is a legitimate continuation, not an error")
		assert.Equal(t, "cursor-xyz", receivedCursor, "widening chunk continuation to page_ids must not hijack resource's own cursor")
	})

	t.Run("page_ids [A] + token for B is rejected, not silently swapped to B", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		token, err := encodeChunkToken(chunkCursor{PageID: "B", Mode: "section", SectionIdx: 0})
		require.NoError(t, err)

		args := ReadArgs{PageIDs: []string{"A"}, NextPageToken: token}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.True(t, result.IsError, "a token addressing a page the caller did not name must be rejected")
		text := firstText(t, result)
		assert.Contains(t, text, "B")
	})

	t.Run("page_ids [A,B] + token for A is rejected, not silently narrowed to A", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		token, err := encodeChunkToken(chunkCursor{PageID: "A", Mode: "section", SectionIdx: 0})
		require.NoError(t, err)

		args := ReadArgs{PageIDs: []string{"A", "B"}, NextPageToken: token}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.True(t, result.IsError, "a token continues a single page — more than one supplied page id must be rejected")
	})

	t.Run("token + url naming the token's page continues the chunked read", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		md := "# S1\n\nOne.\n\n# S2\n\nTwo."
		h.cache.put(&cachedPage{pageID: "123", markdown: md, sections: parseSections(md), fetchedAt: time.Now()})
		token, err := encodeChunkToken(chunkCursor{PageID: "123", Mode: "section", SectionIdx: 1})
		require.NoError(t, err)

		args := ReadArgs{URL: "https://example.atlassian.net/wiki/spaces/DEV/pages/123/Title", NextPageToken: token}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError, "a url naming the token's own page must continue the read, not error")
		text := firstText(t, result)
		assert.Contains(t, text, "continuation", "the token must route to chunk continuation, not a full re-fetch")
		assert.Contains(t, text, "S2")
		assert.NotContains(t, text, "One.", "the addressed chunk must not re-include the first section's body")
	})

	t.Run("token + url naming a different page is rejected, not silently dropped", func(t *testing.T) {
		fetched := 0
		h := &handlers{
			client: &mockClient{
				GetPageFn: func(ctx context.Context, id string) (*confluence.Page, error) {
					fetched++
					return &confluence.Page{
						ID:    id,
						Title: "T",
						Body:  confluence.PageBody{Storage: confluence.StorageBody{Value: "<h1>S1</h1><p>One.</p>"}},
					}, nil
				},
			},
		}
		token, err := encodeChunkToken(chunkCursor{PageID: "999", Mode: "section", SectionIdx: 1})
		require.NoError(t, err)

		args := ReadArgs{URL: "https://example.atlassian.net/wiki/spaces/DEV/pages/123/Title", NextPageToken: token}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.True(t, result.IsError, "a token addressing a page the url does not name must be rejected")
		assert.Zero(t, fetched, "the mismatch must be caught before any page is fetched")
		assert.Contains(t, firstText(t, result), "999")
	})

	t.Run("token + focusedCommentId permalink is rejected", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		token, err := encodeChunkToken(chunkCursor{PageID: "123", Mode: "section", SectionIdx: 1})
		require.NoError(t, err)

		args := ReadArgs{
			URL:           "https://example.atlassian.net/wiki/spaces/DEV/pages/123/Title?focusedCommentId=456",
			NextPageToken: token,
		}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.True(t, result.IsError, "a comment permalink has no chunk-continuation semantics")
		assert.Contains(t, firstText(t, result), "focusedCommentId")
	})
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
			GetPageFooterCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				assert.Equal(t, "123", pageID)
				return []confluence.Comment{
					{ID: "c1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>First comment</p>"}}},
					{ID: "c2", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Second comment</p>"}}},
				}, "", nil
			},
			GetFooterCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				if commentID == "c1" {
					return []confluence.Comment{
						{ID: "c1-r1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>A reply</p>"}}},
					}, "", nil
				}
				return nil, "", nil
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
	assert.Contains(t, text, "(type: footer)")
	assert.Contains(t, text, "**Comment ID:** c1  (type: footer)")
	assert.Contains(t, text, "A reply")
	assert.Contains(t, text, "  **Reply ID:** c1-r1", "reply must be indented two spaces under its parent thread")
	assert.NotContains(t, text, "**Status:**")
}

func TestHandleRead_ListComments_CapsChildFetches(t *testing.T) {
	const totalThreads = maxChildFetchThreads + 3
	var comments []confluence.Comment
	for i := 0; i < totalThreads; i++ {
		comments = append(comments, confluence.Comment{
			ID:   fmt.Sprintf("c%d", i),
			Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>body</p>"}},
		})
	}

	childFetchCalls := 0
	h := &handlers{
		client: &mockClient{
			GetPageFooterCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				return comments, "", nil
			},
			GetFooterCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				childFetchCalls++
				return nil, "", nil
			},
		},
	}

	args := ReadArgs{Resource: "comments", PageID: "123"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := firstText(t, result)
	assert.Equal(t, maxChildFetchThreads, childFetchCalls, "child fetches must stop at the cap, same as inline_comments")
	assert.Contains(t, text, "with a smaller `limit` to see the rest",
		"footer truncation notice must point at limit alone, not the inline-only resolution_status filter")
	assert.Contains(t, text, fmt.Sprintf("past the per-read cap of %d", maxChildFetchThreads),
		"a thread past the cap must carry its own notice — otherwise it is indistinguishable from a thread with no replies")
}

func TestHandleRead_ListComments_ChildFetchError_DegradesGracefully(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageFooterCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				return []confluence.Comment{
					{ID: "c1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>First thread</p>"}}},
					{ID: "c2", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Second thread</p>"}}},
				}, "", nil
			},
			GetFooterCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				if commentID == "c1" {
					return nil, "", assert.AnError
				}
				return []confluence.Comment{
					{ID: "c2-r1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Second reply</p>"}}},
				}, "", nil
			},
		},
	}

	args := ReadArgs{Resource: "comments", PageID: "123"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.False(t, result.IsError, "a single thread's child-fetch error must not fail the whole read")
	text := firstText(t, result)
	assert.Contains(t, text, "First thread", "the failing thread's own parent comment must still render")
	assert.Contains(t, text, "Second thread")
	assert.Contains(t, text, "Second reply", "an unaffected thread's replies must still render")
	assert.Contains(t, text, "*Replies could not be loaded: "+assert.AnError.Error()+"*",
		"the failing thread must carry a clear per-thread notice naming the reason")
}

func TestHandleRead_ListComments_ChildCursorNonEmpty_EmitsPerThreadNotice(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			GetPageFooterCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				return []confluence.Comment{
					{ID: "c1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Body</p>"}}},
				}, "", nil
			},
			GetFooterCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				return []confluence.Comment{
					{ID: "r1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Reply</p>"}}},
				}, "more-replies-cursor", nil
			},
		},
	}

	args := ReadArgs{Resource: "comments", PageID: "123"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, fmt.Sprintf("more than %d replies", maxRepliesPerThread),
		"a non-empty child cursor must not be silently discarded")
}

func TestHandleRead_ListComments_ResolvesUserMentionsOnce(t *testing.T) {
	const mention = `<ac:link><ri:user ri:account-id="acc-1"/></ac:link>`
	var getUserCalls int
	h := &handlers{
		client: &mockClient{
			GetUserFn: func(ctx context.Context, accountID string) (*confluence.User, error) {
				getUserCalls++
				return &confluence.User{AccountID: accountID, DisplayName: "Alice"}, nil
			},
			GetPageFooterCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				return []confluence.Comment{
					{ID: "c1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Ping " + mention + "</p>"}}},
					{ID: "c2", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Again " + mention + "</p>"}}},
				}, "", nil
			},
			GetFooterCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
				return nil, "", nil
			},
		},
	}

	args := ReadArgs{Resource: "comments", PageID: "123"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "@Alice")
	assert.Contains(t, text, "acc-1")
	assert.NotContains(t, text, "@user(acc-1)")
	assert.Equal(t, 1, getUserCalls, "resolver should be shared so GetUser is called once per unique account id")
}

func TestHandleRead_ByURL_CommentResolvesUserMention(t *testing.T) {
	const mention = `<ac:link><ri:user ri:account-id="acc-1"/></ac:link>`
	h := &handlers{
		client: &mockClient{
			GetUserFn: func(ctx context.Context, accountID string) (*confluence.User, error) {
				return &confluence.User{AccountID: accountID, DisplayName: "Bob"}, nil
			},
			GetFooterCommentFn: func(ctx context.Context, commentID string) (*confluence.Comment, error) {
				return &confluence.Comment{ID: commentID, Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Hi " + mention + "</p>"}}}, nil
			},
		},
		cache: pageCache{},
	}
	// Seed the cache so the URL path returns comment-only (doesn't re-fetch page).
	h.cache.put(&cachedPage{pageID: "4125", markdown: "", fetchedAt: time.Now()})

	args := ReadArgs{URL: "https://x.atlassian.net/wiki/spaces/S/pages/4125/Title?focusedCommentId=c1"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "@Bob")
	assert.Contains(t, text, "acc-1")
	assert.NotContains(t, text, "@user(acc-1)")
}

func TestHandleRead_ListComments_MissingPageID(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{Resource: "comments"}
	result, _, err := h.handleRead(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "page_id")
}

func TestHandleRead_ListInlineComments(t *testing.T) {
	t.Run("renders thread with status, selection, type label, and nested reply", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				GetPageInlineCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					assert.Equal(t, "123", pageID)
					return []confluence.InlineComment{
						{
							ID:               "4808802349",
							Body:             confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Inline body</p>"}},
							ResolutionStatus: confluence.ResolutionOpen,
							Properties:       confluence.InlineCommentProperties{InlineOriginalSelection: "the highlighted source text"},
						},
					}, "", nil
				},
				GetInlineCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					assert.Equal(t, "4808802349", commentID)
					return []confluence.InlineComment{
						{ID: "4808802350", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Reply body</p>"}}},
					}, "", nil
				},
			},
		}

		args := ReadArgs{Resource: "inline_comments", PageID: "123"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "the highlighted source text")
		assert.Contains(t, text, "**Status:** open")
		assert.Contains(t, text, "(type: inline)")
		assert.Contains(t, text, "4808802349")
		assert.Contains(t, text, "**Comment ID:** 4808802349  (type: inline)")
		assert.Contains(t, text, "Reply body")
		assert.Contains(t, text, "  **Reply ID:** 4808802350", "reply must be indented two spaces under its parent thread")
	})

	t.Run("resolution_status reaches the client", func(t *testing.T) {
		var received []string
		h := &handlers{
			client: &mockClient{
				GetPageInlineCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					received = opts.ResolutionStatus
					return nil, "", nil
				},
			},
		}

		args := ReadArgs{
			Resource:         "inline_comments",
			PageID:           "123",
			ResolutionStatus: []string{confluence.ResolutionOpen, confluence.ResolutionReopened},
		}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		assert.Equal(t, []string{confluence.ResolutionOpen, confluence.ResolutionReopened}, received)
	})

	t.Run("rejects unknown resolution_status value", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}

		args := ReadArgs{Resource: "inline_comments", PageID: "123", ResolutionStatus: []string{"bogus"}}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, "bogus")
		assert.Contains(t, text, confluence.ResolutionOpen)
		assert.Contains(t, text, confluence.ResolutionResolved)
		assert.Contains(t, text, confluence.ResolutionDangling)
		assert.Contains(t, text, confluence.ResolutionReopened)
	})

	t.Run("caps child fetches at the threshold and reports truncation", func(t *testing.T) {
		const totalThreads = maxChildFetchThreads + 3
		var comments []confluence.InlineComment
		for i := 0; i < totalThreads; i++ {
			comments = append(comments, confluence.InlineComment{
				ID:   fmt.Sprintf("c%d", i),
				Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>body</p>"}},
			})
		}

		childFetchCalls := 0
		h := &handlers{
			client: &mockClient{
				GetPageInlineCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					return comments, "", nil
				},
				GetInlineCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					childFetchCalls++
					return nil, "", nil
				},
			},
		}

		args := ReadArgs{Resource: "inline_comments", PageID: "123"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)
		assert.Equal(t, maxChildFetchThreads, childFetchCalls, "child fetches must stop at the cap")
		assert.Contains(t, text, "narrower `resolution_status`",
			"inline truncation notice must point at resolution_status, not the footer-only limit wording")
		assert.Contains(t, text, fmt.Sprintf("past the per-read cap of %d", maxChildFetchThreads),
			"a thread past the cap must carry its own notice — otherwise it is indistinguishable from a thread with no replies")
	})

	t.Run("a child-fetch error on one thread degrades gracefully instead of failing the read", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				GetPageInlineCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					return []confluence.InlineComment{
						{ID: "c1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>First thread</p>"}}},
						{ID: "c2", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Second thread</p>"}}},
					}, "", nil
				},
				GetInlineCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					if commentID == "c1" {
						return nil, "", assert.AnError
					}
					return []confluence.InlineComment{
						{ID: "c2-r1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Second reply</p>"}}},
					}, "", nil
				},
			},
		}

		args := ReadArgs{Resource: "inline_comments", PageID: "123"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError, "a single thread's child-fetch error must not fail the whole read")
		text := firstText(t, result)
		assert.Contains(t, text, "First thread", "the failing thread's own parent comment must still render")
		assert.Contains(t, text, "Second thread")
		assert.Contains(t, text, "Second reply", "an unaffected thread's replies must still render")
		assert.Contains(t, text, "*Replies could not be loaded: "+assert.AnError.Error()+"*",
			"the failing thread must carry a clear per-thread notice naming the reason")
	})

	t.Run("a non-empty child cursor emits a per-thread notice instead of being silently discarded", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				GetPageInlineCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					return []confluence.InlineComment{
						{ID: "c1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Body</p>"}}},
					}, "", nil
				},
				GetInlineCommentChildrenFn: func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					return []confluence.InlineComment{
						{ID: "r1", Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Reply</p>"}}},
					}, "more-replies-cursor", nil
				},
			},
		}

		args := ReadArgs{Resource: "inline_comments", PageID: "123"}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)
		assert.Contains(t, text, fmt.Sprintf("more than %d replies", maxRepliesPerThread))
	})

	t.Run("continuation hint quotes resolution_status as a valid JSON array", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				GetPageInlineCommentsFn: func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
					return nil, "cursor-abc", nil
				},
			},
		}

		args := ReadArgs{
			Resource:         "inline_comments",
			PageID:           "123",
			ResolutionStatus: []string{"open", "reopened"},
		}
		result, _, err := h.handleRead(context.Background(), nil, args)
		require.NoError(t, err)
		require.False(t, result.IsError)
		text := firstText(t, result)

		const wantHint = "\n*More inline comments available — pass `next_page_token: \"cursor-abc\"` " +
			"with `resource: \"inline_comments\"` and `page_id: \"123\"` " +
			"and `resolution_status: [\"open\",\"reopened\"]` to continue.*"
		assert.Contains(t, text, wantHint, "hint must echo resolution_status as valid JSON, not Go's %%v slice syntax")
	})
}

func TestHandleRead_ListInlineComments_MissingPageID(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := ReadArgs{Resource: "inline_comments"}
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

func TestHandleRead_ResolutionStatus_RejectedOnNonInlineResources(t *testing.T) {
	// spaces takes no page_id, unlike the other three resources.
	cases := []struct {
		resource string
		pageID   string
	}{
		{resource: "spaces"},
		{resource: "comments", pageID: "123"},
		{resource: "children", pageID: "123"},
		{resource: "labels", pageID: "123"},
	}
	for _, tc := range cases {
		t.Run(tc.resource, func(t *testing.T) {
			h := &handlers{client: &mockClient{}}

			args := ReadArgs{Resource: tc.resource, PageID: tc.pageID, ResolutionStatus: []string{"open"}}
			result, _, err := h.handleRead(context.Background(), nil, args)
			require.NoError(t, err)
			require.True(t, result.IsError, "resolution_status must be rejected, not silently dropped, on resource=%q", tc.resource)
			text := firstText(t, result)
			assert.Contains(t, text, "resolution_status")
			assert.Contains(t, text, "inline_comments")
			assert.Contains(t, text, tc.resource)
		})
	}
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
	assert.Contains(t, firstText(t, result), "inline_comments")
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
	result, _, err := h.readNextChunk(context.Background(), "not-valid-base64!!!", nil)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, firstText(t, result), "invalid next_page_token")
}

func TestReadNextChunk_TokenMissingPageID(t *testing.T) {
	token, err := encodeChunkToken(chunkCursor{Mode: "section"})
	require.NoError(t, err)

	h := &handlers{client: &mockClient{}}
	result, _, err := h.readNextChunk(context.Background(), token, nil)
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
	result, _, err := h.readNextChunk(context.Background(), token, nil)
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

	result, _, err := h.readNextChunk(context.Background(), token, nil)
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

// TestReadTool_ResolutionStatusValuesMatch guards against the two
// agent-facing resolution-status strings — the confluence_read tool
// description's Options: line (server.go) and the ResolutionStatus field's
// jsonschema tag (tool_read.go) — drifting from resolutionStatusValues, the
// same duplication class already closed on the write side by
// TestWriteTool_DescriptionFormatActionNames. Both strings must mention
// every value in resolutionStatusValues; if a value is added, renamed, or
// removed there, both strings must be updated too.
func TestReadTool_ResolutionStatusValuesMatch(t *testing.T) {
	require.NotEmpty(t, resolutionStatusValues, "resolutionStatusValues must not be empty")

	argsType := reflect.TypeOf(ReadArgs{})
	var field reflect.StructField
	found := false
	for i := 0; i < argsType.NumField(); i++ {
		f := argsType.Field(i)
		if strings.Split(f.Tag.Get("json"), ",")[0] == "resolution_status" {
			field = f
			found = true
			break
		}
	}
	require.True(t, found, "no ReadArgs field carries the json tag %q", "resolution_status")

	jsonschemaTag := field.Tag.Get("jsonschema")
	require.NotEmpty(t, jsonschemaTag, "field %q has no jsonschema tag to check", field.Name)

	for _, v := range resolutionStatusValues {
		assert.Contains(t, readTool.Description, v,
			"confluence_read tool description (server.go Options: line) is missing resolution_status value %q", v)
		assert.Contains(t, jsonschemaTag, v,
			"ReadArgs.%s jsonschema tag is missing resolution_status value %q", field.Name, v)
	}
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
