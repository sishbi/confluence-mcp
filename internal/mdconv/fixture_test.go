package mdconv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFixtureAllElementsRoundtrip loads the anonymised storage-format fixture
// covering every supported Confluence element and verifies:
//   - critical structural conversions landed (tables with colspan/rowspan,
//     task-list checkboxes, no lingering sentinels or THE END comments);
//   - the full output matches the committed golden Markdown file.
//
// Regenerate the golden after an intentional converter change:
//
//	UPDATE_GOLDEN=1 go test ./internal/mdconv/ -run TestFixtureAllElementsRoundtrip
func TestFixtureAllElementsRoundtrip(t *testing.T) {
	xmlPath := filepath.Join("testdata", "fixture-all-elements.xml")
	goldenPath := filepath.Join("testdata", "fixture-all-elements.md")

	storage, err := os.ReadFile(xmlPath)
	require.NoError(t, err, "fixture not found; see plan 2026-04-17-fixture-review-fixes")

	// Exercise the full macro-aware pipeline — that is what
	// confluencemcp.tool_read uses when rendering pages, so the fixture golden
	// must reflect that path (not the simpler ToMarkdown).
	got, _, _ := ToMarkdownWithMacros(string(storage))

	// Must-have conversions regardless of golden drift.
	t.Run("no library artifacts", func(t *testing.T) {
		assert.NotContains(t, got, "<!--THE END-->")
		assert.NotContains(t, got, "MDTABLE", "table sentinels must be substituted back")
	})

	t.Run("task list checkboxes", func(t *testing.T) {
		assert.Contains(t, got, "[ ]", "unchecked task marker should survive")
		assert.Contains(t, got, "[x]", "checked task marker should survive")
		// Ensure the library did not re-escape the brackets.
		assert.NotContains(t, got, `\[x\]`)
		assert.NotContains(t, got, `\[ \]`)
	})

	t.Run("tables", func(t *testing.T) {
		assert.Contains(t, got, "| --- |", "at least one GFM table header separator")
		assert.Contains(t, got, "(spans 2 cols)", "colspan annotation from rewriteTables")
		assert.Contains(t, got, "⬆", "rowspan fill marker")
	})

	t.Run("inline content preserved", func(t *testing.T) {
		assert.Contains(t, got, "2024-09-01", "date node datetime attribute")
		assert.Contains(t, got, "npm install", "inline code content")
	})

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o644))
		t.Logf("updated golden: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o644))
		t.Fatalf("golden %s did not exist; wrote current output. "+
			"Review the file and re-run the test.", goldenPath)
	}
	require.NoError(t, err)

	if string(want) != got {
		// Emit a compact unified-ish diff summary to help triage without
		// drowning the test log in a 20 KB blob.
		t.Errorf("fixture Markdown does not match golden %s\n"+
			"want %d bytes, got %d bytes\n"+
			"first diff at byte %d\n"+
			"re-run with UPDATE_GOLDEN=1 to accept the new output",
			goldenPath, len(want), len(got), firstDiffOffset(string(want), got))
	}
}

// firstDiffOffset returns the byte index of the first mismatch between a and
// b, or -1 when identical.
func firstDiffOffset(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

