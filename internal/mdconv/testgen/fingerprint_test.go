package testgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFingerprintStorageFormat_Empty(t *testing.T) {
	fp := FingerprintStorageFormat("")
	assert.Equal(t, 0, fp.TextLength)
	assert.Equal(t, [6]int{}, fp.HeadingCount)
}

func TestFingerprintStorageFormat_BasicElements(t *testing.T) {
	html := `<h1>Title</h1><p>Hello <strong>bold</strong> and <em>italic</em> world</p><h2>Sub</h2><p>More text</p>`
	fp := FingerprintStorageFormat(html)

	assert.Equal(t, [6]int{1, 1, 0, 0, 0, 0}, fp.HeadingCount)
	assert.Equal(t, 1, fp.BoldCount)
	assert.Equal(t, 1, fp.ItalicCount)
	assert.Greater(t, fp.TextLength, 0)
}

func TestFingerprintStorageFormat_CodeAndLinks(t *testing.T) {
	html := `<p>See <a href="https://example.com">link</a></p>` +
		`<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[fmt.Println()]]></ac:plain-text-body></ac:structured-macro>`
	fp := FingerprintStorageFormat(html)

	assert.Equal(t, 1, fp.LinkCount)
	assert.Equal(t, 1, fp.CodeBlockCount)
}

func TestFingerprintStorageFormat_Lists(t *testing.T) {
	html := `<ul><li>one</li><li>two</li><li>three</li></ul><ol><li>a</li><li>b</li></ol>`
	fp := FingerprintStorageFormat(html)

	assert.Equal(t, 5, fp.ListItemCount)
}

func TestFingerprintStorageFormat_TextHashDeterministic(t *testing.T) {
	html := `<h1>Title</h1><p>Hello world</p>`
	fp1 := FingerprintStorageFormat(html)
	fp2 := FingerprintStorageFormat(html)
	assert.Equal(t, fp1.TextHash, fp2.TextHash)
	assert.NotEqual(t, [32]byte{}, fp1.TextHash)
}

func TestFingerprintStorageFormat_PerSection(t *testing.T) {
	html := `<h1>Intro</h1><p>First paragraph</p><h2>Details</h2><p>Second paragraph with <strong>bold</strong></p><h2>Summary</h2><p>Third paragraph</p>`
	fp := FingerprintStorageFormat(html)

	assert.Len(t, fp.Sections, 3)
	assert.Contains(t, fp.Sections, "Intro")
	assert.Contains(t, fp.Sections, "Details")
	assert.Contains(t, fp.Sections, "Summary")
	assert.Equal(t, 1, fp.Sections["Details"].BoldCount)
}

func TestFingerprintMarkdown_Empty(t *testing.T) {
	fp := FingerprintMarkdown("")
	assert.Equal(t, 0, fp.TextLength)
}

func TestFingerprintMarkdown_BasicElements(t *testing.T) {
	md := "# Title\n\nHello **bold** and *italic* world\n\n## Sub\n\nMore text"
	fp := FingerprintMarkdown(md)

	assert.Equal(t, [6]int{1, 1, 0, 0, 0, 0}, fp.HeadingCount)
	assert.Equal(t, 1, fp.BoldCount)
	assert.Equal(t, 1, fp.ItalicCount)
	assert.Greater(t, fp.TextLength, 0)
}

func TestFingerprintMarkdown_CodeAndLinks(t *testing.T) {
	md := "See [link](https://example.com)\n\n```go\nfmt.Println()\n```"
	fp := FingerprintMarkdown(md)

	assert.Equal(t, 1, fp.LinkCount)
	assert.Equal(t, 1, fp.CodeBlockCount)
}

func TestFingerprintMarkdown_Lists(t *testing.T) {
	md := "- one\n- two\n- three\n\n1. a\n2. b"
	fp := FingerprintMarkdown(md)

	assert.Equal(t, 5, fp.ListItemCount)
}

func TestFingerprintMarkdown_PerSection(t *testing.T) {
	md := "# Intro\n\nFirst paragraph\n\n## Details\n\nSecond paragraph with **bold**\n\n## Summary\n\nThird paragraph"
	fp := FingerprintMarkdown(md)

	assert.Len(t, fp.Sections, 3)
	assert.Contains(t, fp.Sections, "Intro")
	assert.Contains(t, fp.Sections, "Details")
	assert.Contains(t, fp.Sections, "Summary")
	assert.Equal(t, 1, fp.Sections["Details"].BoldCount)
}

func TestFingerprintMarkdown_TextHashDeterministic(t *testing.T) {
	md := "# Title\n\nHello world"
	fp1 := FingerprintMarkdown(md)
	fp2 := FingerprintMarkdown(md)
	assert.Equal(t, fp1.TextHash, fp2.TextHash)
	assert.NotEqual(t, [32]byte{}, fp1.TextHash)
}
