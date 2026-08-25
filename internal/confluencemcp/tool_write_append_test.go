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
// rather than position alone. wantBoundary additionally pins the anchors the
// preview's "boundary" object carries for start and replace_preamble, whose
// insert/start/end anchors are the caller's only preview signal for where a
// headless-capable splice actually lands.
func TestAppend_DryRun(t *testing.T) {
	const endOfSectionBody = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>` +
		`<h2>Section A</h2><p>existing</p><h2>Section B</h2><p>other</p>` +
		`</ac:layout-cell></ac:layout-section></ac:layout>`
	const preambleDryRunBody = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>` +
		`<h1>Overview</h1><p>Body.</p>` +
		`</ac:layout-cell></ac:layout-section></ac:layout>`

	tests := []struct {
		name         string
		body         string
		position     string
		heading      string
		wantPosition string
		wantSummary  string
		wantBoundary []string
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
		{
			name:         "start",
			body:         appendTestLayoutBody,
			position:     "start",
			wantPosition: "start",
			wantSummary:  "Insert at start of page.",
			wantBoundary: []string{
				`"insert_anchor": "after opening`,
				`"container": "ac:layout-cell"`,
			},
		},
		{
			name:         "replace_preamble",
			body:         preambleDryRunBody,
			position:     "replace_preamble",
			wantPosition: "replace_preamble",
			// No content precedes the heading in preambleDryRunBody, so the
			// replaced-elements clause is empty — the summary is the bare
			// sentence with no "(replaces …)" parenthetical to escape.
			wantSummary: "Replace page preamble.",
			wantBoundary: []string{
				`"start_anchor": "start of container"`,
				`"end_anchor": "before first heading`,
				"Overview",
				`"container": "ac:layout-cell"`,
			},
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
			for _, want := range tt.wantBoundary {
				assert.Contains(t, msg, want)
			}
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

// TestPositionFieldRulesCoversEveryPosition guards the claim in
// positionFields' doc comment that a Mode missing from positionFieldRules
// "fails loudly". It drives from the single production positionStrings list
// (plus the empty string, which parseMode also accepts as shorthand for
// "end" but which positionStrings itself does not carry), so a position
// added to parseMode without a corresponding positionFieldRules entry is
// caught here regardless of whether positionStrings was updated too — the
// two tables can no longer drift out of step with each other undetected.
func TestPositionFieldRulesCoversEveryPosition(t *testing.T) {
	positions := append([]string{""}, positionStrings...)
	for _, position := range positions {
		t.Run(fmt.Sprintf("position %q", position), func(t *testing.T) {
			mode, err := parseMode(position)
			require.NoError(t, err)
			_, ok := positionFieldRules[mode]
			assert.True(t, ok, "no positionFieldRules entry for the Mode produced by position %q", position)
		})
	}
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
		{"start", "start", ModeStart},
		{"replace_preamble", "replace_preamble", ModeReplacePreamble},
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

// TestAppend_SuccessMsg_NamesReplacedElements covers the defect where a
// non-dry-run replace_preamble (or replace_section) reported only the byte
// delta, never what was actually destroyed: ReplacedElementSummary was built
// but never rendered into the success message. A bare-text preamble is the
// sharper case — the walker emits no start/end events for text, so the
// summary itself is empty and the byte delta was the only clue at all; that
// case must fall back to naming the replaced byte count instead of going
// silent.
func TestAppend_SuccessMsg_NamesReplacedElements(t *testing.T) {
	t.Run("replace_preamble names a paragraph and a named macro", func(t *testing.T) {
		body := `<p>intro</p><ac:structured-macro ac:name="toc"/><h2>A</h2><p>c</p>`
		h := &handlers{client: newAppendPageMock(body, nil)}
		msg, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "NEW", Position: "replace_preamble",
		}, false)
		require.NoError(t, err)
		assert.Contains(t, msg, `(replaces <p> x 1, macro "toc" x 1)`)
		assert.NotContains(t, msg, "..", "must not produce a double full stop")
	})

	t.Run("bare-text preamble names the replaced byte count, not silence", func(t *testing.T) {
		const preamble = "Some intro text."
		body := preamble + `<h2>A</h2><p>c</p>`
		h := &handlers{client: newAppendPageMock(body, nil)}
		msg, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "NEW", Position: "replace_preamble",
		}, false)
		require.NoError(t, err)
		assert.Contains(t, msg, fmt.Sprintf("%d bytes", len(preamble)))
	})

	t.Run("replace_section still renders its subsection clause and now also names replaced elements", func(t *testing.T) {
		const body = `<h2>Ticket map</h2><p>intro</p><h3>Delivery sequence</h3><p>seq</p><h2>Next</h2>`
		h := &handlers{client: newAppendPageMock(body, nil)}
		msg, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "New intro.", Position: "replace_section", Heading: "Ticket map",
		}, false)
		require.NoError(t, err)
		assert.Contains(t, msg, `replaces <p> x 1`)
		assert.Contains(t, msg, `Preserved 1 nested section: "Delivery sequence".`)
	})

	t.Run("replace_section rename still renders its rename clause and now also names replaced elements", func(t *testing.T) {
		const body = `<h2>Ticket map</h2><p>intro</p><h2>Next</h2>`
		h := &handlers{client: newAppendPageMock(body, nil)}
		msg, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "New intro.", Position: "replace_section",
			Heading: "Ticket map", NewHeading: "Delivery map",
		}, false)
		require.NoError(t, err)
		assert.Contains(t, msg, `(replaces <p> x 1)`)
		assert.Contains(t, msg, `Renamed heading "Ticket map" → "Delivery map".`)
	})

	t.Run("end insert replaces nothing and gains no spurious clause", func(t *testing.T) {
		h := &handlers{client: newAppendPageMock(appendTestLayoutBody, nil)}
		msg, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "x", Position: "end",
		}, false)
		require.NoError(t, err)
		assert.NotContains(t, msg, "(replaces")
		assert.NotContains(t, msg, "bytes with")
		assert.NotContains(t, msg, "..", "must not produce a double full stop")
	})
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

// headingFor returns the heading a position requires, empty for a position
// that rejects one outright ("end") — supplying one anyway would trip the
// heading check before the field under test is ever reached.
func headingFor(position string) string {
	if position == "end" {
		return ""
	}
	return "Section A"
}

func TestAppend_IncludeSubsections_RejectedForOtherPositions(t *testing.T) {
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, nil)}

	for _, position := range []string{"end", "after_heading", "end_of_section"} {
		t.Run(position, func(t *testing.T) {
			_, err := h.writeAppend(context.Background(), WriteItem{
				PageID: "p1", Body: "x", Position: position,
				Heading: headingFor(position), IncludeSubsections: true,
			}, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "include_subsections")
		})
	}
}

func TestAppend_NewHeading_RejectedForOtherPositions(t *testing.T) {
	h := &handlers{client: newAppendPageMock(appendTestLayoutBody, nil)}

	for _, position := range []string{"end", "after_heading", "end_of_section"} {
		t.Run(position, func(t *testing.T) {
			_, err := h.writeAppend(context.Background(), WriteItem{
				PageID: "p1", Body: "x", Position: position,
				Heading: headingFor(position), NewHeading: "Section Z",
			}, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "new_heading")
		})
	}
}

// TestAppend_ReplaceSection_RenamesHeading covers the whole rename path through
// the handler: the PUT body carries the new heading, the response says so, and
// the on-page anchor reference the rename breaks is named rather than left for
// the caller to discover.
func TestAppend_ReplaceSection_RenamesHeading(t *testing.T) {
	const body = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>` +
		`<h2>Ticket map</h2><p>intro</p><h2>Next</h2>` +
		`<p><ac:link ac:anchor="Ticket map"><ac:link-body>jump</ac:link-body></ac:link></p>` +
		`</ac:layout-cell></ac:layout-section></ac:layout>`

	item := WriteItem{
		PageID: "p1", Body: "New intro.", Position: "replace_section",
		Heading: "Ticket map", NewHeading: "Delivery map",
	}

	var captured map[string]any
	h := &handlers{client: newAppendPageMock(body, &captured)}
	msg, err := h.writeAppend(context.Background(), item, false)
	require.NoError(t, err)
	assert.Contains(t, msg, `Renamed heading "Ticket map" → "Delivery map".`)
	assert.Contains(t, msg, `1 on-page anchor reference to "Ticket map"`)

	value := captured["body"].(map[string]any)["storage"].(map[string]any)["value"].(string)
	assert.Contains(t, value, "<h2>Delivery map</h2>")
	assert.NotContains(t, value, "<h2>Ticket map</h2>")
	assert.Contains(t, value, "New intro.")
	assert.Contains(t, value, "<h2>Next</h2>", "the next section is never touched")

	dryH := &handlers{client: newAppendPageMock(body, nil)}
	preview, err := dryH.writeAppend(context.Background(), item, true)
	require.NoError(t, err)
	assert.Contains(t, preview, `rename it to \"Delivery map\"`)
	// The preview is JSON, so its markup arrives \u-escaped.
	assert.Contains(t, preview, `\u003ch2\u003eDelivery map\u003c/h2\u003e`,
		"the preview's before-context must show the heading as it will be, not as it was")
	assert.Contains(t, preview, `"new_heading": "Delivery map"`)
	assert.Contains(t, preview, `"anchor_references"`)
}

func TestAppend_ReplaceSection_RejectsBadRename(t *testing.T) {
	const body = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>` +
		`<h2>Ticket map</h2><p>intro</p><h2>Next</h2>` +
		`</ac:layout-cell></ac:layout-section></ac:layout>`

	tests := []struct {
		name       string
		newHeading string
		wantInMsg  string
	}{
		{name: "same as current", newHeading: "Ticket map", wantInMsg: "rename_no_op"},
		{name: "already on the page", newHeading: "Next", wantInMsg: "rename_ambiguous"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]any
			h := &handlers{client: newAppendPageMock(body, &captured)}
			_, err := h.writeAppend(context.Background(), WriteItem{
				PageID: "p1", Body: "New intro.", Position: "replace_section",
				Heading: "Ticket map", NewHeading: tt.newHeading,
			}, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantInMsg)
			assert.Nil(t, captured, "a rejected rename must not reach the update call")
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

// TestWriteAppend_PositionFieldValidation pins the per-position field table:
// heading is required by after_heading/end_of_section/replace_section and
// rejected by every other position (including "end" — a behaviour change,
// see below); include_subsections and new_heading are allowed only on
// replace_section. A field the handler does not consume must never be
// silently dropped, which is what "end" + heading did before this change.
func TestWriteAppend_PositionFieldValidation(t *testing.T) {
	const bareBody = `<h2>Section A</h2><p>existing</p>`

	tests := []struct {
		name       string
		item       WriteItem
		wantErrMsg string // empty means no error expected
	}{
		{
			name: "start accepts bare page_id and body",
			item: WriteItem{PageID: "p1", Body: "x", Position: "start"},
		},
		{
			name: "replace_preamble accepts bare page_id and body",
			item: WriteItem{PageID: "p1", Body: "x", Position: "replace_preamble"},
		},
		{
			name:       "start rejects heading, message names the position",
			item:       WriteItem{PageID: "p1", Body: "x", Position: "start", Heading: "Section A"},
			wantErrMsg: `"start"`,
		},
		{
			name:       "replace_preamble rejects heading, message names the position",
			item:       WriteItem{PageID: "p1", Body: "x", Position: "replace_preamble", Heading: "Section A"},
			wantErrMsg: `"replace_preamble"`,
		},
		{
			name:       `end rejects heading (behaviour change — previously silently dropped)`,
			item:       WriteItem{PageID: "p1", Body: "x", Position: "end", Heading: "Section A"},
			wantErrMsg: `"end"`,
		},
		{
			name:       "start rejects include_subsections",
			item:       WriteItem{PageID: "p1", Body: "x", Position: "start", IncludeSubsections: true},
			wantErrMsg: "include_subsections",
		},
		{
			name:       "start rejects new_heading",
			item:       WriteItem{PageID: "p1", Body: "x", Position: "start", NewHeading: "Z"},
			wantErrMsg: "new_heading",
		},
		{
			name:       "replace_preamble rejects include_subsections",
			item:       WriteItem{PageID: "p1", Body: "x", Position: "replace_preamble", IncludeSubsections: true},
			wantErrMsg: "include_subsections",
		},
		{
			name:       "replace_preamble rejects new_heading",
			item:       WriteItem{PageID: "p1", Body: "x", Position: "replace_preamble", NewHeading: "Z"},
			wantErrMsg: "new_heading",
		},
		{
			name:       "after_heading with no heading still gives the existing error",
			item:       WriteItem{PageID: "p1", Body: "x", Position: "after_heading"},
			wantErrMsg: "heading is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &handlers{client: newAppendPageMock(bareBody, nil)}
			_, err := h.writeAppend(context.Background(), tt.item, false)
			if tt.wantErrMsg == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}

	t.Run("unknown position lists every entry in positionStrings", func(t *testing.T) {
		h := &handlers{client: newAppendPageMock(bareBody, nil)}
		_, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "x", Position: "sideways",
		}, false)
		require.Error(t, err)
		for _, pos := range positionStrings {
			assert.Contains(t, err.Error(), pos)
		}
	})

	t.Run("replace_preamble on a headless page surfaces ErrNoHeadingOnPage and issues no UpdatePage call", func(t *testing.T) {
		updateCalled := false
		h := &handlers{client: &mockClient{
			GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
				return &confluence.Page{
					ID: id, Title: "Test", Version: confluence.PageVersion{Number: 1},
					Body: confluence.PageBody{Storage: confluence.StorageBody{Value: "<p>no heading on this page</p>"}},
				}, nil
			},
			UpdatePageFn: func(_ context.Context, _ string, _ map[string]any) (*confluence.Page, error) {
				updateCalled = true
				return nil, nil
			},
		}}

		_, err := h.writeAppend(context.Background(), WriteItem{
			PageID: "p1", Body: "x", Position: "replace_preamble",
		}, false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNoHeadingOnPage), "expected no_heading_on_page, got %v", err)
		assert.False(t, updateCalled, "a rejected replace_preamble must not reach UpdatePage")
	})
}
