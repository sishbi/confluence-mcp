package confluencemcp

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// renamedPrefix returns body up to the end of the matched heading element,
// with the heading's inner text replaced by newHeading (escaped). An empty
// newHeading returns the original bytes. The preview and the splice both build
// their prefix here so a dry run cannot render a rename differently from the
// write it is previewing.
func renamedPrefix(body string, match headingMatch, newHeading string) string {
	if newHeading == "" {
		return body[:match.headingEndOff]
	}
	return body[:match.headingOpenEndOff] +
		html.EscapeString(newHeading) +
		body[match.headingCloseStartOff:match.headingEndOff]
}

// validateRename rejects a rename that would be a no-op, that would make the
// page's headings ambiguous for every later locate, or that would destroy
// element children inside the heading. A no-op newHeading (empty) passes.
//
// Every check runs before the merged body is assembled, so a rejected rename
// leaves the page untouched.
func validateRename(body string, match headingMatch, heading, newHeading string) error {
	if newHeading == "" {
		return nil
	}
	want := normalizeHeading(newHeading)
	if want == "" {
		return fmt.Errorf("%w: new heading text is empty", ErrRenameNoOp)
	}
	if want == normalizeHeading(heading) {
		return fmt.Errorf("%w: new heading %q is the same as the current heading", ErrRenameNoOp, heading)
	}

	// A heading holding a mention, macro, or emoticon cannot be renamed from
	// plain text without destroying it, and the caller never saw it as markup.
	// Inline formatting is not in this set: it is presentation the caller did
	// see, and a rename replaces it along with the words.
	if children := headingConfluenceChildren(body, match); len(children) > 0 {
		return fmt.Errorf(
			"%w: heading %q contains %s — renaming it would destroy them; edit the section with action \"update\" instead",
			ErrHeadingHasChildren, heading, strings.Join(children, ", "),
		)
	}

	texts, err := headingTextsIn(body)
	if err != nil {
		return err
	}
	for _, t := range texts {
		if t == want {
			return fmt.Errorf(
				"%w: heading %q already exists on this page — renaming %q to it would make both unaddressable",
				ErrRenameAmbiguous, newHeading, heading,
			)
		}
	}
	return nil
}

// spliceReplaceSection replaces the content under the target heading up to
// the stop point defined by the section rules. See
// internal/confluencemcp/.../design doc for the full rule.
//
// opts.IncludeSubsections widens the replaced range to the whole section,
// subsections included. It defaults to false because the narrow range is the
// least destructive: a caller sending a small fragment for one section must
// not lose that section's subsections just because the fragment did not
// repeat them.
//
// opts.NewHeading, when set, also rewrites the target heading's text. The
// rewritten range sits strictly before the replaced range, so the two edits
// never overlap.
func spliceReplaceSection(body, fragment string, opts SpliceOptions) (SpliceResult, error) {
	heading := opts.Heading
	match, err := locateHeading(body, heading)
	if err != nil {
		return SpliceResult{}, err
	}

	if err := validateRename(body, match, heading, opts.NewHeading); err != nil {
		return SpliceResult{}, err
	}

	// Fragments often repeat the section heading (e.g. an agent sends
	// "## Data scrubbing\n\n..."), which would duplicate it since the target
	// heading is preserved. Strip a leading heading matching the target or,
	// for a rename, the new heading, since an agent renaming a section
	// naturally puts the new name at the top of its fragment.
	fragment = stripLeadingHeading(fragment, heading, opts.NewHeading)

	// The replaced region is [match.headingEndOff, ext.stop); findSectionExtent
	// also returns the summary of removed elements and nested subsections.
	ext, err := findSectionExtent(body, match, opts.IncludeSubsections)
	if err != nil {
		return SpliceResult{}, err
	}
	if ext.unbalanced {
		return SpliceResult{}, fmt.Errorf(
			"%w: a sibling element in this section opens before the replaced range ends but does not close before it — "+
				"replacing it would delete that element's opening tag and orphan its closing tag; use action \"update\" instead",
			ErrSectionBoundaryUnbalanced,
		)
	}

	replacedByteCount := ext.stop - match.headingEndOff
	merged := renamedPrefix(body, match, opts.NewHeading) + fragment + body[ext.stop:]

	startAnchor := fmt.Sprintf("after </h%d> %q", match.level, heading)
	endAnchor, container := sectionStopAnchor(body, ext.stop, match.layoutCellDepth, !opts.IncludeSubsections)

	boundary := BoundaryInfo{
		StartAnchor:            startAnchor,
		EndAnchor:              endAnchor,
		Container:              container,
		CrossesLayout:          false,
		ReplacedByteCount:      replacedByteCount,
		ReplacedElementSummary: summariseTags(ext.replacedTags),
	}
	// A rename is reported explicitly so the caller isn't left inferring it
	// from the byte delta; the broken anchor references are its collateral
	// damage, reported alongside it.
	if opts.NewHeading != "" {
		boundary.HeadingRenamed = &HeadingRename{From: heading, To: opts.NewHeading}
		boundary.AnchorReferences = findAnchorReferences(body, heading)
	}
	// The same headings are either destroyed or kept, depending on the extent —
	// report them under the field that says which, so neither outcome is
	// silent.
	if opts.IncludeSubsections {
		boundary.ReplacedSections = ext.nestedHeadings
	} else {
		boundary.PreservedSections = ext.nestedHeadings
	}

	return SpliceResult{Merged: merged, Boundary: boundary}, nil
}

// reLeadingHeading matches an optional leading whitespace run followed by a
// single <hN>...</hN> element and any trailing whitespace. Go's RE2 has no
// backreferences; the closing level is checked post-match rather than in the
// pattern. The inner content is captured for comparison against the target
// heading text.
var reLeadingHeading = regexp.MustCompile(`(?s)\A\s*<h([1-6])[^>]*>(.*?)</h([1-6])>\s*`)

// stripLeadingHeading removes a leading <hN>...</hN> element from fragment
// when its text content (normalized) matches any of targets. Returns fragment
// unchanged if the fragment does not start with a heading or no target
// matches. Level is ignored so a fragment that prefixes a different level than
// the target is still cleaned. Empty targets are skipped, so a caller can pass
// an unset new heading without a guard.
func stripLeadingHeading(fragment string, targets ...string) string {
	loc := reLeadingHeading.FindStringSubmatchIndex(fragment)
	if loc == nil {
		return fragment
	}
	openLevel := fragment[loc[2]:loc[3]]
	closeLevel := fragment[loc[6]:loc[7]]
	if openLevel != closeLevel {
		return fragment
	}
	got := normalizeHeading(extractText(fragment[loc[4]:loc[5]]))
	for _, target := range targets {
		if target != "" && got == normalizeHeading(target) {
			return fragment[loc[1]:]
		}
	}
	return fragment
}

// summariseTags turns a document-order list of replaced elements into a
// histogram like ["<p> x 2", "macro \"toc\" x 1"]. The order is document-order
// first appearance.
//
// A named structured-macro is keyed and displayed by its ac:name rather than
// its tag name, so two macros with different names never collapse into one
// misleading "structured-macro x 2" entry — the caller needs to know which
// macros are about to be destroyed, not just how many. An unnamed macro falls
// back to its tag name for both the key and the display.
func summariseTags(elements []replacedElement) []string {
	if len(elements) == 0 {
		return nil
	}
	type entry struct {
		display string
		count   int
	}
	counts := make(map[string]*entry, len(elements))
	order := make([]string, 0, len(elements))
	for _, e := range elements {
		key := e.name
		display := fmt.Sprintf("<%s>", e.name)
		if e.name == "structured-macro" {
			if e.macroName != "" {
				key = "macro:" + e.macroName
				display = fmt.Sprintf("macro %q", e.macroName)
			} else {
				display = "<ac:structured-macro>"
			}
		}
		if _, ok := counts[key]; !ok {
			counts[key] = &entry{display: display}
			order = append(order, key)
		}
		counts[key].count++
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		e := counts[key]
		out = append(out, fmt.Sprintf("%s x %d", e.display, e.count))
	}
	return out
}
