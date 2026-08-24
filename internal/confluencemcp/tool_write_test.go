package confluencemcp

import (
	"context"
	"reflect"
	"strings"
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

// TestWriteCreate_RestoresMacrosFromSourcePage pins the Blocker fix: create's
// page_id names the source page whose macro registry to reuse when the body
// carries <!-- macro:mN --> sentinels (e.g. copying a page while preserving
// its macros verbatim). Without page_id reaching ensureMacroRegistry, the
// macro sentinel falls through to plain mdconv.ToStorageFormat and the macro
// XML is silently dropped while the tool still reports success.
func TestWriteCreate_RestoresMacrosFromSourcePage(t *testing.T) {
	var capturedPayload map[string]any

	h := &handlers{
		client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID:    id,
					Title: "Source",
					Body: confluence.PageBody{Storage: confluence.StorageBody{
						Value: `<p>Text.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Original note.</p></ac:rich-text-body></ac:structured-macro>`,
					}},
					Version: confluence.PageVersion{Number: 5},
				}, nil
			},
			CreatePageFn: func(_ context.Context, payload map[string]any) (*confluence.Page, error) {
				capturedPayload = payload
				return &confluence.Page{ID: "new1", Title: "Copy of Source"}, nil
			},
		},
	}

	// Read the source page first to populate the cache with its macro registry.
	page, _ := h.client.GetPage(context.Background(), "A")
	_ = h.processPage(context.Background(), page)

	_, err := h.writeCreate(context.Background(), WriteItem{
		SpaceID: "DEV",
		Title:   "Copy of Source",
		Body:    "Text.\n\n<!-- macro:m1 -->\n> **Info:** Original note.\n",
		PageID:  "A",
	}, false)
	require.NoError(t, err)

	body := capturedPayload["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	value := storage["value"].(string)

	assert.Contains(t, value, `ac:name="info"`, "macro XML must be restored verbatim, not dropped")
	assert.Contains(t, value, "Original note.")
	assert.Contains(t, value, "Text.")
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

func TestWriteDelete_ErrorPaths(t *testing.T) {
	t.Run("page_id required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeDelete(context.Background(), WriteItem{}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page_id")
	})
	t.Run("dry_run skips API call", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		msg, err := h.writeDelete(context.Background(), WriteItem{PageID: "p1"}, true)
		require.NoError(t, err)
		assert.Contains(t, msg, "Would delete")
	})
	t.Run("client error surfaced", func(t *testing.T) {
		h := &handlers{client: &mockClient{
			DeletePageFn: func(_ context.Context, _ string) error { return assert.AnError },
		}}
		_, err := h.writeDelete(context.Background(), WriteItem{PageID: "p1"}, false)
		require.Error(t, err)
	})
}

func TestWriteComment_ErrorPaths(t *testing.T) {
	t.Run("page_id required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeComment(context.Background(), WriteItem{Body: "hi"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page_id")
	})
	t.Run("body required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeComment(context.Background(), WriteItem{PageID: "p1"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "body")
	})
	t.Run("dry_run skips API call", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		msg, err := h.writeComment(context.Background(), WriteItem{PageID: "p1", Body: "hi"}, true)
		require.NoError(t, err)
		assert.Contains(t, msg, "Would add comment")
	})
	t.Run("client error surfaced", func(t *testing.T) {
		h := &handlers{client: &mockClient{
			AddCommentFn: func(_ context.Context, _ string, _ string) (*confluence.Comment, error) {
				return nil, assert.AnError
			},
		}}
		_, err := h.writeComment(context.Background(), WriteItem{PageID: "p1", Body: "hi"}, false)
		require.Error(t, err)
	})
}

func TestWriteEditComment_ErrorPaths(t *testing.T) {
	t.Run("comment_id required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeEditComment(context.Background(), WriteItem{Body: "x", VersionNumber: 1}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "comment_id")
	})
	t.Run("version_number required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeEditComment(context.Background(), WriteItem{CommentID: "c1", Body: "x"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "version_number")
	})
	t.Run("dry_run skips API call", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		msg, err := h.writeEditComment(context.Background(), WriteItem{CommentID: "c1", Body: "x", VersionNumber: 2}, true)
		require.NoError(t, err)
		assert.Contains(t, msg, "Would update comment")
		assert.Contains(t, msg, "version 3")
	})
	t.Run("client error surfaced", func(t *testing.T) {
		h := &handlers{client: &mockClient{
			UpdateCommentFn: func(_ context.Context, _ string, _ string, _ int) (*confluence.Comment, error) {
				return nil, assert.AnError
			},
		}}
		_, err := h.writeEditComment(context.Background(), WriteItem{CommentID: "c1", Body: "x", VersionNumber: 2}, false)
		require.Error(t, err)
	})
}

func TestWriteAddLabel_ErrorPaths(t *testing.T) {
	t.Run("page_id required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeAddLabel(context.Background(), WriteItem{Label: "x"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page_id")
	})
	t.Run("label required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeAddLabel(context.Background(), WriteItem{PageID: "p1"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "label")
	})
	t.Run("dry_run skips API call", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		msg, err := h.writeAddLabel(context.Background(), WriteItem{PageID: "p1", Label: "x"}, true)
		require.NoError(t, err)
		assert.Contains(t, msg, "Would add label")
	})
	t.Run("client error surfaced", func(t *testing.T) {
		h := &handlers{client: &mockClient{
			AddPageLabelFn: func(_ context.Context, _ string, _ string) (*confluence.Label, error) {
				return nil, assert.AnError
			},
		}}
		_, err := h.writeAddLabel(context.Background(), WriteItem{PageID: "p1", Label: "x"}, false)
		require.Error(t, err)
	})
}

func TestWriteRemoveLabel_ErrorPaths(t *testing.T) {
	t.Run("page_id required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeRemoveLabel(context.Background(), WriteItem{Label: "x"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page_id")
	})
	t.Run("label required", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeRemoveLabel(context.Background(), WriteItem{PageID: "p1"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "label")
	})
	t.Run("dry_run skips API call", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		msg, err := h.writeRemoveLabel(context.Background(), WriteItem{PageID: "p1", Label: "x"}, true)
		require.NoError(t, err)
		assert.Contains(t, msg, "Would remove label")
	})
	t.Run("client error surfaced", func(t *testing.T) {
		h := &handlers{client: &mockClient{
			RemovePageLabelFn: func(_ context.Context, _ string, _ string) error { return assert.AnError },
		}}
		_, err := h.writeRemoveLabel(context.Background(), WriteItem{PageID: "p1", Label: "x"}, false)
		require.Error(t, err)
	})
}

func TestWriteCreate_ClientError(t *testing.T) {
	h := &handlers{client: &mockClient{
		CreatePageFn: func(_ context.Context, _ map[string]any) (*confluence.Page, error) {
			return nil, assert.AnError
		},
	}}
	_, err := h.writeCreate(context.Background(), WriteItem{SpaceID: "s1", Title: "T"}, false)
	require.Error(t, err)
}

func TestWriteUpdate_ClientError(t *testing.T) {
	h := &handlers{client: &mockClient{
		UpdatePageFn: func(_ context.Context, _ string, _ map[string]any) (*confluence.Page, error) {
			return nil, assert.AnError
		},
	}}
	_, err := h.writeUpdate(context.Background(), WriteItem{PageID: "p1", Title: "T", VersionNumber: 1}, false)
	require.Error(t, err)
}

func TestWriteTool_DescriptionMentionsAppend(t *testing.T) {
	desc := writeTool.Description
	assert.Contains(t, desc, "append:")
	assert.Contains(t, desc, "end")
	assert.Contains(t, desc, "after_heading")
	assert.Contains(t, desc, "replace_section")
	assert.Contains(t, desc, "new_heading",
		"agents cannot use a field the tool description never names")
}

// TestWriteTool_DescriptionFormatActionNames guards against the write tool
// description's format="storage" sentence (server.go) drifting from
// permittedWriteFields — the exact duplication that already went stale once
// on this branch (the page_id-on-create removal masked it). The description
// must name exactly the actions whose permitted field set contains "format".
func TestWriteTool_DescriptionFormatActionNames(t *testing.T) {
	var formatActions []string
	for _, action := range writeActionNames {
		if permittedWriteFields[action]["format"] {
			formatActions = append(formatActions, action)
		}
	}
	require.NotEmpty(t, formatActions)

	expected := formatActions[len(formatActions)-1]
	if len(formatActions) > 1 {
		expected = strings.Join(formatActions[:len(formatActions)-1], ", ") + ", or " + expected
	}

	assert.Contains(t, writeTool.Description, expected,
		`the format="storage" sentence must name exactly the actions whose permittedWriteFields set contains "format"`)
	assert.Contains(t, serverInstructions, expected,
		`serverInstructions carries its own copy of the format="storage" sentence — it must also name exactly the actions whose permittedWriteFields set contains "format"`)
}

func TestHandleWrite_ReplyComment(t *testing.T) {
	t.Run("footer reply routes to AddFooterCommentReplyFn", func(t *testing.T) {
		var footerCalls, inlineCalls int
		var capturedParentID string
		h := &handlers{
			client: &mockClient{
				AddFooterCommentReplyFn: func(_ context.Context, parentCommentID string, _ string) (*confluence.Comment, error) {
					footerCalls++
					capturedParentID = parentCommentID
					return &confluence.Comment{ID: "r1"}, nil
				},
				AddInlineCommentReplyFn: func(_ context.Context, _ string, _ string) (*confluence.InlineComment, error) {
					inlineCalls++
					return &confluence.InlineComment{ID: "should-not-be-called"}, nil
				},
			},
		}

		args := WriteArgs{
			Action: "reply_comment",
			Items:  []WriteItem{{ParentCommentID: "999", CommentType: "footer", Body: "Thanks!"}},
		}
		result, _, err := h.handleWrite(context.Background(), nil, args)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		assert.Equal(t, 1, footerCalls)
		assert.Equal(t, 0, inlineCalls, "inline reply must not fire for a footer reply")
		assert.Equal(t, "999", capturedParentID)

		text := firstText(t, result)
		assert.Contains(t, text, "r1")
		assert.Contains(t, text, "999")
		assert.Contains(t, text, "footer")
	})

	t.Run("inline reply routes to AddInlineCommentReplyFn", func(t *testing.T) {
		var footerCalls, inlineCalls int
		var capturedParentID string
		h := &handlers{
			client: &mockClient{
				AddFooterCommentReplyFn: func(_ context.Context, _ string, _ string) (*confluence.Comment, error) {
					footerCalls++
					return &confluence.Comment{ID: "should-not-be-called"}, nil
				},
				AddInlineCommentReplyFn: func(_ context.Context, parentCommentID string, _ string) (*confluence.InlineComment, error) {
					inlineCalls++
					capturedParentID = parentCommentID
					return &confluence.InlineComment{ID: "r2"}, nil
				},
			},
		}

		args := WriteArgs{
			Action: "reply_comment",
			Items:  []WriteItem{{ParentCommentID: "888", CommentType: "inline", Body: "Looks good."}},
		}
		result, _, err := h.handleWrite(context.Background(), nil, args)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		assert.Equal(t, 1, inlineCalls)
		assert.Equal(t, 0, footerCalls, "footer reply must not fire for an inline reply")
		assert.Equal(t, "888", capturedParentID)

		text := firstText(t, result)
		assert.Contains(t, text, "r2")
		assert.Contains(t, text, "888")
		assert.Contains(t, text, "inline")
	})

	t.Run("missing parent_comment_id errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeReplyComment(context.Background(), WriteItem{CommentType: "footer", Body: "x"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parent_comment_id")
	})

	t.Run("missing comment_type errors naming valid values", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeReplyComment(context.Background(), WriteItem{ParentCommentID: "1", Body: "x"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "comment_type is required")
		assert.Contains(t, err.Error(), "footer")
		assert.Contains(t, err.Error(), "inline")
	})

	t.Run("invalid comment_type reports the bad value, not a missing field", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeReplyComment(context.Background(), WriteItem{ParentCommentID: "1", CommentType: "footnote", Body: "x"}, false)
		require.Error(t, err)
		// "is required" would misdescribe a value that was supplied but wrong.
		assert.NotContains(t, err.Error(), "is required")
		assert.Contains(t, err.Error(), `invalid comment_type "footnote"`)
		assert.Contains(t, err.Error(), "footer")
		assert.Contains(t, err.Error(), "inline")
	})

	t.Run("missing body errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.writeReplyComment(context.Background(), WriteItem{ParentCommentID: "1", CommentType: "footer"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "body")
	})

	t.Run("markdown body converts to storage format", func(t *testing.T) {
		var capturedBody string
		h := &handlers{
			client: &mockClient{
				AddFooterCommentReplyFn: func(_ context.Context, _ string, storageBody string) (*confluence.Comment, error) {
					capturedBody = storageBody
					return &confluence.Comment{ID: "r3"}, nil
				},
			},
		}
		_, err := h.writeReplyComment(context.Background(), WriteItem{
			ParentCommentID: "1", CommentType: "footer", Body: "**bold** reply",
		}, false)
		require.NoError(t, err)
		assert.Contains(t, capturedBody, "<strong>bold</strong>")
	})

	t.Run("format storage passes body through unmodified", func(t *testing.T) {
		const raw = `<p>Raw <ac:structured-macro ac:name="info"></ac:structured-macro> XHTML.</p>`
		var capturedBody string
		h := &handlers{
			client: &mockClient{
				AddInlineCommentReplyFn: func(_ context.Context, _ string, storageBody string) (*confluence.InlineComment, error) {
					capturedBody = storageBody
					return &confluence.InlineComment{ID: "r4"}, nil
				},
			},
		}
		_, err := h.writeReplyComment(context.Background(), WriteItem{
			ParentCommentID: "1", CommentType: "inline", Body: raw, Format: "storage",
		}, false)
		require.NoError(t, err)
		assert.Equal(t, raw, capturedBody)
	})

	t.Run("dry_run fires no client call", func(t *testing.T) {
		h := &handlers{client: &mockClient{}} // no Fn set — call would panic
		msg, err := h.writeReplyComment(context.Background(), WriteItem{
			ParentCommentID: "1", CommentType: "footer", Body: "hi",
		}, true)
		require.NoError(t, err)
		assert.Contains(t, msg, "Would add")
		assert.Contains(t, msg, "footer")
		assert.Contains(t, msg, "comment 1")
	})
}

func TestHandleWrite_FieldValidation(t *testing.T) {
	t.Run("parent_id on comment errors naming parent_comment_id", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "comment", WriteItem{PageID: "1", Body: "hi", ParentID: "9"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parent_comment_id")
	})

	t.Run("parent_id on reply_comment errors naming parent_comment_id", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "reply_comment", WriteItem{
			ParentCommentID: "1", CommentType: "footer", Body: "hi", ParentID: "9",
		}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parent_comment_id")
	})

	t.Run("comment_type on comment errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "comment", WriteItem{PageID: "1", Body: "hi", CommentType: "footer"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "comment_type")
	})

	t.Run("comment_type on create errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "create", WriteItem{SpaceID: "s", Title: "t", CommentType: "footer"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "comment_type")
	})

	t.Run("comment_type on update errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "update", WriteItem{PageID: "1", VersionNumber: 1, CommentType: "footer"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "comment_type")
	})

	t.Run("new_heading is rejected by every action but append", func(t *testing.T) {
		items := map[string]WriteItem{
			"create":        {SpaceID: "s", Title: "t"},
			"update":        {PageID: "1", VersionNumber: 1},
			"delete":        {PageID: "1"},
			"comment":       {PageID: "1", Body: "hi"},
			"edit_comment":  {CommentID: "1", Body: "hi", VersionNumber: 1},
			"reply_comment": {ParentCommentID: "1", CommentType: "footer", Body: "hi"},
			"add_label":     {PageID: "1", Label: "l"},
			"remove_label":  {PageID: "1", Label: "l"},
		}
		for action, item := range items {
			t.Run(action, func(t *testing.T) {
				item.NewHeading = "New"
				h := &handlers{client: &mockClient{}}
				_, err := h.dispatchWriteItem(context.Background(), action, item, false)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "new_heading")
			})
		}
	})

	t.Run("parent_comment_id on comment errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "comment", WriteItem{PageID: "1", Body: "hi", ParentCommentID: "9"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parent_comment_id")
	})

	t.Run("parent_comment_id on create errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "create", WriteItem{SpaceID: "s", Title: "t", ParentCommentID: "9"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parent_comment_id")
	})

	t.Run("parent_comment_id on update errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "update", WriteItem{PageID: "1", VersionNumber: 1, ParentCommentID: "9"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parent_comment_id")
	})

	t.Run("parent_id on create still succeeds", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				CreatePageFn: func(_ context.Context, _ map[string]any) (*confluence.Page, error) {
					return &confluence.Page{ID: "1", Title: "t"}, nil
				},
			},
		}
		_, err := h.dispatchWriteItem(context.Background(), "create", WriteItem{SpaceID: "s", Title: "t", ParentID: "9"}, false)
		require.NoError(t, err)
	})

	t.Run("page_id on create with a macro sentinel body is accepted", func(t *testing.T) {
		h := &handlers{
			client: &mockClient{
				GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
					return &confluence.Page{ID: id, Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>Original.</p>"}}}, nil
				},
				CreatePageFn: func(_ context.Context, _ map[string]any) (*confluence.Page, error) {
					return &confluence.Page{ID: "new1", Title: "t"}, nil
				},
			},
		}
		_, err := h.dispatchWriteItem(context.Background(), "create", WriteItem{
			SpaceID: "s", Title: "t", PageID: "12345", Body: "Text.\n\n<!-- macro:m1 -->\n",
		}, false)
		require.NoError(t, err, "page_id names the source page whose macro registry to reuse — create's handler genuinely reads it when the body carries a macro sentinel")
	})

	t.Run("page_id on create without a macro sentinel body errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}} // no Fn set — a client call would panic
		_, err := h.dispatchWriteItem(context.Background(), "create", WriteItem{
			SpaceID: "s", Title: "t", PageID: "12345", Body: "Plain body, no macros.",
		}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page_id")
	})

	t.Run("page_id on create with no body errors", func(t *testing.T) {
		h := &handlers{client: &mockClient{}} // no Fn set — a client call would panic
		_, err := h.dispatchWriteItem(context.Background(), "create", WriteItem{
			SpaceID: "s", Title: "t", PageID: "12345",
		}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page_id")
	})

	t.Run("format on comment errors explaining why, not just that it is invalid", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "comment", WriteItem{PageID: "1", Body: "hi", Format: "storage"}, false)
		require.Error(t, err)
		assert.Equal(t, `format is not supported for action "comment" — comment bodies are always converted from Markdown; raw XHTML in comments is not yet supported`, err.Error())
	})

	t.Run("format on edit_comment errors explaining why, not just that it is invalid", func(t *testing.T) {
		h := &handlers{client: &mockClient{}}
		_, err := h.dispatchWriteItem(context.Background(), "edit_comment", WriteItem{CommentID: "1", Body: "hi", VersionNumber: 1, Format: "storage"}, false)
		require.Error(t, err)
		assert.Equal(t, `format is not supported for action "edit_comment" — comment bodies are always converted from Markdown; raw XHTML in comments is not yet supported`, err.Error())
	})
}

// TestWriteFields_MatchesWriteItemStruct guards D6's self-defending property:
// writeFields is a hand-maintained mirror of every WriteItem field, and a new
// field left off it is accepted by every action and silently dropped — the
// original mis-post's defect class, reproduced. A count-only check would pass
// if a future change adds one field and removes another, so this also checks
// that every writeFields name matches a real WriteItem json tag.
func TestWriteFields_MatchesWriteItemStruct(t *testing.T) {
	typ := reflect.TypeOf(WriteItem{})

	t.Run("count matches WriteItem field count", func(t *testing.T) {
		assert.Equal(t, typ.NumField(), len(writeFields),
			"writeFields has %d entries but WriteItem has %d fields — add the new field to writeFields and to the permittedWriteFields set of every action whose handler consumes it",
			len(writeFields), typ.NumField())
	})

	t.Run("every name matches an actual WriteItem json tag", func(t *testing.T) {
		tagNames := make(map[string]bool, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			tagNames[name] = true
		}
		for _, f := range writeFields {
			assert.True(t, tagNames[f.name],
				"writeFields entry %q has no matching WriteItem json tag — add the new field to writeFields and to the permittedWriteFields set of every action whose handler consumes it",
				f.name)
		}
	})
}

// TestValidActionsMatchesPermittedWriteFields guards against the two tables
// drifting: a key present in validActions but missing from
// permittedWriteFields yields a nil permitted map, which rejects every field
// for that action with a message that reads like a schema bug rather than a
// missing table row.
func TestValidActionsMatchesPermittedWriteFields(t *testing.T) {
	for action := range validActions {
		_, ok := permittedWriteFields[action]
		assert.True(t, ok, "action %q is in validActions but missing from permittedWriteFields", action)
	}
	for action := range permittedWriteFields {
		_, ok := validActions[action]
		assert.True(t, ok, "action %q is in permittedWriteFields but missing from validActions", action)
	}
}

// TestHandleWrite_BatchValidationFailure exercises validation through
// handleWrite's multi-item accumulation, not just dispatchWriteItem directly.
// Every other field-validation test bypasses handleWrite's per-item loop, so
// nothing else would catch a refactor that hoisted validation to a pre-flight
// pass or swallowed a per-item error.
func TestHandleWrite_BatchValidationFailure(t *testing.T) {
	var footerCalls int
	h := &handlers{
		client: &mockClient{
			AddFooterCommentReplyFn: func(_ context.Context, _ string, _ string) (*confluence.Comment, error) {
				footerCalls++
				return &confluence.Comment{ID: "r1"}, nil
			},
		},
	}

	args := WriteArgs{
		Action: "reply_comment",
		Items: []WriteItem{
			{ParentCommentID: "1", CommentType: "footer", Body: "ok"},
			{ParentCommentID: "2", CommentType: "footer", Body: "bad", ParentID: "9"},
		},
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	require.NoError(t, err)

	text := firstText(t, result)
	assert.Contains(t, text, "[1] Added footer reply r1 to comment 1")
	assert.Contains(t, text, "[2] error:")
	assert.Contains(t, text, "parent_comment_id")
	assert.Equal(t, 1, footerCalls, "item 2's validation error must not reach the client")
}

func TestHandleWrite_ReplyCommentActionReachable(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	// dry_run: true so a reachable dispatch never touches the (unset) client
	// mock. If reply_comment were still missing from validActions, this
	// would come back as the unknown-action error instead of the dry-run
	// preview — that is the failure Part 1 of the contract exists to prevent.
	args := WriteArgs{
		Action: "reply_comment",
		Items:  []WriteItem{{ParentCommentID: "1", CommentType: "footer", Body: "hi"}},
		DryRun: true,
	}
	result, _, err := h.handleWrite(context.Background(), nil, args)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := firstText(t, result)
	assert.Contains(t, text, "Would add")
	assert.NotContains(t, text, "unknown action")

	// The unknown-action error string also names reply_comment, so removing
	// it from that string alone (without touching validActions) still fails
	// this test.
	badArgs := WriteArgs{Action: "fly_to_moon", Items: []WriteItem{{PageID: "1"}}}
	badResult, _, err := h.handleWrite(context.Background(), nil, badArgs)
	require.NoError(t, err)
	assert.True(t, badResult.IsError)
	badText := firstText(t, badResult)
	assert.Contains(t, badText, "reply_comment")
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

