package mdconv

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// rewriteTables replaces every <table>…</table> subtree in xhtml with a unique
// placeholder sentinel and returns the modified text alongside a map from
// sentinel to its rendered Markdown table. Callers re-insert tables by
// substituting sentinels after html-to-markdown conversion (see restoreTables).
//
// Sentinels are plain ASCII tokens that Markdown converters preserve verbatim.
//
// If log is non-nil, each table and each colspan/rowspan occurrence is counted.
func rewriteTables(xhtml string, log *ConversionLog) (string, map[string]string, error) {
	tables := map[string]string{}
	var out strings.Builder

	remaining := xhtml
	for {
		start := indexIgnoreCase(remaining, "<table")
		if start < 0 {
			out.WriteString(remaining)
			break
		}
		out.WriteString(remaining[:start])

		end, ok := findMatchingElementClose(remaining, start, "table")
		if !ok {
			// Unbalanced — emit the rest untouched to avoid data loss.
			out.WriteString(remaining[start:])
			break
		}

		fragment := remaining[start:end]
		md, err := tableFragmentToMarkdown(fragment, log)
		if err != nil {
			return "", nil, err
		}

		sentinel := fmt.Sprintf("MDTABLE%dEDNILPKM", len(tables))
		tables[sentinel] = md
		// Surround with blank lines so Markdown block context is clean.
		out.WriteString("\n")
		out.WriteString(sentinel)
		out.WriteString("\n")

		remaining = remaining[end:]
	}

	return out.String(), tables, nil
}

// restoreTables replaces each sentinel in md with its rendered Markdown table.
func restoreTables(md string, tables map[string]string) string {
	if len(tables) == 0 {
		return md
	}
	for sentinel, table := range tables {
		md = strings.ReplaceAll(md, sentinel, table)
	}
	return md
}

// indexIgnoreCase is like strings.Index but case-insensitive for the ASCII
// needle. Confluence always emits lowercase tags but we tolerate both.
func indexIgnoreCase(haystack, needle string) int {
	lowerHay := strings.ToLower(haystack)
	lowerNeedle := strings.ToLower(needle)
	return strings.Index(lowerHay, lowerNeedle)
}

// findMatchingElementClose locates the end (exclusive index just past </tag>)
// of the element whose open tag begins at openIdx in s. Nested elements with
// the same tag name are balanced by depth counting.
func findMatchingElementClose(s string, openIdx int, tag string) (int, bool) {
	openTag := "<" + strings.ToLower(tag)
	closeTag := "</" + strings.ToLower(tag)
	depth := 0
	i := openIdx
	lower := strings.ToLower(s)
	for i < len(lower) {
		switch {
		case strings.HasPrefix(lower[i:], openTag):
			// Skip until the end of this opening tag to avoid counting
			// self-closing variants twice.
			gt := strings.Index(s[i:], ">")
			if gt < 0 {
				return 0, false
			}
			selfClosing := gt > 0 && s[i+gt-1] == '/'
			if !selfClosing {
				depth++
			}
			i += gt + 1
		case strings.HasPrefix(lower[i:], closeTag):
			gt := strings.Index(s[i:], ">")
			if gt < 0 {
				return 0, false
			}
			depth--
			i += gt + 1
			if depth == 0 {
				return i, true
			}
		default:
			i++
		}
	}
	return 0, false
}

// tableFragmentToMarkdown parses a <table>…</table> fragment and renders a
// GitHub-flavoured Markdown table. Colspan repeats cell content across the
// spanned columns and annotates the first occurrence; rowspan fills the
// occluded rows below with the up-arrow convention.
func tableFragmentToMarkdown(fragment string, log *ConversionLog) (string, error) {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return "", err
	}
	tableNode := findFirst(doc, "table")
	if tableNode == nil {
		return fragment, nil
	}

	rows := collectRows(tableNode)
	if len(rows) == 0 {
		return "", nil
	}

	// Expand colspan/rowspan into a rectangular grid.
	grid := expandGrid(rows, log)
	if len(grid) == 0 {
		return "", nil
	}

	// Determine header row: first row if it contains any <th>, else none.
	headerIdx := -1
	for colIdx := range grid[0] {
		if grid[0][colIdx].isHeader {
			headerIdx = 0
			break
		}
	}

	numCols := 0
	for _, row := range grid {
		if len(row) > numCols {
			numCols = len(row)
		}
	}

	var b strings.Builder

	writeRow := func(row []renderedCell) {
		b.WriteString("| ")
		for i := 0; i < numCols; i++ {
			if i < len(row) {
				b.WriteString(row[i].text)
			}
			if i < numCols-1 {
				b.WriteString(" | ")
			}
		}
		b.WriteString(" |\n")
	}

	if headerIdx == 0 {
		writeRow(grid[0])
	} else {
		// Synthesise an empty header so GFM parsers render a table.
		empty := make([]renderedCell, numCols)
		writeRow(empty)
	}
	b.WriteString("|")
	for i := 0; i < numCols; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")

	bodyStart := 0
	if headerIdx == 0 {
		bodyStart = 1
	}
	for _, row := range grid[bodyStart:] {
		writeRow(row)
	}

	if log != nil {
		log.Element("table")
	}

	return b.String(), nil
}

// renderedCell is one cell in the fully-expanded grid.
type renderedCell struct {
	text     string
	isHeader bool
}

// rawRow is one <tr> with its original cells and their span attributes.
type rawRow struct {
	cells []rawCell
}

type rawCell struct {
	text     string
	colspan  int
	rowspan  int
	isHeader bool
}

// collectRows walks the table subtree and returns the rows with their raw
// cells in document order.
func collectRows(table *html.Node) []rawRow {
	var rows []rawRow
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			row := rawRow{}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode {
					continue
				}
				if c.Data != "td" && c.Data != "th" {
					continue
				}
				row.cells = append(row.cells, rawCell{
					text:     cellText(c),
					colspan:  attrInt(c, "colspan", 1),
					rowspan:  attrInt(c, "rowspan", 1),
					isHeader: c.Data == "th",
				})
			}
			rows = append(rows, row)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	return rows
}

// expandGrid turns the raw rows into a rectangular grid honouring colspan
// (repeat + annotate) and rowspan (fill with up-arrow in later rows).
func expandGrid(rows []rawRow, log *ConversionLog) [][]renderedCell {
	if len(rows) == 0 {
		return nil
	}
	// rowspanHold[col] = remaining rows this column is occluded by a rowspan.
	rowspanHold := map[int]int{}

	grid := make([][]renderedCell, len(rows))
	for rIdx, row := range rows {
		var out []renderedCell
		col := 0
		srcIdx := 0
		for srcIdx < len(row.cells) || rowspanHold[col] > 0 {
			if rowspanHold[col] > 0 {
				out = append(out, renderedCell{text: "⬆"})
				rowspanHold[col]--
				col++
				continue
			}
			c := row.cells[srcIdx]
			srcIdx++

			cs := c.colspan
			if cs < 1 {
				cs = 1
			}
			rs := c.rowspan
			if rs < 1 {
				rs = 1
			}
			if cs > 1 && log != nil {
				log.Element("colspan")
			}
			if rs > 1 && log != nil {
				log.Element("rowspan")
			}

			firstText := c.text
			if cs > 1 {
				firstText = c.text + " (spans " + strconv.Itoa(cs) + " cols)"
			}
			out = append(out, renderedCell{text: firstText, isHeader: c.isHeader})
			for extra := 1; extra < cs; extra++ {
				out = append(out, renderedCell{text: c.text, isHeader: c.isHeader})
			}

			if rs > 1 {
				// Reserve every spanned column for the next (rs-1) rows.
				for offset := 0; offset < cs; offset++ {
					rowspanHold[col+offset] = rs - 1
				}
			}
			col += cs
		}
		grid[rIdx] = out
	}
	return grid
}

// cellText renders the inner content of a cell as Markdown-ready inline text,
// preserving <strong>, <em>, <code>, <a>, and <time datetime>.
func cellText(n *html.Node) string {
	var b strings.Builder
	renderInline(n, &b)
	return strings.TrimSpace(b.String())
}

func renderInline(n *html.Node, b *strings.Builder) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.TextNode:
			b.WriteString(c.Data)
		case c.Type == html.ElementNode && c.Data == "strong":
			b.WriteString("**")
			renderInline(c, b)
			b.WriteString("**")
		case c.Type == html.ElementNode && c.Data == "em":
			b.WriteString("*")
			renderInline(c, b)
			b.WriteString("*")
		case c.Type == html.ElementNode && c.Data == "code":
			b.WriteString("`")
			renderInline(c, b)
			b.WriteString("`")
		case c.Type == html.ElementNode && c.Data == "a":
			href := attrString(c, "href")
			b.WriteString("[")
			renderInline(c, b)
			b.WriteString("](")
			b.WriteString(href)
			b.WriteString(")")
		case c.Type == html.ElementNode && c.Data == "time":
			dt := attrString(c, "datetime")
			if dt != "" {
				b.WriteString(dt)
			} else {
				renderInline(c, b)
			}
		case c.Type == html.ElementNode && c.Data == "p":
			renderInline(c, b)
		case c.Type == html.ElementNode && (c.Data == "br"):
			b.WriteString(" ")
		case c.Type == html.ElementNode && (c.Data == "ul" || c.Data == "ol"):
			// GFM tables cannot host block-level lists. Flatten to HTML
			// <br> separators with bullet glyphs so the items remain
			// readable inside the cell.
			renderInlineList(c, b, c.Data == "ol")
		default:
			// Unknown element — render children in-place. This keeps inline
			// text flowing even when the source wraps cells in unexpected
			// containers.
			renderInline(c, b)
		}
	}
}

// renderInlineList flattens a <ul>/<ol> into `<br>•` or `<br>N.` prefixed
// items so the list survives inside a GFM table cell (which cannot host a real
// block-level list). A leading <br> separates the list from any preceding
// cell text.
func renderInlineList(ul *html.Node, b *strings.Builder, ordered bool) {
	n := 0
	for c := ul.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "li" {
			continue
		}
		n++
		b.WriteString("<br>")
		if ordered {
			fmt.Fprintf(b, "%d. ", n)
		} else {
			b.WriteString("• ")
		}
		renderInline(c, b)
	}
}

// findFirst returns the first element node in the subtree with the given tag.
func findFirst(root *html.Node, tag string) *html.Node {
	if root.Type == html.ElementNode && root.Data == tag {
		return root
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func attrInt(n *html.Node, key string, defaultValue int) int {
	for _, a := range n.Attr {
		if a.Key == key {
			if v, err := strconv.Atoi(a.Val); err == nil {
				return v
			}
		}
	}
	return defaultValue
}

func attrString(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
