package confluencemcp

import (
	"context"
	"errors"
	"fmt"
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

// TestAppend_DryRun table-drives the dry-run preview across positions. A
// position missing from the preview switch surfaces as `"position": "unknown"`
// alongside an EMPTY action_summary, so every case pins both fields together
// rather than position alone.
func TestAppend_DryRun(t *testing.T) {
	const endOfSectionBody = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>` +
		`<h2>Section A</h2><p>existing</p><h2>Section B</h2><p>other</p>` +
		`</ac:layout-cell></ac:layout-section></ac:layout>`

	tests := []struct {
		name         string
		body         string
		position     string
		heading      string
		wantPosition string
		wantSummary  string
	}{
		{
			name:         "end",
			body:         appendTestLayoutBody,
			position:     "end",
			wantPosition: "end",
			wantSummary:  "Append to end of page.",
		},
		{
			name:         "end_of_section",
			body:         endOfSectionBody,
			position:     "end_of_section",
			heading:      "Section A",
			wantPosition: "end_of_section",
			wantSummary:  `Append to end of section "Section A".`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getCalls := 0
			updateCalled := false
			h := &handlers{client: &mockClient{
				GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
					getCalls++
					return &confluence.Page{
						ID: id, Title: "Test", Version: confluence.PageVersion{Number: 1},
						Body: confluence.PageBody{Storage: confluence.StorageBody{Value: tt.body}},
					}, nil
				},
				UpdatePageFn: func(_ context.Context, _ string, _ map[string]any) (*confluence.Page, error) {
					updateCalled = true
					return nil, nil
				},
			}}

			msg, err := h.writeAppend(context.Background(), WriteItem{
				PageID: "p1", Body: "dry note", Position: tt.position, Heading: tt.heading,
			}, true)
			require.NoError(t, err)
			assert.Contains(t, msg, "Would append")
			// Preview JSON should be embedded.
			assert.Contains(t, msg, fmt.Sprintf(`"position": %q`, tt.wantPosition))
			assert.Contains(t, msg, fmt.Sprintf(`"action_summary": %q`, tt.wantSummary))
			assert.Contains(t, msg, `"input_body": "dry note"`)
			assert.Contains(t, msg, `"storage_output":`)
			assert.Equal(t, 1, getCalls)
			assert.False(t, updateCalled, "dry_run must not call UpdatePage")
		})
	}
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
	t.Run("heading required for end_of_section", func(t *testing.T) {
		_, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "x", Position: "end_of_section",
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
		// An agent that guesses wrong should be told end_of_section exists.
		assert.Contains(t, err.Error(), "end_of_section")
	})
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		name     string
		position string
		want     Mode
	}{
		{"empty defaults to end", "", ModeEnd},
		{"end", "end", ModeEnd},
		{"after_heading", "after_heading", ModeAfterHeading},
		{"replace_section", "replace_section", ModeReplaceSection},
		{"end_of_section", "end_of_section", ModeEndOfSection},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMode(tt.position)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppend_EndOfSection(t *testing.T) {
	body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>` +
		`<h2>Section A</h2><p>existing</p><h2>Section B</h2><p>other</p>` +
		`</ac:layout-cell></ac:layout-section></ac:layout>`
	var captured map[string]any
	h := &handlers{client: newAppendPageMock(body, &captured)}

	msg, err := h.writeAppend(context.Background(), WriteItem{
		PageID:   "p1",
		Body:     "New paragraph.",
		Position: "end_of_section",
		Heading:  "Section A",
	}, false)
	require.NoError(t, err)
	assert.Contains(t, msg, "Appended to")

	updatedBody := captured["body"].(map[string]any)
	storage := updatedBody["storage"].(map[string]any)
	value := storage["value"].(string)

	// Regression case for the incident this mode fixes: the new fragment must
	// land at the END of Section A (after its own paragraph), and Section B's
	// paragraph must stay attached to Section B rather than being pushed below
	// the new content.
	require.Contains(t, value, "New paragraph.")
	idxExisting := strings.Index(value, "existing")
	idxNew := strings.Index(value, "New paragraph.")
	idxSectionB := strings.Index(value, "Section B")
	idxOther := strings.Index(value, "other")
	require.True(t, idxExisting >= 0 && idxNew >= 0 && idxSectionB >= 0 && idxOther >= 0)
	assert.True(t, idxExisting < idxNew, "existing paragraph should precede the new fragment")
	assert.True(t, idxNew < idxSectionB, "new fragment should precede Section B's heading")
	assert.True(t, idxSectionB < idxOther, "Section B heading should precede its own paragraph")
}

// TestAppend_ReplaceSection_Subsections covers the defect where replacing an
// h2 silently deleted its h3 subsections: the default must keep them, the
// opt-in must remove them, and both the success message and the dry-run
// preview must name them rather than leaving the byte delta as the only clue.
func TestAppend_ReplaceSection_Subsections(t *testing.T) {
	const body = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>` +
		`<h2>Ticket map</h2><p>intro</p><h3>Delivery sequence</h3><p>seq</p><h2>Next</h2>` +
		`</ac:layout-cell></ac:layout-section></ac:layout>`

	tests := []struct {
		name               string
		includeSubsections bool
		wantSubsection     bool
		wantMsg            string
		wantSummary        string
	}{
		{
			name:               "default preserves the subsection",
			includeSubsections: false,
			wantSubsection:     true,
			wantMsg:            `Preserved 1 nested section: "Delivery sequence".`,
			wantSummary:        `(replaces \u003cp\u003e x 1; preserves 1 nested section: \"Delivery sequence\")`,
		},
		{
			name:               "include_subsections removes the subsection",
			includeSubsections: true,
			wantSubsection:     false,
			wantMsg:            `Replaced 1 nested section: "Delivery sequence".`,
			wantSummary:        `(replaces \u003cp\u003e x 2, \u003ch3\u003e x 1; replaces 1 nested section: \"Delivery sequence\")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := WriteItem{
				PageID: "p1", Body: "New intro.", Position: "replace_section",
				Heading: "Ticket map", IncludeSubsections: tt.includeSubsections,
			}

			var captured map[string]any
			h := &handlers{client: newAppendPageMock(body, &captured)}
			msg, err := h.writeAppend(context.Background(), item, false)
			require.NoError(t, err)
			assert.Contains(t, msg, tt.wantMsg)

			value := captured["body"].(map[string]any)["storage"].(map[string]any)["value"].(string)
			assert.Contains(t, value, "New intro.")
			assert.Contains(t, value, "<h2>Ticket map</h2>", "the target heading is always kept")
			assert.Contains(t, value, "<h2>Next</h2>", "the next section is never touched")
			if tt.wantSubsection {
				assert.Contains(t, value, "<h3>Delivery sequence</h3>")
				assert.Contains(t, value, "<p>seq</p>", "the subsection's own content survives too")
			} else {
				assert.NotContains(t, value, "Delivery sequence")
				assert.NotContains(t, value, "<p>seq</p>")
			}

			dryH := &handlers{client: newAppendPageMock(body, nil)}
			preview, err := dryH.writeAppend(context.Background(), item, true)
			require.NoError(t, err)
			assert.Contains(t, preview, tt.wantSummary)
		})
	}
}

func TestAppend_IncludeSubsections_RejectedForOtherPositions(t *testing.T) {
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, nil)}

	for _, position := range []string{"end", "after_heading", "end_of_section"} {
		t.Run(position, func(t *testing.T) {
			_, err := h.writeAppend(context.Background(), WriteItem{
				PageID: "p1", Body: "x", Position: position,
				Heading: "Section A", IncludeSubsections: true,
			}, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "include_subsections")
		})
	}
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
