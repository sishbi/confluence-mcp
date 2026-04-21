//go:build smoke

// Run with: go test -tags smoke -v ./scripts/ -timeout 60s
//
// Required env vars:
//   CONFLUENCE_URL, CONFLUENCE_EMAIL, CONFLUENCE_API_TOKEN
//
// Optional env vars (enable page-specific tests):
//   SMOKE_PAGE_ID     — a page ID to read/inspect
//   SMOKE_PAGE_URL    — full Confluence URL to that page
//   SMOKE_SPACE_KEY   — a space key for CQL search tests
//   SMOKE_COMMENT_ID  — a known comment ID on the page
//   SMOKE_COMMENT_URL — page URL with ?focusedCommentId= parameter
//
// This is a real integration test against a live Confluence instance.
// Read tests are safe. Write tests add/remove a "smoke-test" label and add a comment.

package scripts

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/confluencemcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smokePageID returns the page ID to use for smoke tests.
func smokePageID() string { return os.Getenv("SMOKE_PAGE_ID") }

// smokePageURL returns the full URL to use for URL-based read tests.
func smokePageURL() string { return os.Getenv("SMOKE_PAGE_URL") }

// smokeSpaceKey returns the space key for space-scoped searches.
func smokeSpaceKey() string { return os.Getenv("SMOKE_SPACE_KEY") }

// smokeCommentID returns a known comment ID on the smoke test page.
func smokeCommentID() string { return os.Getenv("SMOKE_COMMENT_ID") }

// smokeCommentURL returns a page URL with focusedCommentId for comment testing.
func smokeCommentURL() string { return os.Getenv("SMOKE_COMMENT_URL") }

// liveEnv holds the shared connection state for smoke tests.
type liveEnv struct {
	session *mcp.ClientSession
	client  *confluence.Client // raw client for direct API access (e.g. restore)
}

func newLiveEnv(t *testing.T) *liveEnv {
	t.Helper()

	url := os.Getenv("CONFLUENCE_URL")
	email := os.Getenv("CONFLUENCE_EMAIL")
	token := os.Getenv("CONFLUENCE_API_TOKEN")

	if url == "" || email == "" || token == "" {
		t.Fatal("CONFLUENCE_URL, CONFLUENCE_EMAIL, and CONFLUENCE_API_TOKEN must be set")
	}

	client, err := confluence.New(confluence.Config{
		URL:        url,
		Email:      email,
		APIToken:   token,
		MaxRetries: 2,
		BaseDelay:  time.Second,
	})
	require.NoError(t, err, "creating Confluence client")

	ctx := context.Background()
	user, err := client.GetCurrentUser(ctx)
	require.NoError(t, err, "fetching current user — check your credentials")
	t.Logf("Authenticated as: %s (%s)", user.DisplayName, user.AccountID)

	server := confluencemcp.NewServer(client, user, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, t1, nil)
	require.NoError(t, err, "connecting server")

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "smoke-test", Version: "v0.0.1"}, nil)
	session, err := mcpClient.Connect(ctx, t2, nil)
	require.NoError(t, err, "connecting client")
	t.Cleanup(func() { session.Close() })

	return &liveEnv{session: session, client: client}
}

// newLiveSession is a convenience wrapper for tests that only need the MCP session.
func newLiveSession(t *testing.T) *mcp.ClientSession {
	return newLiveEnv(t).session
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	require.NoError(t, err, "calling tool %s", name)
	require.NotEmpty(t, result.Content, "tool %s returned no content", name)

	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError {
		t.Logf("Tool %s returned error: %s", name, text[:min(len(text), 500)])
	} else {
		t.Logf("Tool %s response (%d chars): %.500s", name, len(text), text)
	}
	return text
}

func TestSmoke_ListTools(t *testing.T) {
	cs := newLiveSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, res.Tools, 2)
	t.Logf("Tools: %s, %s", res.Tools[0].Name, res.Tools[1].Name)
}

func TestSmoke_Instructions(t *testing.T) {
	cs := newLiveSession(t)
	init := cs.InitializeResult()
	require.NotNil(t, init)
	assert.Contains(t, init.Instructions, "confluence_read")
	assert.Contains(t, init.Instructions, "confluence_write")
	// Should contain current user
	assert.Contains(t, init.Instructions, "Current user")
	t.Logf("Instructions (first 300 chars): %.300s", init.Instructions)
}

func TestSmoke_ListSpaces(t *testing.T) {
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{"resource": "spaces"})
	assert.NotEmpty(t, text)
	assert.Contains(t, text, "Spaces")
	assert.Contains(t, text, "global")
	if key := smokeSpaceKey(); key != "" {
		assert.Contains(t, text, key)
	}
}

func TestSmoke_SearchCQL(t *testing.T) {
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{
		"cql":   "type=page",
		"limit": 3,
	})
	assert.NotEmpty(t, text)
}

func TestSmoke_ReadPageBySearch(t *testing.T) {
	cs := newLiveSession(t)
	ctx := context.Background()

	// Search for a page first
	searchResult, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"cql": "type=page", "limit": 1},
	})
	require.NoError(t, err)
	require.False(t, searchResult.IsError, "search failed")
	searchText := searchResult.Content[0].(*mcp.TextContent).Text
	t.Logf("Search result: %.300s", searchText)

	// We can't easily extract the page ID from the text output,
	// so just verify the search worked
	assert.NotEmpty(t, searchText)
}

func TestSmoke_ReadKnownPage(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{
		"page_ids": []any{pageID},
	})
	assert.Contains(t, text, pageID, "should include page ID")
	assert.Contains(t, text, "#", "should contain Markdown headings")

	// Converter health check — the smoke-test page exercises
	// tables, task lists, and content that would previously surface the
	// library's <!--THE END--> artifact. These assertions are also valid
	// for any page that contains a table and a task list.
	assert.Regexp(t, `\|[^\n]*\|[^\n]*\|`, text,
		"expected at least one GFM table row after converter fix")
	assert.Contains(t, text, "[x]", "expected a checked task marker from the fixture page")
	assert.Contains(t, text, "[ ]", "expected an unchecked task marker from the fixture page")
	assert.NotContains(t, text, "<!--THE END-->",
		"html-to-markdown terminator should be stripped by postprocessMarkdown")
}

func TestSmoke_ReadKnownPageByURL(t *testing.T) {
	pageURL := smokePageURL()
	if pageURL == "" {
		t.Skip("SMOKE_PAGE_URL not set")
	}
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{"url": pageURL})
	assert.Contains(t, text, "#")
}

func TestSmoke_ReadKnownPageSections(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	ctx := context.Background()

	// First read: fetches and caches the page
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{pageID}},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	fullText := result.Content[0].(*mcp.TextContent).Text
	t.Logf("Full page (%d chars): %.500s...", len(fullText), fullText)

	// Pick the first real heading from the fetched page and extract it.
	sectionName := firstHeading(t, fullText)
	t.Logf("Extracting section: %q", sectionName)
	result2, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_id": pageID, "section": sectionName},
	})
	require.NoError(t, err)
	require.False(t, result2.IsError, "section extraction should succeed for %q", sectionName)
	sectionText := result2.Content[0].(*mcp.TextContent).Text
	t.Logf("Section %q (%d chars): %.300s", sectionName, len(sectionText), sectionText)
	require.NotEmpty(t, sectionText)
	assert.Contains(t, sectionText, sectionName,
		"extracted section should include its heading text")
}

// firstHeading returns the first ATX heading found in md (without the leading
// # markers). Skips the page header bold line so we land on a real heading.
func firstHeading(t *testing.T, md string) string {
	t.Helper()
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		rest := strings.TrimLeft(trimmed, "#")
		rest = strings.TrimSpace(rest)
		if rest != "" {
			return rest
		}
	}
	t.Fatalf("no heading found in page markdown")
	return ""
}

func TestSmoke_SearchInKnownSpace(t *testing.T) {
	spaceKey := smokeSpaceKey()
	if spaceKey == "" {
		t.Skip("SMOKE_SPACE_KEY not set")
	}
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{
		"cql":   `type=page AND space="` + spaceKey + `"`,
		"limit": 5,
	})
	assert.NotEmpty(t, text)
	assert.Contains(t, text, "result")
}

func TestSmoke_ReadChildPages(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{
		"resource": "children",
		"page_id":  pageID,
	})
	assert.Contains(t, text, "Child pages")
}

func TestSmoke_ReadLabels(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{
		"resource": "labels",
		"page_id":  pageID,
	})
	assert.Contains(t, text, "Labels")
}

func TestSmoke_ReadComments(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_read", map[string]any{
		"resource": "comments",
		"page_id":  pageID,
	})
	assert.Contains(t, text, "Comments")
	if cid := smokeCommentID(); cid != "" {
		assert.Contains(t, text, cid, "should contain the known comment ID")
	}
	assert.NotContains(t, text, "@user(",
		"comment body should resolve user mentions to display names, not leave raw @user(accountId)")
}

func TestSmoke_ReadFocusedComment(t *testing.T) {
	commentURL := smokeCommentURL()
	if commentURL == "" {
		t.Skip("SMOKE_COMMENT_URL not set")
	}
	cs := newLiveSession(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"url": commentURL},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success")
	text := result.Content[0].(*mcp.TextContent).Text
	t.Logf("Focused comment response (%d chars): %.500s", len(text), text)

	// Extract the focusedCommentId from the URL and verify it's in the response.
	u, err := url.Parse(commentURL)
	require.NoError(t, err)
	focusedID := u.Query().Get("focusedCommentId")
	require.NotEmpty(t, focusedID, "SMOKE_COMMENT_URL should contain focusedCommentId parameter")
	assert.Contains(t, text, focusedID, "response should contain the focused comment ID")
	assert.Contains(t, text, "Comment")
}

func TestSmoke_WriteAddComment(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "comment",
			"items": []any{map[string]any{
				"page_id": pageID,
				"body":    "Automated smoke test comment — safe to delete.",
			}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	t.Logf("Add comment result: %s", text)
}

func TestSmoke_WriteAddAndRemoveLabel(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	ctx := context.Background()

	// Add label
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "add_label",
			"items":  []any{map[string]any{"page_id": pageID, "label": "smoke-test"}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	t.Logf("Add label result: %s", result.Content[0].(*mcp.TextContent).Text)

	// Verify label appears
	labelsText := callTool(t, cs, "confluence_read", map[string]any{
		"resource": "labels",
		"page_id":  pageID,
	})
	assert.Contains(t, labelsText, "smoke-test")

	// Remove label
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "remove_label",
			"items":  []any{map[string]any{"page_id": pageID, "label": "smoke-test"}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	t.Logf("Remove label result: %s", result.Content[0].(*mcp.TextContent).Text)
}

// extractVersion parses the version number from the MCP page read output.
// Looks for "**Version:** N" in the header line.
var versionRe = regexp.MustCompile(`\*\*Version:\*\*\s+(\d+)`)

func extractVersion(text string) int {
	m := versionRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	return v
}

// extractPageBody returns everything after the header line (first blank line).
func extractPageBody(text string) string {
	idx := strings.Index(text, "\n\n")
	if idx < 0 {
		return text
	}
	return strings.TrimSpace(text[idx+2:])
}

func TestSmoke_EditPageRoundTrip(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	env := newLiveEnv(t)
	cs := env.session
	ctx := context.Background()
	marker := "\n\n---\n\n*Smoke test marker — safe to delete.*"

	original := snapshotAndRestorePage(t, env, pageID)

	// Step 1: Read via MCP to get current version and Markdown content
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{pageID}},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	readText := result.Content[0].(*mcp.TextContent).Text
	readVersion := extractVersion(readText)
	readBody := extractPageBody(readText)
	require.Equal(t, original.Version.Number, readVersion)
	t.Logf("Read version: %d, body length: %d", readVersion, len(readBody))

	// Step 2: Update via MCP — append a marker
	modifiedBody := readBody + marker
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items": []any{map[string]any{
				"page_id":        pageID,
				"title":          original.Title,
				"body":           modifiedBody,
				"version_number": readVersion,
			}},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "update failed: %s", result.Content[0].(*mcp.TextContent).Text)
	t.Logf("Update result: %s", result.Content[0].(*mcp.TextContent).Text)

	// Step 3: Read back via MCP — cache should be evicted by the write
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{pageID}},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	updatedText := result.Content[0].(*mcp.TextContent).Text
	updatedVersion := extractVersion(updatedText)

	assert.Equal(t, readVersion+1, updatedVersion, "version should have incremented")
	assert.Contains(t, updatedText, "Smoke test marker", "marker should be in the updated page")
	t.Logf("Updated version: %d", updatedVersion)

	// Step 4: Do a second round-trip to verify converter stability
	updatedBody := extractPageBody(updatedText)
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items": []any{map[string]any{
				"page_id":        pageID,
				"title":          original.Title,
				"body":           updatedBody,
				"version_number": updatedVersion,
			}},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "second update failed: %s", result.Content[0].(*mcp.TextContent).Text)

	// Read after second round-trip
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{pageID}},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	secondText := result.Content[0].(*mcp.TextContent).Text
	secondVersion := extractVersion(secondText)
	secondBody := extractPageBody(secondText)

	assert.Equal(t, readVersion+2, secondVersion, "version should have incremented twice")
	// The html-to-markdown library introduces small formatting differences on each
	// cycle (whitespace normalisation, entity encoding). Allow up to 5% drift.
	drift := float64(len(secondBody)-len(updatedBody)) / float64(len(updatedBody)) * 100
	t.Logf("Second round-trip version: %d, body lengths: %d vs %d (%.1f%% drift)", secondVersion, len(updatedBody), len(secondBody), drift)
	assert.InDelta(t, len(updatedBody), len(secondBody), float64(len(updatedBody))*0.05,
		"body length drifted more than 5%% after second round-trip (converter stability)")

	// Cleanup restores original storage format via t.Cleanup above
}

func TestSmoke_ReadStorageFormat(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	cs := newLiveSession(t)
	ctx := context.Background()

	// Read in default (markdown) format first.
	mdResult, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{pageID}},
	})
	require.NoError(t, err)
	require.False(t, mdResult.IsError)
	mdText := mdResult.Content[0].(*mcp.TextContent).Text

	// Read the same page in storage format.
	storageResult, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_read",
		Arguments: map[string]any{
			"page_ids": []any{pageID},
			"format":   "storage",
		},
	})
	require.NoError(t, err)
	require.False(t, storageResult.IsError)
	storageText := storageResult.Content[0].(*mcp.TextContent).Text

	t.Logf("Markdown length: %d, Storage length: %d", len(mdText), len(storageText))
	t.Logf("Storage (first 500 chars): %.500s", storageText)

	// Storage format should contain raw HTML/XHTML tags.
	assert.Contains(t, storageText, "<", "should contain HTML tags")
	assert.Contains(t, storageText, "**Page ID:**", "should contain header")

	// Storage format should NOT contain macro comments (those are Markdown-mode only).
	assert.NotContains(t, storageText, "<!-- macro:", "storage format should not have macro comments")

	// If the page has macros, the raw XML should be visible.
	if strings.Contains(mdText, "<!-- macro:") {
		assert.Contains(t, storageText, "ac:structured-macro",
			"page has macros in Markdown mode but raw XML missing in storage mode")
	}
}

func TestSmoke_WriteStorageFormat_RoundTrip(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	env := newLiveEnv(t)
	cs := env.session
	ctx := context.Background()

	original := snapshotAndRestorePage(t, env, pageID)

	// 2. Read in storage format via MCP.
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_read",
		Arguments: map[string]any{
			"page_ids": []any{pageID},
			"format":   "storage",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	storageText := result.Content[0].(*mcp.TextContent).Text

	version := extractVersion(storageText)
	require.NotZero(t, version)
	require.Equal(t, original.Version.Number, version)

	// 3. Extract body (skip header), append a marker in raw XHTML.
	storageBody := extractPageBody(storageText)
	marker := fmt.Sprintf(`<p><em>Storage format smoke test: %d</em></p>`, time.Now().Unix())
	modifiedStorage := storageBody + marker

	// 4. Write back in storage format.
	writeResult, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items": []any{map[string]any{
				"page_id":        pageID,
				"title":          original.Title,
				"version_number": version,
				"format":         "storage",
				"body":           modifiedStorage,
			}},
		},
	})
	require.NoError(t, err)
	require.False(t, writeResult.IsError, "storage write failed: %s", writeResult.Content[0].(*mcp.TextContent).Text)
	t.Logf("Write result: %s", writeResult.Content[0].(*mcp.TextContent).Text)

	// 5. Read back via raw API and verify.
	updatedPage, err := env.client.GetPage(ctx, pageID)
	require.NoError(t, err)
	assert.Greater(t, updatedPage.Version.Number, original.Version.Number)
	assert.Contains(t, updatedPage.Body.Storage.Value, "Storage format smoke test",
		"marker should be in the updated page")

	// 6. Verify macros survived the storage-format round-trip.
	originalMacroCount := strings.Count(original.Body.Storage.Value, "<ac:structured-macro")
	updatedMacroCount := strings.Count(updatedPage.Body.Storage.Value, "<ac:structured-macro")
	t.Logf("Macros: original=%d, after storage write=%d", originalMacroCount, updatedMacroCount)
	assert.Equal(t, originalMacroCount, updatedMacroCount,
		"macro count changed after storage-format round-trip")
}

func TestSmoke_DryRunCreate(t *testing.T) {
	cs := newLiveSession(t)
	text := callTool(t, cs, "confluence_write", map[string]any{
		"action":  "create",
		"dry_run": true,
		"items": []any{map[string]any{
			"space_id": "1",
			"title":    "Smoke Test Page",
			"body":     "This page was **not** created — dry run only.",
		}},
	})
	assert.Contains(t, text, "Would create")
}

func TestSmoke_ValidationErrors(t *testing.T) {
	cs := newLiveSession(t)
	ctx := context.Background()

	t.Run("update without version", func(t *testing.T) {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "confluence_write",
			Arguments: map[string]any{
				"action": "update",
				"items":  []any{map[string]any{"page_id": "123", "title": "No Version"}},
			},
		})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(*mcp.TextContent).Text
		assert.Contains(t, text, "version_number")
	})

	t.Run("invalid action", func(t *testing.T) {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "confluence_write",
			Arguments: map[string]any{
				"action": "bogus",
				"items":  []any{map[string]any{}},
			},
		})
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("no items", func(t *testing.T) {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "confluence_write",
			Arguments: map[string]any{
				"action": "create",
				"items":  []any{},
			},
		})
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestSmoke_ReadPageContainsMacroComments(t *testing.T) {
	env := newLiveEnv(t)
	pageID := os.Getenv("SMOKE_PAGE_ID")
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}

	ctx := context.Background()

	// Read the page via MCP.
	result, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{pageID}},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text

	// The test page should contain macros — verify at least one macro comment.
	if !strings.Contains(text, "<!-- macro:") {
		t.Log("Page content (first 500 chars):", text[:min(500, len(text))])
		t.Fatal("Expected page to contain <!-- macro:mN --> comments")
	}

	// Count macros.
	macroCount := strings.Count(text, "<!-- macro:")
	t.Logf("Found %d macro comments in page %s", macroCount, pageID)
}

func TestSmoke_EditRoundTrip_MacrosPreserved(t *testing.T) {
	env := newLiveEnv(t)
	pageID := os.Getenv("SMOKE_PAGE_ID")
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}

	ctx := context.Background()

	original := snapshotAndRestorePage(t, env, pageID)

	// 2. Read via MCP.
	result, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{pageID}},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	require.Contains(t, text, "<!-- macro:")

	macroCount := strings.Count(text, "<!-- macro:")
	t.Logf("Read %d macros from page", macroCount)

	// 3. Append a marker line (leave macros untouched).
	marker := fmt.Sprintf("\n\nMacro preservation test marker: %d\n", time.Now().Unix())

	// Extract version number from the response header.
	version := extractVersion(text)
	require.NotZero(t, version, "could not extract version number")

	// Extract just the body (skip the header line).
	body := extractPageBody(text)
	modifiedBody := body + marker

	// 4. Write back via MCP.
	writeResult, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items": []any{map[string]any{
				"page_id":        pageID,
				"title":          original.Title,
				"version_number": version,
				"body":           modifiedBody,
			}},
		},
	})
	require.NoError(t, err)
	require.False(t, writeResult.IsError, "write failed: %s", writeResult.Content[0].(*mcp.TextContent).Text)

	// 5. Re-read via raw API and verify macros survive.
	updatedPage, err := env.client.GetPage(ctx, pageID)
	require.NoError(t, err)
	assert.Greater(t, updatedPage.Version.Number, original.Version.Number)

	// Storage format should still contain ac:structured-macro elements.
	assert.Contains(t, updatedPage.Body.Storage.Value, "ac:structured-macro",
		"macros lost after round-trip!")

	// Count macros in storage format (each has open + close tag).
	macrosInStorage := strings.Count(updatedPage.Body.Storage.Value, "<ac:structured-macro")
	originalMacrosInStorage := strings.Count(original.Body.Storage.Value, "<ac:structured-macro")
	t.Logf("Macros: original=%d, after edit=%d", originalMacrosInStorage, macrosInStorage)
	assert.Equal(t, originalMacrosInStorage, macrosInStorage,
		"macro count changed after round-trip")
}

// TestSmoke_Append exercises the append action against a live Confluence page.
// Requires SMOKE_PAGE_ID to be set. Appends a sentinel note to the end of the
// page, reads back, asserts the note is present and macro count is unchanged,
// then restores the original storage body.
func TestSmoke_Append(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	env := newLiveEnv(t)
	original := snapshotAndRestorePage(t, env, pageID)
	originalMacros := strings.Count(original.Body.Storage.Value, "<ac:structured-macro")

	sentinel := fmt.Sprintf("Smoke append sentinel %d", time.Now().UnixNano())

	// Dry-run first.
	dry := callTool(t, env.session, "confluence_write", map[string]any{
		"action":  "append",
		"dry_run": true,
		"items": []any{map[string]any{
			"page_id":  pageID,
			"body":     sentinel,
			"position": "end",
		}},
	})
	assert.Contains(t, dry, "Would append")
	assert.Contains(t, dry, `"position": "end"`)

	// Real append.
	text := callTool(t, env.session, "confluence_write", map[string]any{
		"action": "append",
		"items": []any{map[string]any{
			"page_id":  pageID,
			"body":     sentinel,
			"position": "end",
		}},
	})
	assert.Contains(t, text, "Appended to")

	// Read back via raw client and verify.
	updated, err := env.client.GetPage(context.Background(), pageID)
	require.NoError(t, err)
	assert.Greater(t, updated.Version.Number, original.Version.Number)
	assert.Contains(t, updated.Body.Storage.Value, sentinel, "sentinel missing from updated page")
	updatedMacros := strings.Count(updated.Body.Storage.Value, "<ac:structured-macro")
	assert.Equal(t, originalMacros, updatedMacros, "macro count changed after append")
}

// snapshotAndRestorePage takes a snapshot of a Confluence page and registers a
// t.Cleanup hook that restores the original storage body after the test. Used
// by the append smoke tests so each one runs against an identical starting page.
func snapshotAndRestorePage(t *testing.T, env *liveEnv, pageID string) *confluence.Page {
	t.Helper()
	ctx := context.Background()
	original, err := env.client.GetPage(ctx, pageID)
	require.NoError(t, err, "fetching original page for backup")
	originalStorage := original.Body.Storage.Value
	originalTitle := original.Title
	t.Logf("Backup: version=%d, title=%q, storage=%d bytes",
		original.Version.Number, originalTitle, len(originalStorage))

	t.Cleanup(func() {
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			current, err := env.client.GetPage(ctx, pageID)
			if err != nil {
				continue
			}
			_, err = env.client.UpdatePage(ctx, pageID, map[string]any{
				"id":     pageID,
				"status": "current",
				"title":  originalTitle,
				"version": map[string]any{
					"number": current.Version.Number + 1,
				},
				"body": map[string]any{
					"storage": map[string]any{
						"value":          originalStorage,
						"representation": "storage",
					},
				},
			})
			if err == nil {
				return
			}
		}
		t.Logf("ERROR: could not restore page after 3 attempts")
	})
	return original
}

// TestSmoke_Append_AfterHeading exercises position=after_heading against a
// real heading. This is the most likely place the heading locator could
// disagree with real storage (entities, local-id attrs, nested formatting),
// so we target a heading from the all-elements fixture.
func TestSmoke_Append_AfterHeading(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	env := newLiveEnv(t)
	original := snapshotAndRestorePage(t, env, pageID)

	const heading = "27. Final Verification Notes"
	sentinel := fmt.Sprintf("Smoke after_heading sentinel %d", time.Now().UnixNano())

	text := callTool(t, env.session, "confluence_write", map[string]any{
		"action": "append",
		"items": []any{map[string]any{
			"page_id":  pageID,
			"body":     sentinel,
			"position": "after_heading",
			"heading":  heading,
		}},
	})
	assert.Contains(t, text, "Appended to")

	updated, err := env.client.GetPage(context.Background(), pageID)
	require.NoError(t, err)
	assert.Greater(t, updated.Version.Number, original.Version.Number)

	storage := updated.Body.Storage.Value
	assert.Contains(t, storage, sentinel)
	assert.Contains(t, storage, heading)
	// Sentinel must appear after the target heading but before the next h2.
	sentinelIdx := strings.Index(storage, sentinel)
	headingIdx := strings.Index(storage, heading)
	nextHeadingIdx := strings.Index(storage, "28. Additional Headings")
	require.Positive(t, sentinelIdx, "sentinel missing")
	require.Positive(t, headingIdx, "target heading missing")
	require.Positive(t, nextHeadingIdx, "next heading missing")
	assert.Greater(t, sentinelIdx, headingIdx, "sentinel should appear after target heading")
	assert.Less(t, sentinelIdx, nextHeadingIdx, "sentinel should appear before next h2")
}

// TestSmoke_Append_ReplaceSection exercises position=replace_section. Replaces
// content under a known heading, asserts the old content is gone, the fragment
// is present, and the next heading survives (no cross-layout-boundary walk).
func TestSmoke_Append_ReplaceSection(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	env := newLiveEnv(t)
	original := snapshotAndRestorePage(t, env, pageID)

	const heading = "27. Final Verification Notes"
	const oldContentMarker = "Check that real Confluence macros (TOC, Jira, excerpt"
	require.Contains(t, original.Body.Storage.Value, oldContentMarker,
		"fixture page unexpectedly missing reference content — test cannot verify replace")

	sentinel := fmt.Sprintf("Smoke replace_section sentinel %d", time.Now().UnixNano())
	// Include the target heading at the top of the fragment to exercise the
	// server-side strip — the merged body must still contain exactly one copy.
	body := fmt.Sprintf("## %s\n\n%s", heading, sentinel)

	text := callTool(t, env.session, "confluence_write", map[string]any{
		"action": "append",
		"items": []any{map[string]any{
			"page_id":  pageID,
			"body":     body,
			"position": "replace_section",
			"heading":  heading,
		}},
	})
	assert.Contains(t, text, "Appended to")

	updated, err := env.client.GetPage(context.Background(), pageID)
	require.NoError(t, err)

	storage := updated.Body.Storage.Value
	assert.Contains(t, storage, heading, "target heading should still be present")
	assert.Contains(t, storage, sentinel, "sentinel should be present")
	assert.NotContains(t, storage, oldContentMarker,
		"old content under section 27 should have been replaced")
	assert.Contains(t, storage, "28. Additional Headings",
		"next heading should survive — we must not cross the layout-cell boundary")
	assert.Equal(t, 1, strings.Count(storage, heading),
		"target heading should appear exactly once — fragment-leading duplicate should be stripped")
}

// TestSmoke_Append_StorageMacro exercises appending a macro via format=storage.
// Markdown does not synthesise new macros on write (GFM alerts are a read-only
// projection of existing macros), so adding a macro requires raw storage XHTML.
// Verifies the macro lands on the real page and macro count rises by exactly 1.
func TestSmoke_Append_StorageMacro(t *testing.T) {
	pageID := smokePageID()
	if pageID == "" {
		t.Skip("SMOKE_PAGE_ID not set")
	}
	env := newLiveEnv(t)
	original := snapshotAndRestorePage(t, env, pageID)
	originalMacros := strings.Count(original.Body.Storage.Value, "<ac:structured-macro")

	sentinel := fmt.Sprintf("Smoke storage-macro sentinel %d", time.Now().UnixNano())
	fragment := fmt.Sprintf(
		`<ac:structured-macro ac:name="note" ac:schema-version="1"><ac:rich-text-body><p>%s</p></ac:rich-text-body></ac:structured-macro>`,
		sentinel,
	)

	text := callTool(t, env.session, "confluence_write", map[string]any{
		"action": "append",
		"items": []any{map[string]any{
			"page_id":  pageID,
			"body":     fragment,
			"format":   "storage",
			"position": "end",
		}},
	})
	assert.Contains(t, text, "Appended to")

	updated, err := env.client.GetPage(context.Background(), pageID)
	require.NoError(t, err)

	storage := updated.Body.Storage.Value
	assert.Contains(t, storage, sentinel, "sentinel missing from updated page")
	updatedMacros := strings.Count(storage, "<ac:structured-macro")
	assert.Equal(t, originalMacros+1, updatedMacros,
		"expected macro count to increase by 1, got %d -> %d", originalMacros, updatedMacros)
	assert.Contains(t, storage, `ac:name="note"`, "storage should contain the appended note macro")
}
