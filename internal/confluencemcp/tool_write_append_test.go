package confluencemcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sishbi/confluence-mcp/internal/confluence"
)

const appendTestLayoutBody = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Section A</h2><p>existing</p></ac:layout-cell></ac:layout-section></ac:layout>`

func newAppendPageMock(base string, updateSpy *map[string]any) *mockClient {
	return &mockClient{
		GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
			return &confluence.Page{
				ID:      id,
				Title:   "Test Page",
				Version: confluence.PageVersion{Number: 7},
				Body:    confluence.PageBody{Storage: confluence.StorageBody{Value: base, Representation: "storage"}},
			}, nil
		},
		UpdatePageFn: func(_ context.Context, id string, payload map[string]any) (*confluence.Page, error) {
			if updateSpy != nil {
				*updateSpy = payload
			}
			return &confluence.Page{ID: id, Title: "Test Page"}, nil
		},
	}
}

func TestAppend_End_Markdown(t *testing.T) {
	var captured map[string]any
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, &captured)}

	msg, err := h.writeAppend(context.Background(), WriteItem{
		PageID:   "p1",
		Body:     "Note appended.",
		Position: "end",
	}, false)
	require.NoError(t, err)
	assert.Contains(t, msg, "Appended to")

	body := captured["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	value := storage["value"].(string)
	assert.Contains(t, value, "Note appended.")
	// Original content preserved.
	assert.Contains(t, value, "Section A")
	assert.Contains(t, value, "existing")
	// Version bumped.
	version := captured["version"].(map[string]any)
	assert.Equal(t, 8, version["number"])
}

func TestAppend_End_DryRun(t *testing.T) {
	getCalls := 0
	updateCalled := false
	h := &handlers{client: &mockClient{
		GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
			getCalls++
			return &confluence.Page{
				ID: id, Title: "Test", Version: confluence.PageVersion{Number: 1},
				Body: confluence.PageBody{Storage: confluence.StorageBody{Value: appendTestLayoutBody}},
			}, nil
		},
		UpdatePageFn: func(_ context.Context, _ string, _ map[string]any) (*confluence.Page, error) {
			updateCalled = true
			return nil, nil
		},
	}}

	msg, err := h.writeAppend(context.Background(), WriteItem{
		PageID: "p1", Body: "dry note", Position: "end",
	}, true)
	require.NoError(t, err)
	assert.Contains(t, msg, "Would append")
	// Preview JSON should be embedded.
	assert.Contains(t, msg, `"position": "end"`)
	assert.Contains(t, msg, `"input_body": "dry note"`)
	assert.Contains(t, msg, `"storage_output":`)
	assert.Equal(t, 1, getCalls)
	assert.False(t, updateCalled, "dry_run must not call UpdatePage")
}

func TestAppend_StorageFormat_SkipsConversion(t *testing.T) {
	var captured map[string]any
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, &captured)}

	storageFragment := `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>raw</p></ac:rich-text-body></ac:structured-macro>`

	_, err := h.writeAppend(context.Background(), WriteItem{
		PageID: "p1", Body: storageFragment, Format: "storage", Position: "end",
	}, false)
	require.NoError(t, err)

	body := captured["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	value := storage["value"].(string)
	// The raw XHTML fragment should appear verbatim, not wrapped in <p>.
	assert.Contains(t, value, `ac:name="info"`)
	assert.NotContains(t, value, `<p>`+storageFragment)
}

func TestAppend_AfterHeading_NotFound(t *testing.T) {
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, nil)}

	_, err := h.writeAppend(context.Background(), WriteItem{
		PageID:   "p1",
		Body:     "x",
		Position: "after_heading",
		Heading:  "Does Not Exist",
	}, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHeadingNotFound), "expected heading_not_found, got %v", err)
}

func TestAppend_ReplaceSection_InMacro(t *testing.T) {
	body := `<ac:structured-macro ac:name="expand"><ac:rich-text-body><h3>T</h3></ac:rich-text-body></ac:structured-macro>`
	h := &handlers{client: newAppendPageMock(body, nil)}

	_, err := h.writeAppend(context.Background(), WriteItem{
		PageID: "p1", Body: "x", Position: "replace_section", Heading: "T",
	}, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHeadingInUnsafeContainer))
}

func TestAppend_RequiredFields(t *testing.T) {
	h := &handlers{client: &mockClient{}}

	t.Run("page_id required", func(t *testing.T) {
		_, err := h.writeAppend(context.Background(), WriteItem{Body: "x"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page_id")
	})
	t.Run("body required", func(t *testing.T) {
		_, err := h.writeAppend(context.Background(), WriteItem{PageID: "p1"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "body")
	})
	t.Run("heading required for after_heading", func(t *testing.T) {
		_, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "x", Position: "after_heading",
		}, false)
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "heading")
	})
	t.Run("unknown position rejected", func(t *testing.T) {
		_, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "x", Position: "fly_away",
		}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown position")
	})
}

func TestAppend_VersionMismatch(t *testing.T) {
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, nil)}

	_, err := h.writeAppend(context.Background(), WriteItem{
		PageID:        "p1",
		Body:          "x",
		Position:      "end",
		VersionNumber: 3, // server says 7
	}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version_conflict")
}

func TestAppend_CacheEvictedOnSuccess(t *testing.T) {
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, nil)}
	h.cache.put(&cachedPage{pageID: "p1", markdown: "stale"})

	_, err := h.writeAppend(context.Background(), WriteItem{
		PageID: "p1", Body: "x", Position: "end",
	}, false)
	require.NoError(t, err)

	_, ok := h.cache.get("p1")
	assert.False(t, ok, "cache should be evicted after successful append")
}
