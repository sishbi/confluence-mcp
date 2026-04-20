package confluencemcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/confluencemcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubConfluence returns an httptest.Server that simulates the Confluence Cloud API.
// It maintains a minimal in-memory state for pages, comments, and labels.
func stubConfluence(t *testing.T) *httptest.Server {
	t.Helper()

	pages := map[string]confluence.Page{
		"101": {
			ID: "101", Title: "Architecture Overview", SpaceID: "1", Status: "current",
			Version: confluence.PageVersion{Number: 3},
			Body: confluence.PageBody{Storage: confluence.StorageBody{
				Representation: "storage",
				Value:          "<h1>Architecture</h1><p>This document describes the <strong>system architecture</strong> including all components.</p><h2>Backend</h2><p>The backend uses Go with a REST API.</p><h2>Frontend</h2><p>The frontend is a React SPA.</p>",
			}},
		},
		"102": {
			ID: "102", Title: "API Reference", SpaceID: "1", Status: "current",
			Version: confluence.PageVersion{Number: 1},
			Body: confluence.PageBody{Storage: confluence.StorageBody{
				Representation: "storage",
				Value:          "<h1>API Reference</h1><p>Endpoints listed below.</p><h2>GET /users</h2><p>Returns all users.</p><h2>POST /users</h2><p>Creates a new user.</p>",
			}},
		},
		"103": {
			ID: "103", Title: "Meeting Notes", SpaceID: "1", Status: "current",
			Version: confluence.PageVersion{Number: 2},
			Body: confluence.PageBody{Storage: confluence.StorageBody{
				Representation: "storage",
				Value: `<h1>Meeting Notes</h1>` +
					`<ac:structured-macro ac:name="toc"></ac:structured-macro>` +
					`<h2>Agenda</h2>` +
					`<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Review Q2 goals.</p></ac:rich-text-body></ac:structured-macro>` +
					`<p>Regular discussion points.</p>` +
					`<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Action Items</ac:parameter><ac:rich-text-body><p>Follow up with team.</p></ac:rich-text-body></ac:structured-macro>` +
					`<ac:structured-macro ac:name="status"><ac:parameter ac:name="title">In Progress</ac:parameter><ac:parameter ac:name="colour">Yellow</ac:parameter></ac:structured-macro>`,
			}},
		},
	}

	comments := map[string]confluence.Comment{
		"c1": {
			ID: "c1", PageID: "101",
			Version: confluence.CommentVersion{Number: 1},
			Body: confluence.PageBody{Storage: confluence.StorageBody{
				Value: "<p>Looks good, but needs a diagram.</p>",
			}},
		},
	}

	labels := map[string][]confluence.Label{
		"101": {{ID: "l1", Name: "architecture", Prefix: "global"}, {ID: "l2", Name: "approved", Prefix: "global"}},
	}

	nextPageID := 200

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		// Current user
		case path == "/wiki/rest/api/user/current":
			_ = json.NewEncoder(w).Encode(confluence.User{
				AccountID: "user-001", DisplayName: "Test User", Email: "test@example.com",
			})

		// Spaces
		case path == "/wiki/api/v2/spaces":
			_ = json.NewEncoder(w).Encode(confluence.PaginatedResponse[confluence.Space]{
				Results: []confluence.Space{
					{ID: "1", Key: "ENG", Name: "Engineering", Type: "global"},
					{ID: "2", Key: "OPS", Name: "Operations", Type: "global"},
				},
			})

		// Get page by ID
		case strings.HasPrefix(path, "/wiki/api/v2/pages/") && !strings.Contains(path, "/children") && !strings.Contains(path, "/footer-comments") && !strings.Contains(path, "/labels") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(path, "/wiki/api/v2/pages/")
			if page, ok := pages[id]; ok {
				_ = json.NewEncoder(w).Encode(page)
			} else {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"page not found"}`))
			}

		// Create page
		case path == "/wiki/api/v2/pages" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			nextPageID++
			id := fmt.Sprintf("%d", nextPageID)
			page := confluence.Page{
				ID:      id,
				Title:   body["title"].(string),
				Version: confluence.PageVersion{Number: 1},
			}
			pages[id] = page
			_ = json.NewEncoder(w).Encode(page)

		// Update page
		case strings.HasPrefix(path, "/wiki/api/v2/pages/") && r.Method == http.MethodPut:
			id := strings.TrimPrefix(path, "/wiki/api/v2/pages/")
			if page, ok := pages[id]; ok {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if title, ok := body["title"].(string); ok {
					page.Title = title
				}
				page.Version.Number++
				pages[id] = page
				_ = json.NewEncoder(w).Encode(page)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}

		// Delete page
		case strings.HasPrefix(path, "/wiki/api/v2/pages/") && r.Method == http.MethodDelete && !strings.Contains(path, "/labels/"):
			id := strings.TrimPrefix(path, "/wiki/api/v2/pages/")
			delete(pages, id)
			w.WriteHeader(http.StatusNoContent)

		// Page children
		case strings.HasSuffix(path, "/children"):
			_ = json.NewEncoder(w).Encode(confluence.PaginatedResponse[confluence.Page]{
				Results: []confluence.Page{{ID: "102", Title: "API Reference"}},
			})

		// Footer comments — list
		case strings.HasSuffix(path, "/footer-comments") && r.Method == http.MethodGet:
			pageID := strings.Split(strings.TrimPrefix(path, "/wiki/api/v2/pages/"), "/")[0]
			var result []confluence.Comment
			for _, c := range comments {
				if c.PageID == pageID {
					result = append(result, c)
				}
			}
			_ = json.NewEncoder(w).Encode(confluence.PaginatedResponse[confluence.Comment]{Results: result})

		// Footer comments — add (v2: POST /wiki/api/v2/footer-comments)
		case path == "/wiki/api/v2/footer-comments" && r.Method == http.MethodPost:
			comment := confluence.Comment{ID: "c-new", Version: confluence.CommentVersion{Number: 1}}
			_ = json.NewEncoder(w).Encode(comment)

		// Single comment
		case strings.HasPrefix(path, "/wiki/api/v2/footer-comments/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(path, "/wiki/api/v2/footer-comments/")
			if c, ok := comments[id]; ok {
				_ = json.NewEncoder(w).Encode(c)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}

		// Labels — list
		case strings.HasSuffix(path, "/labels") && r.Method == http.MethodGet:
			pageID := strings.Split(strings.TrimPrefix(path, "/wiki/api/v2/pages/"), "/")[0]
			_ = json.NewEncoder(w).Encode(confluence.PaginatedResponse[confluence.Label]{
				Results: labels[pageID],
			})

		// Labels — add (v1: POST /wiki/rest/api/content/{id}/label)
		case strings.HasSuffix(path, "/label") && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []confluence.Label{{ID: "l-new", Name: "new-label"}},
			})

		// Labels — remove (v1: DELETE /wiki/rest/api/content/{id}/label/{name})
		case strings.Contains(path, "/label/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)

		// CQL search (v1)
		case path == "/wiki/rest/api/search":
			cql := r.URL.Query().Get("cql")
			var results []confluence.SearchResultItem
			for _, p := range pages {
				if strings.Contains(strings.ToLower(p.Title), "architecture") && strings.Contains(cql, "architecture") ||
					cql == "type=page" {
					results = append(results, confluence.SearchResultItem{
						Title:   p.Title,
						Content: confluence.SearchContent{ID: p.ID, Type: "page", Title: p.Title},
					})
				}
			}
			_ = json.NewEncoder(w).Encode(confluence.SearchResult{
				Results:   results,
				TotalSize: len(results),
			})

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unknown endpoint: ` + path + `"}`))
		}
	}))
}

// newIntegrationServer creates a real MCP server backed by a stub Confluence API.
func newIntegrationServer(t *testing.T) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()

	stub := stubConfluence(t)

	client, err := confluence.New(confluence.Config{
		URL:        stub.URL,
		Email:      "test@example.com",
		APIToken:   "test-token",
		MaxRetries: 0,
		BaseDelay:  time.Millisecond,
	})
	require.NoError(t, err)

	user, err := client.GetCurrentUser(ctx)
	require.NoError(t, err)

	server := confluencemcp.NewServer(client, user, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, t1, nil)
	require.NoError(t, err)

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "v0.0.1"}, nil)
	session, err := mcpClient.Connect(ctx, t2, nil)
	require.NoError(t, err)

	return session, func() {
		_ = session.Close()
		stub.Close()
	}
}

func TestIntegration_ListTools(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, res.Tools, 2)

	names := make(map[string]bool)
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	assert.True(t, names["confluence_read"])
	assert.True(t, names["confluence_write"])
}

func TestIntegration_Instructions(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	init := cs.InitializeResult()
	require.NotNil(t, init)
	assert.Contains(t, init.Instructions, "Test User")
	assert.Contains(t, init.Instructions, "user-001")
	assert.Contains(t, init.Instructions, "confluence_read")
}

func TestIntegration_ReadSpaces(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"resource": "spaces"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "ENG")
	assert.Contains(t, text, "Engineering")
	assert.Contains(t, text, "OPS")
}

func TestIntegration_ReadPageByID(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{"101"}},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	// Should contain converted Markdown, not raw HTML
	assert.Contains(t, text, "Architecture")
	assert.Contains(t, text, "system architecture")
	assert.Contains(t, text, "Backend")
	assert.Contains(t, text, "Frontend")
	// Page ID should be in the output for reference
	assert.Contains(t, text, "101")
}

func TestIntegration_ReadPageByURL(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"url": "https://company.atlassian.net/wiki/spaces/ENG/pages/101/Architecture+Overview"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Architecture")
	assert.Contains(t, text, "Backend")
}

func TestIntegration_ReadPageByURL_WithComment(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"url": "https://company.atlassian.net/wiki/spaces/ENG/pages/101/Title?focusedCommentId=c1"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	// Both page and comment should appear
	assert.Contains(t, text, "diagram")    // from the comment
	assert.Contains(t, text, "Architecture") // from the page
}

func TestIntegration_SearchCQL(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"cql": "type=page"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Architecture Overview")
}

func TestIntegration_ReadChildren(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"resource": "children", "page_id": "101"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "API Reference")
}

func TestIntegration_ReadComments(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"resource": "comments", "page_id": "101"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "diagram")
}

func TestIntegration_ReadLabels(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"resource": "labels", "page_id": "101"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "architecture")
	assert.Contains(t, text, "approved")
}

func TestIntegration_WriteDryRun(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action":  "create",
			"dry_run": true,
			"items": []any{map[string]any{
				"space_id": "1",
				"title":    "New Page",
				"body":     "Hello **world**",
			}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Would create")
	assert.Contains(t, text, "New Page")
}

func TestIntegration_WriteCreatePage(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	// Create a page
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "create",
			"items": []any{map[string]any{
				"space_id": "1",
				"title":    "Integration Test Page",
				"body":     "Created during **integration test**",
			}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Created")
}

func TestIntegration_WriteUpdatePage(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items": []any{map[string]any{
				"page_id":        "101",
				"title":          "Architecture Overview v2",
				"version_number": 3,
			}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestIntegration_WriteDeletePage(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "delete",
			"items":  []any{map[string]any{"page_id": "102"}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestIntegration_WriteAddComment(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "comment",
			"items": []any{map[string]any{
				"page_id": "101",
				"body":    "Great work on this page!",
			}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestIntegration_WriteAddLabel(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "add_label",
			"items":  []any{map[string]any{"page_id": "101", "label": "reviewed"}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestIntegration_WriteRemoveLabel(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "remove_label",
			"items":  []any{map[string]any{"page_id": "101", "label": "architecture"}},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestIntegration_ReadThenSectionExtract(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	// First read: fetches the page and caches it
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{"101"}},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Second read: extract a section from cache
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_id": "101", "section": "Backend"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Go")
	assert.Contains(t, text, "REST API")
	assert.NotContains(t, text, "React") // Frontend section should not be included
}

func TestIntegration_ReadNotFound(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{"999"}},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestIntegration_WriteValidationError(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()

	// Missing version_number for update
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items":  []any{map[string]any{"page_id": "101", "title": "Updated"}},
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestIntegration_ReadPageWithMacros(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{"103"}},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(*mcp.TextContent).Text

	// Verify macro comments are present.
	assert.Contains(t, text, "<!-- macro:m1 -->") // toc
	assert.Contains(t, text, "<!-- macro:m2 -->") // info
	assert.Contains(t, text, "<!-- macro:m3 -->") // expand
	assert.Contains(t, text, "<!-- macro:m4 -->") // status

	// Verify human-readable representations.
	assert.Contains(t, text, "Table of Contents")
	assert.Contains(t, text, "Review Q2 goals.")
	assert.Contains(t, text, "Action Items")
	assert.Contains(t, text, "🟡 In Progress")
}

func TestIntegration_WritePreservesMacros(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	// First read to populate cache.
	_, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"page_ids": []any{"103"}},
	})
	require.NoError(t, err)

	// Update with modified text but macros intact.
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items": []any{map[string]any{
				"page_id":        "103",
				"title":          "Meeting Notes",
				"version_number": 2,
				"body":           "# Meeting Notes\n\n<!-- macro:m1 --> [Table of Contents]\n\n## Agenda\n\n<!-- macro:m2 -->\n> **Info:** Updated Q2 goals.\n\nUpdated discussion points.\n\n<!-- macro:m3 -->\n<details><summary>Action Items</summary><p>Follow up with team.</p></details>\n\n<!-- macro:m4 --> **Status: In Progress**\n",
			}},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Updated page")
}

func TestIntegration_ReadStorageFormat(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_read",
		Arguments: map[string]any{
			"page_ids": []any{"101"},
			"format":   "storage",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(*mcp.TextContent).Text

	// Should contain raw HTML tags, not Markdown
	assert.Contains(t, text, "<h1>")
	assert.Contains(t, text, "<strong>")
	assert.NotContains(t, text, "# Architecture") // no Markdown headings
}

func TestIntegration_WriteStorageFormat(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action": "update",
			"items": []any{map[string]any{
				"page_id":        "101",
				"title":          "Architecture Overview",
				"version_number": 3,
				"format":         "storage",
				"body":           `<h1>Architecture</h1><p>Updated directly in storage format.</p>`,
			}},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Updated page")
}

func TestIntegration_ReadStorageFormat_WithMacros(t *testing.T) {
	cs, cleanup := newIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_read",
		Arguments: map[string]any{
			"page_ids": []any{"103"},
			"format":   "storage",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(*mcp.TextContent).Text

	// Raw storage format should contain actual macro XML
	assert.Contains(t, text, `ac:structured-macro`)
	assert.Contains(t, text, `ac:name="toc"`)
	assert.Contains(t, text, `ac:name="info"`)
	// Should NOT contain macro comments (those are only in Markdown mode)
	assert.NotContains(t, text, "<!-- macro:")
}
