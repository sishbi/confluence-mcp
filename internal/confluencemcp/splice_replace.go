package confluencemcp

import (
	"fmt"
	"regexp"
	"strings"
)

// spliceReplaceSection replaces the content under the target heading up to
// the stop point defined by the section rules. See
// internal/confluencemcp/.../design doc for the full rule.
func spliceReplaceSection(body, fragment, heading string) (SpliceResult, error) {
	match, err := locateHeading(body, heading)
	if err != nil {
		return SpliceResult{}, err
	}

	// Callers frequently emit a fragment that begins with the section heading
	// (e.g. "## Data scrubbing\n\n..."), which would produce a duplicated
	// heading after the splice since the target heading is preserved. Strip a
	// leading heading whose text matches the target so the fragment body-only
	// convention is forgiving.
	fragment = stripLeadingHeading(fragment, heading)

	// The replaced region starts at match.headingEndOff (just after </hN>) and
	// extends up to the stop offset. findSectionEnd also collects top-level
	// element names seen between the heading end and the stop, for the
	// replaced-element summary.

	targetLayoutDepth := match.layoutCellDepth
	targetLevel := match.level

	stopOff, replacedTags, err := findSectionEnd(body, match)
	if err != nil {
		return SpliceResult{}, err
	}

	replacedByteCount := stopOff - match.headingEndOff
	merged := body[:match.headingEndOff] + fragment + body[stopOff:]

	container := "document root"
	if targetLayoutDepth > 0 {
		container = "ac:layout-cell"
	}
	startAnchor := fmt.Sprintf("after </h%d> %q", targetLevel, heading)
	endAnchor := "end of " + container
	// If we stopped at a heading rather than a container close, report that.
	// We can detect this by re-walking the original body to find the element at
	// stopOff — but a simpler heuristic is: if stopOff < end of body, it's a
	// heading stop.
	if stopOff < len(body) && (targetLayoutDepth == 0 || !strings.HasPrefix(body[stopOff:], "</ac:layout-cell>")) {
		endAnchor = "before next heading at same or higher level"
	}

	return SpliceResult{
		Merged: merged,
		Boundary: BoundaryInfo{
			StartAnchor:            startAnchor,
			EndAnchor:              endAnchor,
			Container:              container,
			CrossesLayout:          false,
			ReplacedByteCount:      replacedByteCount,
			ReplacedElementSummary: summariseTags(replacedTags),
		},
	}, nil
}

// reLeadingHeading matches an optional leading whitespace run followed by a
// single <hN>...</hN> element and any trailing whitespace. Go's RE2 has no
// backreferences; the closing level is checked post-match rather than in the
// pattern. The inner content is captured for comparison against the target
// heading text.
var reLeadingHeading = regexp.MustCompile(`(?s)\A\s*<h([1-6])[^>]*>(.*?)</h([1-6])>\s*`)

// stripLeadingHeading removes a leading <hN>...</hN> element from fragment
// when its text content (normalized) matches target. Returns fragment
// unchanged if the fragment does not start with a heading or the text
// doesn't match. Level is ignored so a fragment that prefixes a different
// level than the target is still cleaned.
func stripLeadingHeading(fragment, target string) string {
	loc := reLeadingHeading.FindStringSubmatchIndex(fragment)
	if loc == nil {
		return fragment
	}
	openLevel := fragment[loc[2]:loc[3]]
	closeLevel := fragment[loc[6]:loc[7]]
	if openLevel != closeLevel {
		return fragment
	}
	inner := fragment[loc[4]:loc[5]]
	if normalizeHeading(extractText(inner)) != normalizeHeading(target) {
		return fragment
	}
	return fragment[loc[1]:]
}

// summariseTags turns a document-order list of element local names into a
// histogram like ["<p> x 2", "<ul> x 1"]. The order is document-order first
// appearance.
func summariseTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	counts := make(map[string]int, len(tags))
	order := make([]string, 0, len(tags))
	for _, t := range tags {
		if _, ok := counts[t]; !ok {
			order = append(order, t)
		}
		counts[t]++
	}
	out := make([]string, 0, len(order))
	for _, t := range order {
		out = append(out, fmt.Sprintf("<%s> x %d", t, counts[t]))
	}
	return out
}
