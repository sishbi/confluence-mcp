package mdconv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// onlyTable returns the single rendered Markdown table from tables, failing
// the test if there isn't exactly one.
func onlyTable(t *testing.T, tables map[string]string) string {
	t.Helper()
	if len(tables) != 1 {
		t.Fatalf("expected exactly 1 table, got %d", len(tables))
	}
	for _, md := range tables {
		return md
	}
	return ""
}

func TestTableSimple(t *testing.T) {
	input := `<table><tbody>` +
		`<tr><th>H1</th><th>H2</th></tr>` +
		`<tr><td>a</td><td>b</td></tr>` +
		`<tr><td>c</td><td>d</td></tr>` +
		`</tbody></table>`

	_, tables, err := rewriteTables(input, nil)
	assert.NoError(t, err)
	md := onlyTable(t, tables)

	assert.Contains(t, md, "| H1 | H2 |")
	assert.Contains(t, md, "| --- | --- |")
	assert.Contains(t, md, "| a | b |")
	assert.Contains(t, md, "| c | d |")
}

func TestTableColspan(t *testing.T) {
	input := `<table><tbody>` +
		`<tr><th>H1</th><th>H2</th><th>H3</th></tr>` +
		`<tr><td colspan="2">merged</td><td>right</td></tr>` +
		`</tbody></table>`

	_, tables, err := rewriteTables(input, nil)
	assert.NoError(t, err)
	md := onlyTable(t, tables)

	assert.Contains(t, md, "| merged (spans 2 cols) | merged | right |")
}

func TestTableRowspan(t *testing.T) {
	input := `<table><tbody>` +
		`<tr><th>H1</th><th>H2</th></tr>` +
		`<tr><td rowspan="2">spans down</td><td>row1</td></tr>` +
		`<tr><td>row2</td></tr>` +
		`</tbody></table>`

	_, tables, err := rewriteTables(input, nil)
	assert.NoError(t, err)
	md := onlyTable(t, tables)

	assert.Contains(t, md, "| spans down | row1 |")
	assert.Contains(t, md, "| ⬆ | row2 |")
}

func TestTableNestedTime(t *testing.T) {
	input := `<table><tbody>` +
		`<tr><th>Date</th></tr>` +
		`<tr><td><time datetime="2024-09-01"/></td></tr>` +
		`</tbody></table>`

	_, tables, err := rewriteTables(input, nil)
	assert.NoError(t, err)
	md := onlyTable(t, tables)

	assert.Contains(t, md, "| 2024-09-01 |")
}

func TestTableInlineFormatting(t *testing.T) {
	input := `<table><tbody>` +
		`<tr><th>Bold</th><th>Code</th><th>Link</th></tr>` +
		`<tr>` +
		`<td><strong>important</strong></td>` +
		`<td><code>fn()</code></td>` +
		`<td><a href="https://example.com">site</a></td>` +
		`</tr></tbody></table>`

	_, tables, err := rewriteTables(input, nil)
	assert.NoError(t, err)
	md := onlyTable(t, tables)

	assert.Contains(t, md, "**important**")
	assert.Contains(t, md, "`fn()`")
	assert.Contains(t, md, "[site](https://example.com)")
}

func TestTableEndToEndMarkdown(t *testing.T) {
	// Full pipeline test — ToMarkdown should produce a Markdown table that
	// survives html-to-markdown and postprocessMarkdown unchanged.
	input := `<p>Before</p>` +
		`<table><tbody>` +
		`<tr><th>A</th><th>B</th></tr>` +
		`<tr><td>1</td><td>2</td></tr>` +
		`</tbody></table>` +
		`<p>After</p>`

	got := ToMarkdown(input)

	assert.Contains(t, got, "| A | B |")
	assert.Contains(t, got, "| --- | --- |")
	assert.Contains(t, got, "| 1 | 2 |")
	assert.Contains(t, got, "Before")
	assert.Contains(t, got, "After")
	// Ensure the table is not wrapped in backtick code fences.
	assert.False(t, strings.Contains(got, "```\n| A |"), "table should not be wrapped in code fence")
}
