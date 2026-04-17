package mdconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sishbi/confluence-mcp/internal/mdconv/testgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToStorageFormat_EmptyString(t *testing.T) {
	assert.Equal(t, "", ToStorageFormat(""))
}

func TestToStorageFormat_Paragraph(t *testing.T) {
	result := ToStorageFormat("Hello world")
	assert.Equal(t, "<p>Hello world</p>\n", result)
}

func TestToStorageFormat_Heading(t *testing.T) {
	assert.Equal(t, "<h1>Title</h1>\n", ToStorageFormat("# Title"))
	assert.Equal(t, "<h2>Sub</h2>\n", ToStorageFormat("## Sub"))
	assert.Equal(t, "<h3>Deep</h3>\n", ToStorageFormat("### Deep"))
}

func TestToStorageFormat_Bold(t *testing.T) {
	result := ToStorageFormat("Hello **bold** world")
	assert.Equal(t, "<p>Hello <strong>bold</strong> world</p>\n", result)
}

func TestToStorageFormat_Italic(t *testing.T) {
	result := ToStorageFormat("Hello *italic* world")
	assert.Equal(t, "<p>Hello <em>italic</em> world</p>\n", result)
}

func TestToStorageFormat_InlineCode(t *testing.T) {
	result := ToStorageFormat("Use `fmt.Println` here")
	assert.Equal(t, "<p>Use <code>fmt.Println</code> here</p>\n", result)
}

func TestToStorageFormat_Link(t *testing.T) {
	result := ToStorageFormat("Click [here](https://example.com)")
	assert.Equal(t, "<p>Click <a href=\"https://example.com\">here</a></p>\n", result)
}

func TestToStorageFormat_BulletList(t *testing.T) {
	result := ToStorageFormat("- one\n- two\n- three")
	assert.Contains(t, result, "<ul>")
	assert.Contains(t, result, "<li>one</li>")
	assert.Contains(t, result, "<li>two</li>")
	assert.Contains(t, result, "<li>three</li>")
	assert.Contains(t, result, "</ul>")
}

func TestToStorageFormat_OrderedList(t *testing.T) {
	result := ToStorageFormat("1. first\n2. second")
	assert.Contains(t, result, "<ol>")
	assert.Contains(t, result, "<li>first</li>")
	assert.Contains(t, result, "<li>second</li>")
	assert.Contains(t, result, "</ol>")
}

func TestToStorageFormat_FencedCodeBlock(t *testing.T) {
	result := ToStorageFormat("```go\nfmt.Println(\"hi\")\n```")
	assert.Contains(t, result, `ac:name="code"`)
	assert.Contains(t, result, "go")
	assert.Contains(t, result, `fmt.Println("hi")`)
}

func TestToStorageFormat_FencedCodeBlock_NoLanguage(t *testing.T) {
	result := ToStorageFormat("```\nsome code\n```")
	assert.Contains(t, result, `ac:name="code"`)
	assert.Contains(t, result, "some code")
}

func TestToStorageFormat_Blockquote(t *testing.T) {
	result := ToStorageFormat("> quoted text")
	assert.Contains(t, result, "<blockquote>")
	assert.Contains(t, result, "quoted text")
	assert.Contains(t, result, "</blockquote>")
}

func TestToStorageFormat_ThematicBreak(t *testing.T) {
	result := ToStorageFormat("above\n\n---\n\nbelow")
	assert.Contains(t, result, "<hr")
}

func TestToStorageFormat_Image(t *testing.T) {
	result := ToStorageFormat("![alt text](https://example.com/img.png)")
	assert.Contains(t, result, "<ac:image>")
	assert.Contains(t, result, "https://example.com/img.png")
}

func TestToStorageFormat_Table(t *testing.T) {
	md := "| A | B |\n|---|---|\n| 1 | 2 |"
	result := ToStorageFormat(md)
	assert.Contains(t, result, "<table>")
	assert.Contains(t, result, "<th>")
	assert.Contains(t, result, "</table>")
}

func TestToMarkdown_EmptyString(t *testing.T) {
	assert.Equal(t, "", ToMarkdown(""))
}

func TestToMarkdown_Paragraph(t *testing.T) {
	result := ToMarkdown("<p>Hello world</p>")
	assert.Equal(t, "Hello world", strings.TrimSpace(result))
}

func TestToMarkdown_Heading(t *testing.T) {
	assert.Contains(t, ToMarkdown("<h1>Title</h1>"), "# Title")
	assert.Contains(t, ToMarkdown("<h2>Sub</h2>"), "## Sub")
}

func TestToMarkdown_Bold(t *testing.T) {
	result := ToMarkdown("<p>Hello <strong>bold</strong> world</p>")
	assert.Contains(t, result, "**bold**")
}

func TestToMarkdown_Italic(t *testing.T) {
	result := ToMarkdown("<p>Hello <em>italic</em> world</p>")
	assert.Contains(t, result, "*italic*")
}

func TestToMarkdown_InlineCode(t *testing.T) {
	result := ToMarkdown("<p>Use <code>fmt.Println</code> here</p>")
	assert.Contains(t, result, "`fmt.Println`")
}

func TestToMarkdown_Link(t *testing.T) {
	result := ToMarkdown(`<p>Click <a href="https://example.com">here</a></p>`)
	assert.Contains(t, result, "[here](https://example.com)")
}

func TestToMarkdown_BulletList(t *testing.T) {
	result := ToMarkdown("<ul><li>one</li><li>two</li></ul>")
	assert.Contains(t, result, "one")
	assert.Contains(t, result, "two")
}

func TestToMarkdown_OrderedList(t *testing.T) {
	result := ToMarkdown("<ol><li>first</li><li>second</li></ol>")
	assert.Contains(t, result, "first")
	assert.Contains(t, result, "second")
}

func TestToMarkdown_CodeMacro(t *testing.T) {
	input := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[fmt.Println("hi")]]></ac:plain-text-body></ac:structured-macro>`
	result := ToMarkdown(input)
	assert.Contains(t, result, "```go")
	assert.Contains(t, result, `fmt.Println("hi")`)
	assert.Contains(t, result, "```")
}

func TestToMarkdown_CodeMacro_NoLanguage(t *testing.T) {
	input := `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[some code]]></ac:plain-text-body></ac:structured-macro>`
	result := ToMarkdown(input)
	assert.Contains(t, result, "```")
	assert.Contains(t, result, "some code")
}

func TestToMarkdown_Blockquote(t *testing.T) {
	result := ToMarkdown("<blockquote><p>quoted text</p></blockquote>")
	assert.Contains(t, result, "> quoted text")
}

func TestToMarkdown_Strikethrough(t *testing.T) {
	for _, tag := range []string{"del", "s", "strike"} {
		result := ToMarkdown("<p>before <" + tag + ">gone</" + tag + "> after</p>")
		assert.Contains(t, result, "~~gone~~", "tag %s", tag)
	}
}

func TestToMarkdown_UnderlinePreserved(t *testing.T) {
	result := ToMarkdown("<p>before <u>under</u> after</p>")
	assert.Contains(t, result, "<u>under</u>")
}

func TestToStorageFormat_Strikethrough(t *testing.T) {
	result := ToStorageFormat("plain ~~struck~~ end")
	assert.Contains(t, result, "<s>struck</s>")
}

func TestToStorageFormat_UnderlinePassthrough(t *testing.T) {
	result := ToStorageFormat("plain <u>under</u> end")
	assert.Contains(t, result, "<u>under</u>")
}

func TestToMarkdown_ADFPanelRoundtrip(t *testing.T) {
	input := `<ac:adf-extension><ac:adf-node type="panel">` +
		`<ac:adf-attribute key="panel-type">note</ac:adf-attribute>` +
		`<ac:adf-content><p>body text</p></ac:adf-content>` +
		`</ac:adf-node></ac:adf-extension>`

	md, registry, _ := ToMarkdownWithMacros(input)
	assert.Contains(t, md, "<!-- macro:m")
	// ADF note panels render as GFM alerts (`> [!NOTE]`).
	assert.Contains(t, md, "[!NOTE]")
	require.NotNil(t, registry)
	require.Len(t, registry.Entries, 1)
	assert.Equal(t, "adf-panel", registry.Entries[0].Name)

	storage := ToStorageFormatWithMacros(md, registry)
	assert.Contains(t, storage, "ac:adf-extension")
	assert.Contains(t, storage, `key="panel-type"`)
	assert.Contains(t, storage, "note")
}

func TestToMarkdown_UnknownMacro(t *testing.T) {
	input := `<ac:structured-macro ac:name="toc"><ac:parameter ac:name="maxLevel">3</ac:parameter></ac:structured-macro>`
	result := ToMarkdown(input)
	assert.NotContains(t, result, "<ac:")
}

func TestToMarkdown_UnknownHTMLTag(t *testing.T) {
	result := ToMarkdown("<div><p>inner text</p></div>")
	assert.Contains(t, result, "inner text")
}

func TestToMarkdown_UserMention(t *testing.T) {
	input := `<ac:link><ri:user ri:account-id="abc123" /><ac:plain-text-link-body>John Doe</ac:plain-text-link-body></ac:link>`
	result := ToMarkdown(input)
	assert.Contains(t, result, "John Doe")
}

func TestToMarkdown_Image(t *testing.T) {
	input := `<ac:image><ri:url ri:value="https://example.com/img.png" /></ac:image>`
	result := ToMarkdown(input)
	assert.Contains(t, result, "https://example.com/img.png")
}

func TestRoundTrip_Paragraph(t *testing.T) {
	md := "Hello world"
	result := ToMarkdown(ToStorageFormat(md))
	assert.Equal(t, md, strings.TrimSpace(result))
}

func TestRoundTrip_HeadingAndParagraph(t *testing.T) {
	md := "# Title\n\nSome text below."
	result := ToMarkdown(ToStorageFormat(md))
	assert.Contains(t, result, "# Title")
	assert.Contains(t, result, "Some text below.")
}

func TestRoundTrip_BoldAndItalic(t *testing.T) {
	md := "Hello **bold** and *italic* world"
	result := ToMarkdown(ToStorageFormat(md))
	assert.Contains(t, result, "**bold**")
	assert.Contains(t, result, "*italic*")
}

func TestRoundTrip_CodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hi\")\n```"
	result := ToMarkdown(ToStorageFormat(md))
	assert.Contains(t, result, "```go")
	assert.Contains(t, result, `fmt.Println("hi")`)
}

func TestRoundTrip_Link(t *testing.T) {
	md := "Click [here](https://example.com)"
	result := ToMarkdown(ToStorageFormat(md))
	assert.Contains(t, result, "[here](https://example.com)")
}

// --- Fixture-based round-trip tests using generated and anonymised documents ---

// generatedFixtures defines the same configs as testgen.StandardFixtures.
// We regenerate here rather than loading from disk to avoid dependency on
// the testgen test having run first.
var generatedFixtures = []struct {
	Name   string
	Config testgen.DocConfig
}{
	{"small-simple", testgen.DocConfig{Seed: 100, Sections: 2, Complexity: 1}},
	{"medium-mixed", testgen.DocConfig{Seed: 200, Sections: 5, Complexity: 2}},
	{"medium-complex", testgen.DocConfig{Seed: 300, Sections: 5, Complexity: 3}},
	{"large-mixed", testgen.DocConfig{Seed: 400, Sections: 30, Complexity: 2, TargetBytes: 50_000}},
	{"large-complex", testgen.DocConfig{Seed: 500, Sections: 30, Complexity: 3, TargetBytes: 80_000}},
}

func TestRoundTrip_GeneratedFixtures(t *testing.T) {
	for _, fix := range generatedFixtures {
		t.Run(fix.Name, func(t *testing.T) {
			storageFormat := testgen.GenerateStorageFormat(fix.Config)
			fpOriginal := testgen.FingerprintStorageFormat(storageFormat)

			// Storage format -> Markdown -> fingerprint
			markdown := ToMarkdown(storageFormat)
			require.NotEmpty(t, markdown, "ToMarkdown returned empty for %s", fix.Name)
			fpMarkdown := testgen.FingerprintMarkdown(markdown)

			// Verify structural counts survive the conversion
			t.Logf("%s: original headings=%v code=%d links=%d listItems=%d textLen=%d",
				fix.Name, fpOriginal.HeadingCount, fpOriginal.CodeBlockCount,
				fpOriginal.LinkCount, fpOriginal.ListItemCount, fpOriginal.TextLength)
			t.Logf("%s: markdown headings=%v code=%d links=%d listItems=%d textLen=%d",
				fix.Name, fpMarkdown.HeadingCount, fpMarkdown.CodeBlockCount,
				fpMarkdown.LinkCount, fpMarkdown.ListItemCount, fpMarkdown.TextLength)

			// Headings must be preserved exactly
			assert.Equal(t, fpOriginal.HeadingCount, fpMarkdown.HeadingCount,
				"%s: heading counts differ", fix.Name)

			// Code blocks must be preserved
			assert.Equal(t, fpOriginal.CodeBlockCount, fpMarkdown.CodeBlockCount,
				"%s: code block counts differ", fix.Name)

			// Links must be preserved
			assert.Equal(t, fpOriginal.LinkCount, fpMarkdown.LinkCount,
				"%s: link counts differ", fix.Name)

			// List items must be preserved
			assert.Equal(t, fpOriginal.ListItemCount, fpMarkdown.ListItemCount,
				"%s: list item counts differ", fix.Name)

			// Text content must survive — allow some variance from whitespace differences
			// but the bulk of content should be there
			assert.InDelta(t, fpOriginal.TextLength, fpMarkdown.TextLength,
				float64(fpOriginal.TextLength)*0.1, // 10% tolerance for whitespace
				"%s: text length differs by more than 10%%", fix.Name)

			// Bold and italic should be preserved
			assert.Equal(t, fpOriginal.BoldCount, fpMarkdown.BoldCount,
				"%s: bold counts differ", fix.Name)
			assert.Equal(t, fpOriginal.ItalicCount, fpMarkdown.ItalicCount,
				"%s: italic counts differ", fix.Name)

			// Per-section: verify each section from the original exists in the markdown
			for sectionName := range fpOriginal.Sections {
				assert.Contains(t, fpMarkdown.Sections, sectionName,
					"%s: section %q missing after round-trip", fix.Name, sectionName)
			}
		})
	}
}

func TestRoundTrip_GeneratedFixtures_FullCycle(t *testing.T) {
	// Full round-trip: storage -> markdown -> storage -> markdown
	// Verify the second markdown matches the first (converter is stable)
	for _, fix := range generatedFixtures {
		t.Run(fix.Name, func(t *testing.T) {
			storageFormat := testgen.GenerateStorageFormat(fix.Config)

			md1 := ToMarkdown(storageFormat)
			storage2 := ToStorageFormat(md1)
			md2 := ToMarkdown(storage2)

			fp1 := testgen.FingerprintMarkdown(md1)
			fp2 := testgen.FingerprintMarkdown(md2)

			// Second round-trip should be stable
			assert.Equal(t, fp1.HeadingCount, fp2.HeadingCount,
				"%s: heading counts unstable after double round-trip", fix.Name)
			assert.Equal(t, fp1.CodeBlockCount, fp2.CodeBlockCount,
				"%s: code block counts unstable", fix.Name)
			assert.Equal(t, fp1.LinkCount, fp2.LinkCount,
				"%s: link counts unstable", fix.Name)
			assert.Equal(t, fp1.ListItemCount, fp2.ListItemCount,
				"%s: list item counts unstable", fix.Name)
			assert.Equal(t, fp1.BoldCount, fp2.BoldCount,
				"%s: bold counts unstable", fix.Name)
			assert.Equal(t, fp1.ItalicCount, fp2.ItalicCount,
				"%s: italic counts unstable", fix.Name)
		})
	}
}

func TestToStorageFormat_IndentedCodeBlock(t *testing.T) {
	result := ToStorageFormat("    some indented code\n    more code")
	assert.Contains(t, result, `ac:name="code"`)
	assert.Contains(t, result, "some indented code")
}

func TestToStorageFormat_AutoLink(t *testing.T) {
	result := ToStorageFormat("<https://example.com>")
	assert.Contains(t, result, "https://example.com")
	assert.Contains(t, result, "<a ")
}

func TestToStorageFormat_RawHTML(t *testing.T) {
	result := ToStorageFormat("text with <br> inline html")
	assert.Contains(t, result, "<br>")
}

func TestToMarkdown_Table(t *testing.T) {
	input := "<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>"
	result := ToMarkdown(input)
	assert.Contains(t, result, "A")
	assert.Contains(t, result, "B")
	assert.Contains(t, result, "1")
	assert.Contains(t, result, "2")
}

func TestRoundTrip_AnonymisedFixtures(t *testing.T) {
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
			require.NoError(t, err, "loading fixture %s", path)
			require.NotEmpty(t, fixture.Content, "fixture %s has empty content", fixture.Name)

			fpOriginal := testgen.FingerprintStorageFormat(fixture.Content)
			t.Logf("%s: headings=%v code=%d links=%d listItems=%d textLen=%d",
				fixture.Name, fpOriginal.HeadingCount, fpOriginal.CodeBlockCount,
				fpOriginal.LinkCount, fpOriginal.ListItemCount, fpOriginal.TextLength)
			if len(fpOriginal.MacroCount) > 0 {
				t.Logf("%s: macros=%v", fixture.Name, fpOriginal.MacroCount)
			}

			// Convert to Markdown
			markdown := ToMarkdown(fixture.Content)
			require.NotEmpty(t, markdown, "ToMarkdown returned empty for %s", fixture.Name)

			fpMarkdown := testgen.FingerprintMarkdown(markdown)
			t.Logf("%s (md): headings=%v code=%d links=%d listItems=%d textLen=%d",
				fixture.Name, fpMarkdown.HeadingCount, fpMarkdown.CodeBlockCount,
				fpMarkdown.LinkCount, fpMarkdown.ListItemCount, fpMarkdown.TextLength)

			// Headings should survive — real-world docs may have some variance
			// due to Chrome-saved HTML having extra structure, but total heading
			// count should be close
			totalOriginal := 0
			totalMarkdown := 0
			for i := 0; i < 6; i++ {
				totalOriginal += fpOriginal.HeadingCount[i]
				totalMarkdown += fpMarkdown.HeadingCount[i]
			}
			if totalOriginal > 0 {
				assert.InDelta(t, totalOriginal, totalMarkdown,
					float64(totalOriginal)*0.2, // 20% tolerance for real-world docs
					"%s: total heading count differs significantly", fixture.Name)
			}

			// Text content must survive
			if fpOriginal.TextLength > 0 {
				assert.Greater(t, fpMarkdown.TextLength, 0,
					"%s: all text content lost", fixture.Name)
				assert.InDelta(t, fpOriginal.TextLength, fpMarkdown.TextLength,
					float64(fpOriginal.TextLength)*0.3, // 30% tolerance for real-world
					"%s: text length differs significantly", fixture.Name)
			}

			// List items should mostly survive
			if fpOriginal.ListItemCount > 0 {
				assert.Greater(t, fpMarkdown.ListItemCount, 0,
					"%s: all list items lost", fixture.Name)
			}

			// Full cycle stability: markdown -> storage -> markdown
			storage2 := ToStorageFormat(markdown)
			md2 := ToMarkdown(storage2)
			fp2 := testgen.FingerprintMarkdown(md2)

			// Second round-trip should be stable
			assert.Equal(t, fpMarkdown.HeadingCount, fp2.HeadingCount,
				"%s: headings unstable after double round-trip", fixture.Name)
			assert.InDelta(t, fpMarkdown.TextLength, fp2.TextLength,
				float64(fpMarkdown.TextLength)*0.1,
				"%s: text length unstable after double round-trip", fixture.Name)
		})
	}
}
