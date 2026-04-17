package mdconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sishbi/confluence-mcp/internal/mdconv/testgen"
)

func TestClassifyMacro(t *testing.T) {
	tests := []struct {
		name     string
		expected MacroCategory
	}{
		{"info", CategoryEditableFlat},
		{"note", CategoryEditableFlat},
		{"warning", CategoryEditableFlat},
		{"tip", CategoryEditableFlat},
		{"excerpt", CategoryEditableFlat},
		{"expand", CategoryEditableStructured},
		{"details", CategoryOpaqueBody},
		{"toc", CategoryOpaque},
		{"status", CategoryOpaque},
		{"noformat", CategoryOpaque},
		{"anchor", CategoryOpaque},
		{"children", CategoryOpaque},
		{"jira", CategoryOpaque},
		{"view-file", CategoryOpaque},
		{"unknown-macro", CategoryOpaque},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, classifyMacro(tt.name))
		})
	}
}

func TestMacroRegistry_Lookup(t *testing.T) {
	reg := &MacroRegistry{
		Entries: []MacroEntry{
			{ID: "m1", Name: "info", Category: CategoryEditableFlat, OriginalXML: "<original-m1/>"},
			{ID: "m2", Name: "toc", Category: CategoryOpaque, OriginalXML: "<original-m2/>"},
		},
	}

	// Found
	entry := reg.Lookup("m1")
	assert.NotNil(t, entry)
	assert.Equal(t, "info", entry.Name)

	// Found
	entry = reg.Lookup("m2")
	assert.NotNil(t, entry)
	assert.Equal(t, "toc", entry.Name)

	// Not found
	assert.Nil(t, reg.Lookup("m99"))
	assert.Nil(t, (*MacroRegistry)(nil).Lookup("m1"))
}

func TestExtractMacros_InfoPanel(t *testing.T) {
	input := `<p>Before.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Important info.</p></ac:rich-text-body></ac:structured-macro><p>After.</p>`

	result, registry := extractMacros(input, nil)

	assert.Contains(t, result, "Before.")
	assert.Contains(t, result, "After.")
	assert.NotContains(t, result, "ac:structured-macro")
	// Placeholder should contain the sentinel token and body text. info panels
	// render as GFM alerts (`[!NOTE]`), so check for the alert marker rather
	// than the legacy "Info:" label.
	assert.Contains(t, result, "MACROSENTINEL:m1:")
	assert.Contains(t, result, "[!NOTE]")
	assert.Contains(t, result, "Important info.")

	require.NotNil(t, registry)
	require.Len(t, registry.Entries, 1)
	assert.Equal(t, "m1", registry.Entries[0].ID)
	assert.Equal(t, "info", registry.Entries[0].Name)
	assert.Equal(t, CategoryEditableFlat, registry.Entries[0].Category)
	assert.Contains(t, registry.Entries[0].OriginalXML, `ac:name="info"`)
}

func TestExtractMacros_Expand(t *testing.T) {
	input := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Click me</ac:parameter><ac:rich-text-body><p>Hidden content.</p></ac:rich-text-body></ac:structured-macro>`

	result, registry := extractMacros(input, nil)

	assert.NotContains(t, result, "ac:structured-macro")
	assert.Contains(t, result, "MACROSENTINEL:m1:")
	// New format: bold title line with ▶ prefix inside a <p>, not <details>/<summary>.
	assert.Contains(t, result, "<strong>▶ Click me</strong>")
	assert.NotContains(t, result, "<details>")
	assert.Contains(t, result, "Hidden content.")
	// End sentinel must be present so the block boundary is explicit.
	assert.Contains(t, result, "MACROENDSENTINEL:m1:")

	require.NotNil(t, registry)
	require.Len(t, registry.Entries, 1)
	assert.Equal(t, CategoryEditableStructured, registry.Entries[0].Category)
}

func TestExtractMacros_Opaque_Status(t *testing.T) {
	input := `<p>Text before.</p><ac:structured-macro ac:name="status"><ac:parameter ac:name="title">In Progress</ac:parameter><ac:parameter ac:name="colour">Yellow</ac:parameter></ac:structured-macro><p>Text after.</p>`

	result, registry := extractMacros(input, nil)

	assert.NotContains(t, result, "ac:structured-macro")
	assert.Contains(t, result, "MACROSENTINEL:m1:")
	// Status renders as an emoji-prefixed badge keyed off the `colour` param.
	assert.Contains(t, result, "🟡 In Progress")

	require.NotNil(t, registry)
	require.Len(t, registry.Entries, 1)
	assert.Equal(t, CategoryOpaque, registry.Entries[0].Category)
}

func TestExtractMacros_Opaque_TOC(t *testing.T) {
	input := `<ac:structured-macro ac:name="toc"><ac:parameter ac:name="printable">true</ac:parameter></ac:structured-macro>`

	result, registry := extractMacros(input, nil)

	assert.NotContains(t, result, "ac:structured-macro")
	assert.Contains(t, result, "MACROSENTINEL:m1:")
	assert.Contains(t, result, "Table of Contents")

	require.NotNil(t, registry)
	assert.Equal(t, "toc", registry.Entries[0].Name)
}

func TestExtractMacros_MultipleMacros(t *testing.T) {
	input := `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Info text.</p></ac:rich-text-body></ac:structured-macro>` +
		`<ac:structured-macro ac:name="toc"></ac:structured-macro>` +
		`<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Details</ac:parameter><ac:rich-text-body><p>Body.</p></ac:rich-text-body></ac:structured-macro>`

	result, registry := extractMacros(input, nil)

	assert.Contains(t, result, "MACROSENTINEL:m1:")
	assert.Contains(t, result, "MACROSENTINEL:m2:")
	assert.Contains(t, result, "MACROSENTINEL:m3:")

	require.NotNil(t, registry)
	require.Len(t, registry.Entries, 3)
	assert.Equal(t, "info", registry.Entries[0].Name)
	assert.Equal(t, "toc", registry.Entries[1].Name)
	assert.Equal(t, "expand", registry.Entries[2].Name)
}

func TestExtractMacros_NoMacros(t *testing.T) {
	input := `<p>Plain content with no macros.</p>`

	result, registry := extractMacros(input, nil)

	assert.Equal(t, input, result)
	assert.Nil(t, registry)
}

func TestExtractMacros_CodeMacro_NotExtracted(t *testing.T) {
	// Code macros are handled in Pass 0 (preprocessNonMacroElements) and never
	// reach extractMacros in normal pipeline execution. This unit test calls
	// extractMacros directly to verify the fallback behaviour: if a code macro
	// somehow arrived here, it would be classified as opaque (not lost).
	input := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[fmt.Println("hi")]]></ac:plain-text-body></ac:structured-macro>`

	_, registry := extractMacros(input, nil)

	// When code macros arrive at extractMacros directly, they are treated as opaque.
	require.NotNil(t, registry)
	assert.Equal(t, CategoryOpaque, registry.Entries[0].Category)
}


func TestRenumberMacroMarkers_DocumentOrder(t *testing.T) {
	md := "<!-- macro:m2 -->\n\n> **Info:** one\n\n" +
		"<!-- macro:m3 -->[Status]\n\n" +
		"<!-- macro:m1 -->\n\n> **Note:** two\n"
	registry := &MacroRegistry{Entries: []MacroEntry{
		{ID: "m1", Name: "note", Category: CategoryEditableFlat, OriginalXML: "<xml-m1/>"},
		{ID: "m2", Name: "info", Category: CategoryEditableFlat, OriginalXML: "<xml-m2/>"},
		{ID: "m3", Name: "status", Category: CategoryOpaque, OriginalXML: "<xml-m3/>"},
	}}

	got, reg := renumberMacroMarkers(md, registry)

	// First appearance in the document is m2 → m1, then m3 → m2, then m1 → m3.
	assert.Contains(t, got, "<!-- macro:m1 -->\n\n> **Info:** one")
	assert.Contains(t, got, "<!-- macro:m2 -->[Status]")
	assert.Contains(t, got, "<!-- macro:m3 -->\n\n> **Note:** two")

	require.Len(t, reg.Entries, 3)
	assert.Equal(t, "m1", reg.Entries[0].ID)
	assert.Equal(t, "info", reg.Entries[0].Name)
	assert.Equal(t, "m2", reg.Entries[1].ID)
	assert.Equal(t, "status", reg.Entries[1].Name)
	assert.Equal(t, "m3", reg.Entries[2].ID)
	assert.Equal(t, "note", reg.Entries[2].Name)
}

func TestRenumberMacroMarkers_StructuredEndMarker(t *testing.T) {
	md := "<!-- macro:m2 -->\n\n> **Info:** lead\n\n" +
		"<!-- macro:m1 -->**▶ Title**\n\nBody\n\n<!-- /macro:m1 -->\n"
	registry := &MacroRegistry{Entries: []MacroEntry{
		{ID: "m1", Name: "expand", Category: CategoryEditableStructured, OriginalXML: "<xml-m1/>"},
		{ID: "m2", Name: "info", Category: CategoryEditableFlat, OriginalXML: "<xml-m2/>"},
	}}

	got, reg := renumberMacroMarkers(md, registry)

	// Both open and close markers for the expand must be rewritten.
	assert.Contains(t, got, "<!-- macro:m2 -->**▶ Title**")
	assert.Contains(t, got, "<!-- /macro:m2 -->")
	assert.NotContains(t, got, "<!-- /macro:m1 -->")

	require.Len(t, reg.Entries, 2)
	assert.Equal(t, "m1", reg.Entries[0].ID)
	assert.Equal(t, "info", reg.Entries[0].Name)
	assert.Equal(t, "m2", reg.Entries[1].ID)
	assert.Equal(t, "expand", reg.Entries[1].Name)
}

func TestRenumberMacroMarkers_NilOrEmpty(t *testing.T) {
	md, reg := renumberMacroMarkers("no macros", nil)
	assert.Equal(t, "no macros", md)
	assert.Nil(t, reg)

	empty := &MacroRegistry{}
	md, reg = renumberMacroMarkers("no macros", empty)
	assert.Equal(t, "no macros", md)
	assert.Equal(t, empty, reg)
}

func TestToMarkdownWithMacros_InfoPanel(t *testing.T) {
	input := `<p>Before.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Important info.</p></ac:rich-text-body></ac:structured-macro><p>After.</p>`

	md, registry, _ := ToMarkdownWithMacros(input)

	assert.Contains(t, md, "Before.")
	assert.Contains(t, md, "After.")
	assert.Contains(t, md, "<!-- macro:m1 -->")
	assert.Contains(t, md, "Important info.")
	assert.NotContains(t, md, "ac:structured-macro")

	require.NotNil(t, registry)
	require.Len(t, registry.Entries, 1)
	assert.Equal(t, "info", registry.Entries[0].Name)
}

func TestToMarkdownWithMacros_Expand(t *testing.T) {
	input := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Click me</ac:parameter><ac:rich-text-body><p>Hidden content.</p></ac:rich-text-body></ac:structured-macro>`

	md, registry, _ := ToMarkdownWithMacros(input)

	// New format: macro comment + bold title line with ▶ prefix, then body, then end marker.
	assert.Contains(t, md, "<!-- macro:m1 -->")
	assert.Contains(t, md, "**▶ Click me**")
	assert.Contains(t, md, "Hidden content.")
	assert.Contains(t, md, "<!-- /macro:m1 -->")
	// Should NOT contain the legacy <details> format.
	assert.NotContains(t, md, "<details>")

	require.NotNil(t, registry)
	assert.Equal(t, CategoryEditableStructured, registry.Entries[0].Category)
}

func TestToMarkdownWithMacros_OpaqueStatus(t *testing.T) {
	input := `<ac:structured-macro ac:name="status"><ac:parameter ac:name="title">Done</ac:parameter><ac:parameter ac:name="colour">Green</ac:parameter></ac:structured-macro>`

	md, registry, _ := ToMarkdownWithMacros(input)

	assert.Contains(t, md, "<!-- macro:m1 -->")
	assert.Contains(t, md, "🟢 Done")

	require.NotNil(t, registry)
	assert.Equal(t, CategoryOpaque, registry.Entries[0].Category)
}

func TestToMarkdownWithMacros_NoMacros(t *testing.T) {
	input := "<p>Plain content</p>"

	md, registry, _ := ToMarkdownWithMacros(input)

	assert.Contains(t, md, "Plain content")
	assert.Nil(t, registry)
}

func TestToMarkdownWithMacros_MixedContent(t *testing.T) {
	input := `<h1>Title</h1>` +
		`<ac:structured-macro ac:name="toc"></ac:structured-macro>` +
		`<h2>Section</h2>` +
		`<ac:structured-macro ac:name="info"><ac:rich-text-body><p>A note.</p></ac:rich-text-body></ac:structured-macro>` +
		`<p>Regular text.</p>`

	md, registry, _ := ToMarkdownWithMacros(input)

	assert.Contains(t, md, "<!-- macro:m1 -->") // toc
	assert.Contains(t, md, "<!-- macro:m2 -->") // info
	assert.Contains(t, md, "# Title")
	assert.Contains(t, md, "## Section")
	assert.Contains(t, md, "A note.")
	assert.Contains(t, md, "Regular text.")

	require.NotNil(t, registry)
	assert.GreaterOrEqual(t, len(registry.Entries), 2)
}

func TestToMarkdownWithMacros_BackwardsCompatible(t *testing.T) {
	// ToMarkdown (without macros) should still work identically for non-macro content
	input := "<h1>Hello</h1><p>World</p>"

	mdOld := ToMarkdown(input)
	mdNew, registry, _ := ToMarkdownWithMacros(input)

	assert.Equal(t, mdOld, mdNew)
	assert.Nil(t, registry)
}

// stubResolver is a minimal mdconv.Resolver used in unit tests.
type stubResolver struct {
	users    map[string]string
	children []ChildPage
	err      error
}

func (s *stubResolver) ResolveUser(accountID string) (string, bool) {
	name, ok := s.users[accountID]
	return name, ok
}

func (s *stubResolver) ListChildren(_ string, _ int) ([]ChildPage, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.children, nil
}

func TestToMarkdownWithMacrosResolved_ExpandsMentionsAndChildren(t *testing.T) {
	input := `<p>Owner: <ac:link><ri:user ri:account-id="acc-1" /></ac:link></p>` +
		`<ac:structured-macro ac:name="children" ac:schema-version="2">` +
		`<ac:parameter ac:name="depth">2</ac:parameter>` +
		`</ac:structured-macro>`

	resolver := &stubResolver{
		users: map[string]string{"acc-1": "Alice Example"},
		children: []ChildPage{
			{
				ID:    "1001",
				Title: "First child",
				URL:   "https://example.atlassian.net/wiki/pages/viewpage.action?pageId=1001",
				Children: []ChildPage{
					{ID: "1002", Title: "Nested grandchild", URL: "https://example.atlassian.net/wiki/pages/viewpage.action?pageId=1002"},
				},
			},
			{ID: "1003", Title: "Second child"},
		},
	}

	md, registry, _ := ToMarkdownWithMacrosResolved(input, resolver)

	// Square brackets around the account id get escaped by the Markdown writer.
	assert.Contains(t, md, `@Alice Example \[acc-1]`, "mention should be expanded via resolver")
	assert.NotContains(t, md, "@user(acc-1)")

	assert.Contains(t, md, "[First child](https://example.atlassian.net/wiki/pages/viewpage.action?pageId=1001)")
	assert.Contains(t, md, "Nested grandchild")
	assert.Contains(t, md, "Second child")
	assert.NotContains(t, md, "[Child pages]", "children macro should be expanded, not fall back")

	require.NotNil(t, registry)
	// One macro (children). Mentions are not macros.
	require.Len(t, registry.Entries, 1)
	assert.Equal(t, "children", registry.Entries[0].Name)
}

func TestToMarkdownWithMacrosResolved_ChildrenFallsBackOnError(t *testing.T) {
	input := `<ac:structured-macro ac:name="children" ac:schema-version="2"></ac:structured-macro>`
	resolver := &stubResolver{err: errSentinel}

	md, _, _ := ToMarkdownWithMacrosResolved(input, resolver)
	assert.Contains(t, md, "[Child pages]")
}

var errSentinel = errorString("stub failure")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestOpaqueSummary(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{"toc", "", "Table of Contents"},
		{"status", `<ac:parameter ac:name="title">Done</ac:parameter><ac:parameter ac:name="colour">Green</ac:parameter>`, "Status: Done (Green)"},
		{"status", `<ac:parameter ac:name="title">Done</ac:parameter>`, "Status: Done"},
		{"status", `<ac:parameter ac:name="colour">Green</ac:parameter>`, "Status"},
		{"status", "", "Status"},
		{"code", "", "Code block"},
		{"noformat", "", "Preformatted text"},
		{"anchor", "", "Anchor"},
		{"anchor", `<ac:parameter ac:name="">top-of-fixture</ac:parameter>`, "Anchor: top-of-fixture"},
		{"children", "", "Child pages"},
		{"jira", `<ac:parameter ac:name="key">PROJ-42</ac:parameter>`, "Jira: PROJ-42"},
		{"jira", "", "Jira"},
		{"view-file", `<ac:parameter ac:name="name"><ri:attachment ri:filename="spec.pdf" /></ac:parameter>`, "File: spec.pdf"},
		{"view-file", "", "File"},
		{"custom-macro", `<p>Some visible text here</p>`, "custom-macro: Some visible text here"},
		{"long-text-macro", `<p>` + strings.Repeat("A", 50) + `</p>`, "long-text-macro: " + strings.Repeat("A", 40) + "..."},
		{"empty-body", "", "empty-body"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, opaqueSummary(tt.name, tt.body))
		})
	}
}

func TestSegmentMarkdown_NoMacros(t *testing.T) {
	md := "# Hello\n\nSome text.\n"
	segments := segmentMarkdown(md)

	require.Len(t, segments, 1)
	assert.Equal(t, "plain", segments[0].Type)
	assert.Equal(t, md, segments[0].Content)
}

func TestSegmentMarkdown_SingleOpaque(t *testing.T) {
	md := "Before.\n\n<!-- macro:m1 --> **Status: Done**\n\nAfter.\n"
	segments := segmentMarkdown(md)

	require.Len(t, segments, 3)
	assert.Equal(t, "plain", segments[0].Type)
	assert.Contains(t, segments[0].Content, "Before.")

	assert.Equal(t, "macro", segments[1].Type)
	assert.Equal(t, "m1", segments[1].MacroID)

	assert.Equal(t, "plain", segments[2].Type)
	assert.Contains(t, segments[2].Content, "After.")
}

func TestSegmentMarkdown_EditableFlat(t *testing.T) {
	md := "Before.\n\n<!-- macro:m1 -->\n> **Info:** Important text.\n\nAfter.\n"
	segments := segmentMarkdown(md)

	require.Len(t, segments, 3)
	assert.Equal(t, "macro", segments[1].Type)
	assert.Equal(t, "m1", segments[1].MacroID)
	assert.Contains(t, segments[1].Content, "> **Info:**")
}

func TestSegmentMarkdown_EditableStructured_Legacy(t *testing.T) {
	// Legacy <details>/<summary> format — must still be segmented correctly.
	md := "Before.\n\n<!-- macro:m1 -->\n<details>\n<summary>Title</summary>\n\nBody content.\n\n</details>\n\nAfter.\n"
	segments := segmentMarkdown(md)

	require.Len(t, segments, 3)
	assert.Equal(t, "macro", segments[1].Type)
	assert.Equal(t, "m1", segments[1].MacroID)
	assert.Contains(t, segments[1].Content, "<details>")
	assert.Contains(t, segments[1].Content, "</details>")
}

func TestSegmentMarkdown_EditableStructured_NewFormat(t *testing.T) {
	// New **▶ title** format — macro block must be segmented correctly.
	// The expand block ends at the <!-- /macro:m1 --> end marker so headings
	// inside the body do not prematurely terminate the segment.
	md := "Before.\n\n<!-- macro:m1 -->**▶ My Title**\n\nBody paragraph.\n\n## Subhead\n\n<!-- /macro:m1 -->\n\nAfter.\n"
	segments := segmentMarkdown(md)

	require.Len(t, segments, 3)
	assert.Equal(t, "macro", segments[1].Type)
	assert.Equal(t, "m1", segments[1].MacroID)
	assert.Contains(t, segments[1].Content, "**▶ My Title**")
	assert.Contains(t, segments[1].Content, "Body paragraph.")
	// The subheading is inside the expand body (before the end marker).
	assert.Contains(t, segments[1].Content, "## Subhead")
	// Everything after the end marker is in the trailing plain segment.
	assert.Equal(t, "plain", segments[2].Type)
	assert.Contains(t, segments[2].Content, "After.")
}

func TestSegmentMarkdown_MultipleOpaqueOnSameLine(t *testing.T) {
	md := "Status: <!-- macro:m1 --> [In Progress] <!-- macro:m2 --> [Done] <!-- macro:m3 --> [Blocked]\n"
	segments := segmentMarkdown(md)

	macroCount := 0
	for _, s := range segments {
		if s.Type == "macro" {
			macroCount++
		}
	}
	assert.Equal(t, 3, macroCount, "expected 3 macro segments from same-line opaque macros")

	// Verify each macro ID is captured
	macroIDs := make(map[string]bool)
	for _, s := range segments {
		if s.Type == "macro" {
			macroIDs[s.MacroID] = true
		}
	}
	assert.True(t, macroIDs["m1"])
	assert.True(t, macroIDs["m2"])
	assert.True(t, macroIDs["m3"])
}

func TestSegmentMarkdown_MultipleMacros(t *testing.T) {
	md := "<!-- macro:m1 --> **[Table of Contents]**\n\n# Title\n\nText.\n\n<!-- macro:m2 -->\n> **Info:** Note.\n\n<!-- macro:m3 --> **Status: Open**\n"
	segments := segmentMarkdown(md)

	macroCount := 0
	for _, s := range segments {
		if s.Type == "macro" {
			macroCount++
		}
	}
	assert.Equal(t, 3, macroCount)
}

func TestRestoreMacro_EditableFlat(t *testing.T) {
	entry := &MacroEntry{
		ID: "m1", Name: "info", Category: CategoryEditableFlat,
		OriginalXML: `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Old text.</p></ac:rich-text-body></ac:structured-macro>`,
	}
	content := "<!-- macro:m1 -->\n> **Info:** Updated text."

	result := restoreMacro(entry, content)

	assert.Contains(t, result, `ac:name="info"`)
	assert.Contains(t, result, "<ac:rich-text-body>")
	assert.Contains(t, result, "Updated text.")
	assert.NotContains(t, result, "Old text.")
}

func TestRestoreMacro_EditableStructured(t *testing.T) {
	// Legacy <details>/<summary> format — must still be restorable.
	entry := &MacroEntry{
		ID: "m1", Name: "expand", Category: CategoryEditableStructured,
		OriginalXML: `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Old Title</ac:parameter><ac:rich-text-body><p>Old body.</p></ac:rich-text-body></ac:structured-macro>`,
	}
	content := "<!-- macro:m1 -->\n<details>\n<summary>New Title</summary>\n\nNew body.\n\n</details>"

	result := restoreMacro(entry, content)

	assert.Contains(t, result, `ac:name="expand"`)
	assert.Contains(t, result, `ac:name="title">New Title</ac:parameter>`)
	assert.Contains(t, result, "New body.")
	assert.NotContains(t, result, "Old Title")
	assert.NotContains(t, result, "Old body.")
}

func TestRestoreMacro_EditableStructured_NewFormat(t *testing.T) {
	// New **▶ title** format — must be restorable.
	entry := &MacroEntry{
		ID: "m1", Name: "expand", Category: CategoryEditableStructured,
		OriginalXML: `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Old Title</ac:parameter><ac:rich-text-body><p>Old body.</p></ac:rich-text-body></ac:structured-macro>`,
	}
	// Segment content as produced by findMacroBlockEnd + segmentMarkdown.
	content := "<!-- macro:m1 -->\n**▶ New Title**\n\nNew body.\n"

	result := restoreMacro(entry, content)

	assert.Contains(t, result, `ac:name="expand"`)
	assert.Contains(t, result, `ac:name="title">New Title</ac:parameter>`)
	assert.Contains(t, result, "New body.")
	assert.NotContains(t, result, "Old Title")
	assert.NotContains(t, result, "Old body.")
}

func TestRestoreMacro_Opaque(t *testing.T) {
	originalXML := `<ac:structured-macro ac:name="toc"><ac:parameter ac:name="printable">true</ac:parameter></ac:structured-macro>`
	entry := &MacroEntry{
		ID: "m1", Name: "toc", Category: CategoryOpaque,
		OriginalXML: originalXML,
	}
	content := "<!-- macro:m1 --> *[Table of Contents]*"

	result := restoreMacro(entry, content)

	assert.Equal(t, originalXML, result)
}

func TestToStorageFormatWithMacros_NilRegistry(t *testing.T) {
	md := "Hello world"
	result := ToStorageFormatWithMacros(md, nil)
	assert.Equal(t, ToStorageFormat(md), result)
}

func TestToStorageFormatWithMacros_NoMacroComments(t *testing.T) {
	md := "# Title\n\nSome text."
	registry := &MacroRegistry{
		Entries: []MacroEntry{{ID: "m1", Name: "info", Category: CategoryEditableFlat, OriginalXML: "<ignored/>"}},
	}
	result := ToStorageFormatWithMacros(md, registry)
	assert.Equal(t, ToStorageFormat(md), result)
}

func TestToStorageFormatWithMacros_InfoPanel(t *testing.T) {
	registry := &MacroRegistry{
		Entries: []MacroEntry{{
			ID: "m1", Name: "info", Category: CategoryEditableFlat,
			OriginalXML: `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Old.</p></ac:rich-text-body></ac:structured-macro>`,
		}},
	}
	md := "Before.\n\n<!-- macro:m1 -->\n> **Info:** Updated.\n\nAfter."

	result := ToStorageFormatWithMacros(md, registry)

	assert.Contains(t, result, `ac:name="info"`)
	assert.Contains(t, result, "Updated.")
	assert.Contains(t, result, "Before.")
	assert.Contains(t, result, "After.")
	assert.NotContains(t, result, "Old.")
}

func TestToStorageFormatWithMacros_OpaqueVerbatim(t *testing.T) {
	originalXML := `<ac:structured-macro ac:name="toc"><ac:parameter ac:name="printable">true</ac:parameter></ac:structured-macro>`
	registry := &MacroRegistry{
		Entries: []MacroEntry{{
			ID: "m1", Name: "toc", Category: CategoryOpaque,
			OriginalXML: originalXML,
		}},
	}
	md := "# Title\n\n<!-- macro:m1 --> *[Table of Contents]*\n\nContent."

	result := ToStorageFormatWithMacros(md, registry)

	assert.Contains(t, result, originalXML)
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "Content.")
}

func TestToStorageFormatWithMacros_UnknownMacroID(t *testing.T) {
	registry := &MacroRegistry{
		Entries: []MacroEntry{{ID: "m1", Name: "info", Category: CategoryEditableFlat, OriginalXML: "<original/>"}},
	}
	md := "<!-- macro:m99 --> Some text."

	result := ToStorageFormatWithMacros(md, registry)

	assert.NotEmpty(t, result)
}

func TestMacroRoundTrip_InfoPanel(t *testing.T) {
	original := `<p>Before.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Important info.</p></ac:rich-text-body></ac:structured-macro><p>After.</p>`

	md, registry, _ := ToMarkdownWithMacros(original)
	require.NotNil(t, registry)

	restored := ToStorageFormatWithMacros(md, registry)

	assert.Contains(t, restored, `ac:name="info"`)
	assert.Contains(t, restored, "Important info.")
	assert.Contains(t, restored, "Before.")
	assert.Contains(t, restored, "After.")
}

func TestMacroRoundTrip_Expand(t *testing.T) {
	original := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Click me</ac:parameter><ac:rich-text-body><p>Hidden content.</p></ac:rich-text-body></ac:structured-macro>`

	md, registry, _ := ToMarkdownWithMacros(original)
	require.NotNil(t, registry)

	restored := ToStorageFormatWithMacros(md, registry)

	assert.Contains(t, restored, `ac:name="expand"`)
	assert.Contains(t, restored, "Click me")
	assert.Contains(t, restored, "Hidden content.")
}

func TestMacroRoundTrip_Opaque_Verbatim(t *testing.T) {
	original := `<ac:structured-macro ac:name="toc"><ac:parameter ac:name="printable">true</ac:parameter><ac:parameter ac:name="style">disc</ac:parameter></ac:structured-macro>`

	md, registry, _ := ToMarkdownWithMacros(original)
	require.NotNil(t, registry)

	restored := ToStorageFormatWithMacros(md, registry)

	// Opaque macros must be restored byte-for-byte.
	assert.Contains(t, restored, original)
}

func TestMacroRoundTrip_MixedPage(t *testing.T) {
	original := `<h1>Guide</h1>` +
		`<ac:structured-macro ac:name="toc"></ac:structured-macro>` +
		`<h2>Overview</h2>` +
		`<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Read this first.</p></ac:rich-text-body></ac:structured-macro>` +
		`<p>Regular paragraph.</p>` +
		`<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Details</ac:parameter><ac:rich-text-body><p>Expanded content.</p></ac:rich-text-body></ac:structured-macro>` +
		`<ac:structured-macro ac:name="status"><ac:parameter ac:name="title">Active</ac:parameter><ac:parameter ac:name="colour">Green</ac:parameter></ac:structured-macro>`

	md, registry, _ := ToMarkdownWithMacros(original)
	require.NotNil(t, registry)
	assert.Len(t, registry.Entries, 4)

	restored := ToStorageFormatWithMacros(md, registry)

	assert.Contains(t, restored, `ac:name="toc"`)
	assert.Contains(t, restored, `ac:name="info"`)
	assert.Contains(t, restored, `ac:name="expand"`)
	assert.Contains(t, restored, `ac:name="status"`)
	assert.Contains(t, restored, "Guide")
	assert.Contains(t, restored, "Overview")
	assert.Contains(t, restored, "Regular paragraph.")
}

func TestMacroRoundTrip_Stability(t *testing.T) {
	original := `<p>Text.</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>Note.</p></ac:rich-text-body></ac:structured-macro><p>More text.</p>`

	md1, reg1, _ := ToMarkdownWithMacros(original)
	storage1 := ToStorageFormatWithMacros(md1, reg1)

	md2, reg2, _ := ToMarkdownWithMacros(storage1)
	storage2 := ToStorageFormatWithMacros(md2, reg2)

	assert.Equal(t, storage1, storage2, "round-trip not stable after two cycles")
}

func TestMacroRoundTrip_AnonymisedFixtures(t *testing.T) {
	testdataDir := "testdata"
	entries, err := os.ReadDir(testdataDir)
	if err != nil {
		t.Skipf("testdata directory not found: %v", err)
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "anonymised-") || !strings.HasSuffix(entry.Name(), ".json.gz") {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(testdataDir, entry.Name())
			fixture, err := testgen.LoadFixture(path)
			require.NoError(t, err)
			require.NotEmpty(t, fixture.Content)

			md, registry, _ := ToMarkdownWithMacros(fixture.Content)
			require.NotEmpty(t, md)

			if registry == nil {
				t.Logf("%s: no macros found, skipping macro round-trip", fixture.Name)
				return
			}

			t.Logf("%s: %d macros extracted", fixture.Name, len(registry.Entries))
			for _, e := range registry.Entries {
				t.Logf("  %s: %s (%s)", e.ID, e.Name, e.Category)
			}

			restored := ToStorageFormatWithMacros(md, registry)
			_, registry2, _ := ToMarkdownWithMacros(restored)

			if registry2 == nil {
				t.Errorf("%s: macros lost after round-trip (had %d, now 0)", fixture.Name, len(registry.Entries))
				return
			}

			assert.Equal(t, len(registry.Entries), len(registry2.Entries),
				"%s: macro count changed after round-trip", fixture.Name)

			for i := range registry.Entries {
				if i < len(registry2.Entries) {
					assert.Equal(t, registry.Entries[i].Name, registry2.Entries[i].Name,
						"%s: macro %d name changed", fixture.Name, i)
				}
			}
		})
	}
}

// TestMacroRoundTrip_Expand_HeadingInBody verifies that an expand macro whose body
// contains a heading round-trips correctly — the heading must remain inside the macro,
// not float out as top-level content.
func TestMacroRoundTrip_Expand_HeadingInBody(t *testing.T) {
	original := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Details</ac:parameter><ac:rich-text-body><h2>Subhead</h2><p>Body para.</p></ac:rich-text-body></ac:structured-macro>`

	md, registry, _ := ToMarkdownWithMacros(original)
	require.NotNil(t, registry)

	// The Markdown must contain the heading and body inside the expand block,
	// before the end marker.
	assert.Contains(t, md, "<!-- macro:m1 -->")
	assert.Contains(t, md, "<!-- /macro:m1 -->")
	endMarkerIdx := strings.Index(md, "<!-- /macro:m1 -->")
	subheadIdx := strings.Index(md, "## Subhead")
	bodyIdx := strings.Index(md, "Body para.")
	assert.Greater(t, endMarkerIdx, subheadIdx, "subhead must appear before end marker")
	assert.Greater(t, endMarkerIdx, bodyIdx, "body para must appear before end marker")

	restored := ToStorageFormatWithMacros(md, registry)

	assert.Contains(t, restored, `ac:name="expand"`)
	assert.Contains(t, restored, "Subhead")
	assert.Contains(t, restored, "Body para.")
}

// TestRestoreMacro_EditableStructured_NewFormat_TitleWithAsterisks verifies that a
// title containing internal bold markup (e.g. "Say **hello** world") round-trips
// without truncation.
func TestRestoreMacro_EditableStructured_NewFormat_TitleWithAsterisks(t *testing.T) {
	entry := &MacroEntry{
		ID: "m1", Name: "expand", Category: CategoryEditableStructured,
		OriginalXML: `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Old Title</ac:parameter><ac:rich-text-body><p>Body.</p></ac:rich-text-body></ac:structured-macro>`,
	}
	// Segment content as the user might edit it — title with internal bold markers.
	content := "<!-- macro:m1 -->**▶ Say **hello** world**\n\nBody.\n\n<!-- /macro:m1 -->"

	result := restoreMacro(entry, content)

	assert.Contains(t, result, `ac:name="expand"`)
	// Title must be the full string including the internal asterisks.
	assert.Contains(t, result, `ac:name="title">Say **hello** world</ac:parameter>`)
	assert.NotContains(t, result, "Old Title")
}

// TestMacroRoundTrip_Expand_TwoExpandsInARow verifies that two expand macros in
// sequence each retain their own body without content from one leaking into the other.
func TestFindMatchingClose(t *testing.T) {
	t.Run("balanced single", func(t *testing.T) {
		// One open already consumed; one close in s.
		s := "body content</ac:structured-macro>trailing"
		result := findMatchingClose(s, 0)
		// Should return position right after </ac:structured-macro>
		expected := len("body content</ac:structured-macro>")
		assert.Equal(t, expected, result)
	})

	t.Run("nested balanced", func(t *testing.T) {
		// Caller consumed first open. s has: inner open, inner close, outer close.
		s := "<ac:structured-macro ac:name=\"inner\">inner body</ac:structured-macro></ac:structured-macro>tail"
		result := findMatchingClose(s, 0)
		// Should skip the inner pair and return after the second </ac:structured-macro>.
		// Find position after the first close (which brings inner depth to 0 but outer depth to 1 still).
		firstClosePos := strings.Index(s, "</ac:structured-macro>") + len("</ac:structured-macro>")
		secondClose := strings.Index(s[firstClosePos:], "</ac:structured-macro>")
		expected := firstClosePos + secondClose + len("</ac:structured-macro>")
		assert.Equal(t, expected, result)
	})

	t.Run("unbalanced no close", func(t *testing.T) {
		s := "<ac:structured-macro ac:name=\"orphan\">no close tag here"
		result := findMatchingClose(s, 0)
		assert.Equal(t, -1, result)
	})

	t.Run("no open or close after cursor returns -1", func(t *testing.T) {
		// depth=1 from caller, no close tag in s at all.
		s := "just plain text with no macro tags"
		result := findMatchingClose(s, 0)
		assert.Equal(t, -1, result)
	})

	t.Run("close immediately at cursor", func(t *testing.T) {
		s := "</ac:structured-macro>rest"
		result := findMatchingClose(s, 0)
		assert.Equal(t, len("</ac:structured-macro>"), result)
	})
}

func TestExtractMacros_NestedMacros(t *testing.T) {
	t.Run("info panel containing expand", func(t *testing.T) {
		input := `<ac:structured-macro ac:name="info">` +
			`<ac:rich-text-body>` +
			`<p>Before inner.</p>` +
			`<ac:structured-macro ac:name="expand">` +
			`<ac:parameter ac:name="title">Details</ac:parameter>` +
			`<ac:rich-text-body><p>Expanded.</p></ac:rich-text-body>` +
			`</ac:structured-macro>` +
			`<p>After inner.</p>` +
			`</ac:rich-text-body>` +
			`</ac:structured-macro>`

		result, registry := extractMacros(input, nil)

		// Registry should have 2 entries: inner expand (extracted first) and outer info.
		require.NotNil(t, registry)
		require.Len(t, registry.Entries, 2)

		// Inner expand is registered first (depth-first recursion).
		expandEntry := registry.Entries[0]
		assert.Equal(t, "expand", expandEntry.Name)
		assert.Equal(t, CategoryEditableStructured, expandEntry.Category)
		assert.Contains(t, expandEntry.OriginalXML, `ac:name="expand"`)
		assert.Contains(t, expandEntry.OriginalXML, "Expanded.")

		// Outer info is registered after inner.
		infoEntry := registry.Entries[1]
		assert.Equal(t, "info", infoEntry.Name)
		assert.Equal(t, CategoryEditableFlat, infoEntry.Category)

		// Outer info's OriginalXML must be the full verbatim XML including the nested expand.
		assert.Contains(t, infoEntry.OriginalXML, `ac:name="info"`)
		assert.Contains(t, infoEntry.OriginalXML, `ac:name="expand"`)
		assert.Contains(t, infoEntry.OriginalXML, "Before inner.")
		assert.Contains(t, infoEntry.OriginalXML, "After inner.")
		assert.Contains(t, infoEntry.OriginalXML, "Expanded.")

		// The result must not contain raw ac:structured-macro tags.
		assert.NotContains(t, result, "ac:structured-macro")

		// The outer info placeholder must contain sentinels for both macros.
		// The inner expand placeholder appears inside the outer info body.
		assert.Contains(t, result, "MACROSENTINEL:"+infoEntry.ID+":")
		assert.Contains(t, result, "MACROSENTINEL:"+expandEntry.ID+":")

		// Body content should be visible.
		assert.Contains(t, result, "Before inner.")
		assert.Contains(t, result, "After inner.")
		assert.Contains(t, result, "Details") // expand title
		assert.Contains(t, result, "Expanded.")
	})

	t.Run("info panel containing pre/code non-macro content", func(t *testing.T) {
		// Non-macro tags inside the outer body do not affect depth balancing.
		input := `<ac:structured-macro ac:name="info">` +
			`<ac:rich-text-body>` +
			`<p>Text.</p>` +
			`<pre><code>some code</code></pre>` +
			`</ac:rich-text-body>` +
			`</ac:structured-macro>`

		result, registry := extractMacros(input, nil)

		// Only 1 entry — the outer info. pre/code is not a structured macro.
		require.NotNil(t, registry)
		require.Len(t, registry.Entries, 1)
		assert.Equal(t, "info", registry.Entries[0].Name)

		// Result should not contain ac:structured-macro.
		assert.NotContains(t, result, "ac:structured-macro")
		// The code content should be visible in the info placeholder body.
		assert.Contains(t, result, "some code")
	})
}

func TestMacroRoundTrip_Expand_TwoExpandsInARow(t *testing.T) {
	original := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">First</ac:parameter><ac:rich-text-body><p>First body.</p></ac:rich-text-body></ac:structured-macro>` +
		`<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Second</ac:parameter><ac:rich-text-body><p>Second body.</p></ac:rich-text-body></ac:structured-macro>`

	md, registry, _ := ToMarkdownWithMacros(original)
	require.NotNil(t, registry)
	require.Len(t, registry.Entries, 2)

	// Each expand must have its own open and close markers.
	assert.Contains(t, md, "<!-- macro:m1 -->")
	assert.Contains(t, md, "<!-- /macro:m1 -->")
	assert.Contains(t, md, "<!-- macro:m2 -->")
	assert.Contains(t, md, "<!-- /macro:m2 -->")

	restored := ToStorageFormatWithMacros(md, registry)

	// Split the restored XML at the boundary between the two macros and verify
	// that each body appears inside its own macro, not cross-contaminated.
	firstClose := strings.Index(restored, "</ac:structured-macro>")
	require.NotEqual(t, -1, firstClose, "expected first macro close tag")
	firstMacro := restored[:firstClose]
	secondMacro := restored[firstClose:]

	assert.Contains(t, firstMacro, "First body.")
	assert.NotContains(t, firstMacro, "Second body.")
	assert.Contains(t, secondMacro, "Second body.")
	assert.NotContains(t, secondMacro, "First body.")
}
