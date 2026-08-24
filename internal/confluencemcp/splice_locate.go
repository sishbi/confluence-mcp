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
	// headingOpenEndOff is the byte offset just past the opening tag, and
	// headingCloseStartOff the offset of the closing tag. Together they bound
	// the heading's inner XHTML, which is the range a rename rewrites.
	headingOpenEndOff    int
	headingCloseStartOff int
	// layoutCellDepth, macroDepth, and unsafeContainerDepth recorded at the
	// opening tag.
	layoutCellDepth      int
	macroDepth           int
	unsafeContainerDepth int
}

// locateHeading walks body looking for a heading whose extracted text equals
// heading (decoded, whitespace-collapsed, trimmed). Returns the match or a
// sentinel error (ErrHeadingNotFound, ErrHeadingInUnsafeContainer,
// ErrAmbiguousHeading). Matches inside a macro body or any of the
// unsafeContainerTags (see splice_walker.go) are excluded from candidacy; the
// unsafe error only fires if no safe candidate exists and at least one
// unsafe candidate does.
func locateHeading(body, heading string) (headingMatch, error) {
	want := normalizeHeading(heading)

	collected, err := collectHeadings(body)
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
		if normalizeHeading(c.text(body)) != want {
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

// headingCandidate is one heading found by collectHeadings, with the safety
// flag that decides whether locateHeading will consider it.
type headingCandidate struct {
	match headingMatch
	safe  bool
}

// text returns the candidate's raw text content, tags stripped and entities
// decoded. Not normalised — callers that compare do that themselves.
func (c headingCandidate) text(body string) string {
	return extractText(body[c.match.headingStartOff:c.match.headingEndOff])
}

// collectHeadings walks body once and returns every heading it contains, in
// document order, each flagged safe or not. Shared by locateHeading (which
// matches one) and headingTextsIn (which needs them all).
func collectHeadings(body string) ([]headingCandidate, error) {
	var (
		stack     []*headingCandidate
		collected []headingCandidate
	)

	err := walkStorage(body, func(ev walkEvent) error {
		switch ev.kind {
		case eventHeadingStart:
			c := &headingCandidate{
				match: headingMatch{
					level:                ev.level,
					headingStartOff:      ev.tokStart,
					headingOpenEndOff:    ev.tokEnd,
					layoutCellDepth:      ev.layoutCellDepth,
					macroDepth:           ev.macroDepth,
					unsafeContainerDepth: ev.unsafeContainerDepth,
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
			top.match.headingCloseStartOff = ev.tokStart
			top.match.headingEndOff = ev.tokEnd
			collected = append(collected, *top)
		case eventStart, eventEnd:
			// Text extraction is handled by the XML decoder in walkStorage
			// only for elements; the heading's own text is recovered from the
			// raw body by headingCandidate.text.
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return collected, nil
}

// headingTextsIn returns the normalised text of every heading in body that
// locateHeading would treat as a candidate. Headings inside a macro or an
// unsafe container are excluded: they can never be located, so they cannot
// make a rename ambiguous.
func headingTextsIn(body string) ([]string, error) {
	collected, err := collectHeadings(body)
	if err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(collected))
	for _, c := range collected {
		if c.safe {
			texts = append(texts, normalizeHeading(c.text(body)))
		}
	}
	return texts, nil
}

// headingConfluenceChildren returns the qualified names of the Confluence-only
// element children inside the located heading, in document order,
// deduplicated. These are the constructs a plain-text rename would destroy
// without the caller ever having seen them as markup: mentions
// (ac:link + ri:user), macros including status lozenges (ac:structured-macro),
// and emoticons (ac:emoticon).
//
// Plain XHTML inline formatting (em, strong, code, span, …) is deliberately NOT
// reported. It carries presentation only, the caller saw it in the Markdown
// they read, and replacing it along with the words is exactly what a rename
// means.
func headingConfluenceChildren(body string, match headingMatch) []string {
	inner := body[match.headingOpenEndOff:match.headingCloseStartOff]
	var (
		names []string
		seen  = map[string]bool{}
	)
	// A malformed inner fragment yields no names rather than an error: the
	// caller uses this to refuse a rename, and refusing on unparseable markup
	// would be a worse answer than letting the rename through.
	_ = walkStorage(inner, func(ev walkEvent) error {
		if ev.kind != eventStart && ev.kind != eventHeadingStart {
			return nil
		}
		// Confluence declares no namespaces, so any prefix at all marks one of
		// its own constructs rather than plain XHTML.
		if ev.space == "" {
			return nil
		}
		qualified := ev.space + ":" + ev.name
		if !seen[qualified] {
			seen[qualified] = true
			names = append(names, qualified)
		}
		return nil
	})
	return names
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
