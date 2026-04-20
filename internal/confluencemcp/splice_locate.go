package confluencemcp

import (
	"strings"
)

// headingMatch describes a located heading.
type headingMatch struct {
	// level is the heading level (1-6).
	level int
	// headingStartOff is the byte offset of the opening tag in the body.
	headingStartOff int
	// headingEndOff is the byte offset just past the closing tag in the body.
	headingEndOff int
	// layoutCellDepth and macroDepth recorded at the opening tag.
	layoutCellDepth int
	macroDepth      int
}

// locateHeading walks body looking for a heading whose extracted text equals
// heading (decoded, whitespace-collapsed, trimmed). Returns the match or a
// sentinel error (ErrHeadingNotFound, ErrHeadingInUnsafeContainer,
// ErrAmbiguousHeading). Matches inside unsafe containers (macro body, td, th,
// blockquote, li) are excluded from candidacy; the unsafe error only fires if
// no safe candidate exists and at least one unsafe candidate does.
func locateHeading(body, heading string) (headingMatch, error) {
	want := normalizeHeading(heading)

	type candidate struct {
		match headingMatch
		safe  bool
	}

	var (
		stack     []*candidate
		collected []candidate
	)

	err := walkStorage(body, func(ev walkEvent) error {
		switch ev.kind {
		case eventHeadingStart:
			c := &candidate{
				match: headingMatch{
					level:           ev.level,
					headingStartOff: ev.tokStart,
					layoutCellDepth: ev.layoutCellDepth,
					macroDepth:      ev.macroDepth,
				},
				safe: ev.macroDepth == 0 && ev.unsafeContainerDepth == 0,
			}
			stack = append(stack, c)
		case eventHeadingEnd:
			if len(stack) == 0 {
				return nil
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			top.match.headingEndOff = ev.tokEnd
			collected = append(collected, *top)
		case eventStart, eventEnd:
			// Text extraction is handled by the XML decoder in walkStorage
			// only for elements; we need a separate text capture for heading
			// content. Handled by re-parsing below.
		}
		return nil
	})
	if err != nil {
		return headingMatch{}, err
	}

	// Second pass: extract the text inside each heading range from the raw body.
	// Strip tags and decode entities, then normalize.
	var (
		safeMatches   []headingMatch
		unsafeMatches []headingMatch
	)
	for _, c := range collected {
		text := extractText(body[c.match.headingStartOff:c.match.headingEndOff])
		if normalizeHeading(text) != want {
			continue
		}
		if c.safe {
			safeMatches = append(safeMatches, c.match)
		} else {
			unsafeMatches = append(unsafeMatches, c.match)
		}
	}

	switch {
	case len(safeMatches) == 1:
		return safeMatches[0], nil
	case len(safeMatches) > 1:
		return headingMatch{}, ErrAmbiguousHeading
	case len(unsafeMatches) > 0:
		return headingMatch{}, ErrHeadingInUnsafeContainer
	default:
		return headingMatch{}, ErrHeadingNotFound
	}
}

// normalizeHeading canonicalises a heading for comparison: decode common XML
// entities, collapse internal whitespace to single spaces, trim.
func normalizeHeading(s string) string {
	s = decodeEntities(s)
	// Collapse whitespace.
	var b strings.Builder
	prevSpace := true
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// extractText returns the concatenated text content of a string of XHTML by
// dropping tags and decoding common entities. It does not attempt to handle
// CDATA or comments — headings don't contain those in practice.
func extractText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
		case !inTag:
			b.WriteByte(c)
		}
	}
	return decodeEntities(b.String())
}

// decodeEntities decodes the handful of XML/HTML entities that appear in
// Confluence storage headings: &amp;, &lt;, &gt;, &quot;, &apos;, &ndash;,
// &mdash;, &nbsp;, &lsquo;, &rsquo;, &ldquo;, &rdquo;. Numeric entities are
// not handled (unseen in real pages).
func decodeEntities(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	replacements := []string{
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&apos;", "'",
		"&nbsp;", " ",
		"&ndash;", "–",
		"&mdash;", "—",
		"&lsquo;", "‘",
		"&rsquo;", "’",
		"&ldquo;", "“",
		"&rdquo;", "”",
	}
	return strings.NewReplacer(replacements...).Replace(s)
}
