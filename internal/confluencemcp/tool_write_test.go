package confluencemcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sishbi/confluence-mcp/internal/confluence"
)

func TestHandleWrite_CreatePage(t *testing.T) {
	var capturedPayload map[string]any
	h := &handlers{
		client: &mockClient{
			CreatePageFn: func(ctx context.Context, payload map[string]any) (*confluence.Page, error) {
				capturedPayload = payload
				return &confluence.Page{ID: "999", Title: "New Page"}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "create",
		Items: []WriteItem{
			{SpaceID: "~space1", Title: "New Page", Body: "Hello world"},
		},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "999")

	// Verify payload fields
	assert.Equal(t, "~space1", capturedPayload["spaceId"])
	assert.Equal(t, "New Page", capturedPayload["title"])
	body, ok := capturedPayload["body"].(map[string]any)
	assert.True(t, ok)
	storage, ok := body["storage"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "storage", storage["representation"])
}

func TestHandleWrite_CreatePage_DryRun(t *testing.T) {
	h := &handlers{
		// CreatePageFn intentionally NOT set — should not be called
		client: &mockClient{},
	}

	args := WriteArgs{
		Action: "create",
		Items: []WriteItem{
			{SpaceID: "~space1", Title: "Draft Page", Body: "Draft content"},
		},
		DryRun: true,
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "Would create")
}

func TestHandleWrite_UpdatePage(t *testing.T) {
	var capturedID string
	var capturedPayload map[string]any
	h := &handlers{
		client: &mockClient{
			UpdatePageFn: func(ctx context.Context, id string, payload map[string]any) (*confluence.Page, error) {
				capturedID = id
				capturedPayload = payload
				return &confluence.Page{ID: "42", Title: "Updated Page"}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "update",
		Items: []WriteItem{
			{PageID: "42", Title: "Updated Page", Body: "New content", VersionNumber: 3},
		},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "42")

	assert.Equal(t, "42", capturedID)
	version, ok := capturedPayload["version"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 4, version["number"]) // version + 1
}

func TestHandleWrite_UpdatePage_MissingVersion(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := WriteArgs{
		Action: "update",
		Items: []WriteItem{
			{PageID: "42", Title: "Updated Page", Body: "New content"},
			// VersionNumber is 0 (missing)
		},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "version_number")
}

func TestHandleWrite_DeletePage(t *testing.T) {
	var deletedID string
	h := &handlers{
		client: &mockClient{
			DeletePageFn: func(ctx context.Context, id string) error {
				deletedID = id
				return nil
			},
		},
	}

	args := WriteArgs{
		Action: "delete",
		Items:  []WriteItem{{PageID: "77"}},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "77", deletedID)
	text := firstText(t, result)
	assert.Contains(t, text, "77")
}

func TestHandleWrite_AddComment(t *testing.T) {
	var capturedBody string
	h := &handlers{
		client: &mockClient{
			AddCommentFn: func(ctx context.Context, pageID string, body string) (*confluence.Comment, error) {
				capturedBody = body
				return &confluence.Comment{ID: "555", PageID: pageID}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "comment",
		Items:  []WriteItem{{PageID: "100", Body: "Great page!"}},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	// body should be converted to storage format (contains <p>)
	assert.Contains(t, capturedBody, "<p>")
	text := firstText(t, result)
	assert.Contains(t, text, "555")
}

func TestHandleWrite_EditComment(t *testing.T) {
	var capturedCommentID string
	var capturedVersion int
	h := &handlers{
		client: &mockClient{
			UpdateCommentFn: func(ctx context.Context, commentID string, body string, versionNumber int) (*confluence.Comment, error) {
				capturedCommentID = commentID
				capturedVersion = versionNumber
				return &confluence.Comment{ID: commentID}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "edit_comment",
		Items:  []WriteItem{{CommentID: "888", Body: "Updated comment", VersionNumber: 2}},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "888", capturedCommentID)
	assert.Equal(t, 3, capturedVersion) // version + 1
}

func TestHandleWrite_AddLabel(t *testing.T) {
	var capturedPageID, capturedLabel string
	h := &handlers{
		client: &mockClient{
			AddPageLabelFn: func(ctx context.Context, pageID string, label string) (*confluence.Label, error) {
				capturedPageID = pageID
				capturedLabel = label
				return &confluence.Label{ID: "L1", Name: label}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "add_label",
		Items:  []WriteItem{{PageID: "200", Label: "important"}},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "200", capturedPageID)
	assert.Equal(t, "important", capturedLabel)
}

func TestHandleWrite_RemoveLabel(t *testing.T) {
	var capturedPageID, capturedLabel string
	h := &handlers{
		client: &mockClient{
			RemovePageLabelFn: func(ctx context.Context, pageID string, label string) error {
				capturedPageID = pageID
				capturedLabel = label
				return nil
			},
		},
	}

	args := WriteArgs{
		Action: "remove_label",
		Items:  []WriteItem{{PageID: "300", Label: "outdated"}},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "300", capturedPageID)
	assert.Equal(t, "outdated", capturedLabel)
}

func TestHandleWrite_BatchItems(t *testing.T) {
	callCount := 0
	h := &handlers{
		client: &mockClient{
			CreatePageFn: func(ctx context.Context, payload map[string]any) (*confluence.Page, error) {
				callCount++
				return &confluence.Page{ID: "100", Title: payload["title"].(string)}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "create",
		Items: []WriteItem{
			{SpaceID: "~s", Title: "Page One"},
			{SpaceID: "~s", Title: "Page Two"},
			{SpaceID: "~s", Title: "Page Three"},
		},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, 3, callCount)
	text := firstText(t, result)
	assert.Contains(t, text, "[1]")
	assert.Contains(t, text, "[2]")
	assert.Contains(t, text, "[3]")
}

func TestHandleWrite_EmptyItems(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := WriteArgs{
		Action: "create",
		Items:  []WriteItem{},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleWrite_InvalidAction(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	args := WriteArgs{
		Action: "fly_to_moon",
		Items:  []WriteItem{{PageID: "1"}},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestWriteUpdate_PreservesMacros(t *testing.T) {
	var capturedPayload map[string]any

	h := &handlers{
		client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:    id,
					Title: "Test",
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: `<p>Text.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Original note.</p></ac:rich-text-body></ac:structured-macro>`,
					}},
					Version: confluence.PageVersion{Number: 5},
				}, nil
			},
			UpdatePageFn: func(_ context.Context, id string, payload map[string]any) (*confluence.Page, error) {
				capturedPayload = payload
				return &confluence.Page{ID: id, Title: "Test"}, nil
			},
		},
	}

	// Read the page first to populate the cache with registry.
	page, _ := h.client.GetPage(context.Background(), "p1")
	_ = h.processPage(context.Background(), page)

	// Update with markdown that includes macro comments.
	_, err := h.writeUpdate(context.Background(), WriteItem{
		PageID:        "p1",
		Title:         "Test",
		Body:          "Updated text.\n\n<!-- macro:m1 -->\n> **Info:** Updated note.\n",
		VersionNumber: 5,
	}, false)
	require.NoError(t, err)

	// The payload body should contain the restored macro XML.
	body := capturedPayload["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	value := storage["value"].(string)

	assert.Contains(t, value, `ac:name="info"`)
	assert.Contains(t, value, "Updated note.")
	assert.Contains(t, value, "Updated text.")
}

func TestWriteUpdate_RegistryRefresh(t *testing.T) {
	getPageCalls := 0

	h := &handlers{
		client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				getPageCalls++
				return &confluence.Page{
					ID:    id,
					Title: "Test",
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: `<ac:structured-macro ac:name="toc"></ac:structured-macro><p>Content.</p>`,
					}},
					Version: confluence.PageVersion{Number: 3},
				}, nil
			},
			UpdatePageFn: func(_ context.Context, id string, payload map[string]any) (*confluence.Page, error) {
				return &confluence.Page{ID: id, Title: "Test"}, nil
			},
		},
	}

	// Don't read the page first — cache is empty.
	_, err := h.writeUpdate(context.Background(), WriteItem{
		PageID:        "p1",
		Title:         "Test",
		Body:          "<!-- macro:m1 --> *[Table of Contents]*\n\nUpdated content.\n",
		VersionNumber: 3,
	}, false)
	require.NoError(t, err)

	// Should have called GetPage once for the registry refresh.
	assert.Equal(t, 1, getPageCalls, "expected registry refresh to call GetPage")
}

func TestWriteUpdate_StorageFormat(t *testing.T) {
	var capturedPayload map[string]any

	h := &handlers{
		client: &mockClient{
			UpdatePageFn: func(_ context.Context, id string, payload map[string]any) (*confluence.Page, error) {
				capturedPayload = payload
				return &confluence.Page{ID: id, Title: "Test"}, nil
			},
		},
	}

	_, err := h.writeUpdate(context.Background(), WriteItem{
		PageID:        "p1",
		Title:         "Test",
		Body:          `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Direct XHTML.</p></ac:rich-text-body></ac:structured-macro>`,
		Format:        "storage",
		VersionNumber: 5,
	}, false)
	require.NoError(t, err)

	body := capturedPayload["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	value := storage["value"].(string)

	// Body should be passed through verbatim, not converted
	assert.Contains(t, value, `ac:name="info"`)
	assert.Contains(t, value, "Direct XHTML.")
	assert.NotContains(t, value, "<p><p>") // no double-wrapping from ToStorageFormat
}

func TestWriteCreate_StorageFormat(t *testing.T) {
	var capturedPayload map[string]any

	h := &handlers{
		client: &mockClient{
			CreatePageFn: func(_ context.Context, payload map[string]any) (*confluence.Page, error) {
				capturedPayload = payload
				return &confluence.Page{ID: "new1", Title: "Test"}, nil
			},
		},
	}

	_, err := h.writeCreate(context.Background(), WriteItem{
		SpaceID: "1",
		Title:   "Test",
		Body:    `<p>Raw XHTML body.</p>`,
		Format:  "storage",
	}, false)
	require.NoError(t, err)

	body := capturedPayload["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	value := storage["value"].(string)

	assert.Equal(t, `<p>Raw XHTML body.</p>`, value)
}

func TestHandleWrite_DispatchesAppend(t *testing.T) {
	var capturedPayload map[string]any
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:      id,
					Title:   "Test",
					Version: confluence.PageVersion{Number: 2},
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>orig</p></ac:layout-cell></ac:layout-section></ac:layout>`,
					}},
				}, nil
			},
			UpdatePageFn: func(_ context.Context, id string, payload map[string]any) (*confluence.Page, error) {
				capturedPayload = payload
				return &confluence.Page{ID: id, Title: "Test"}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "append",
		Items: []WriteItem{
			{PageID: "p1", Body: "A new note.", Position: "end"},
		},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)

	body := capturedPayload["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	value := storage["value"].(string)
	assert.Contains(t, value, "A new note.")
	assert.Contains(t, value, "orig") // original preserved
}

func TestAppend_RetriesOn409_WhenVersionNotPinned(t *testing.T) {
	const body = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>orig</p></ac:layout-cell></ac:layout-section></ac:layout>`

	getCalls, updateCalls := 0, 0
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				getCalls++
				// First GET returns stale version 5; second GET (after 409)
				// returns the true current version 7.
				ver := 5
				if getCalls >= 2 {
					ver = 7
				}
				return &confluence.Page{
					ID:      id,
					Title:   "Test",
					Version: confluence.PageVersion{Number: ver},
					Body:    confluence.PageBody{Storage: confluence.StorageBody{Value: body}},
				}, nil
			},
			UpdatePageFn: func(_ context.Context, id string, _ map[string]any) (*confluence.Page, error) {
				updateCalls++
				if updateCalls == 1 {
					return nil, &confluence.APIError{StatusCode: 409, Body: "StaleStateException"}
				}
				return &confluence.Page{ID: id, Title: "Test"}, nil
			},
		},
	}

	msg, err := h.writeAppend(context.Background(), WriteItem{
		PageID:   "p1",
		Body:     "A new line.",
		Position: "end",
	}, false)
	require.NoError(t, err)
	assert.Contains(t, msg, "Appended to")
	assert.Equal(t, 2, getCalls, "expected a re-fetch after 409")
	assert.Equal(t, 2, updateCalls, "expected a retry PUT after 409")
}

func TestAppend_NoRetryWhenVersionPinned(t *testing.T) {
	updateCalls := 0
	h := &handlers{
		client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID: id, Title: "Test",
					Version: confluence.PageVersion{Number: 5},
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>orig</p></ac:layout-cell></ac:layout-section></ac:layout>`,
					}},
				}, nil
			},
			UpdatePageFn: func(_ context.Context, id string, _ map[string]any) (*confluence.Page, error) {
				updateCalls++
				return nil, &confluence.APIError{StatusCode: 409, Body: "StaleStateException"}
			},
		},
	}

	_, err := h.writeAppend(context.Background(), WriteItem{
		PageID:        "p1",
		Body:          "A new line.",
		Position:      "end",
		VersionNumber: 5, // caller pinned the version — surface the 409, no retry
	}, false)
	assert.Error(t, err)
	assert.Equal(t, 1, updateCalls, "pinned version must not retry on 409")
}

func TestWriteTool_DescriptionMentionsAppend(t *testing.T) {
	desc := writeTool.Description
	assert.Contains(t, desc, "append:")
	assert.Contains(t, desc, "end")
	assert.Contains(t, desc, "after_heading")
	assert.Contains(t, desc, "replace_section")
}

func TestHandleWrite_CacheEviction(t *testing.T) {
	h := &handlers{
		client: &mockClient{
			UpdatePageFn: func(ctx context.Context, id string, payload map[string]any) (*confluence.Page, error) {
				return &confluence.Page{ID: id, Title: "Updated"}, nil
			},
		},
	}

	// Pre-populate cache
	h.cache.put(&cachedPage{
		pageID:    "42",
		markdown:  "# Old content",
		fetchedAt: time.Now(),
	})

	// Verify it's in cache before the write
	_, ok := h.cache.get("42")
	assert.True(t, ok)

	args := WriteArgs{
		Action: "update",
		Items:  []WriteItem{{PageID: "42", Title: "Updated", VersionNumber: 1}},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	assert.NoError(t, err)
	assert.False(t, result.IsError)

	// Cache should be evicted after write
	_, ok = h.cache.get("42")
	assert.False(t, ok)
}

