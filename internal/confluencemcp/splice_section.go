package confluencemcp

import (
	"errors"
	"fmt"
	"strings"
)

// errStopWalk is a sentinel used internally to stop the walker once we've
// found the stop point.
var errStopWalk = errors.New("stop walk")

// sectionExtent describes the region a section-scoped splice covers.
type sectionExtent struct {
	// stop is the byte offset where the covered region ends.
	stop int
	// replacedTags holds the element local-names of the top-level siblings
	// between the heading and stop, in document order.
	replacedTags []string
	// nestedHeadings holds the text of the deeper headings inside the FULL
	// section (i.e. up to the next same-or-higher-level heading), regardless of
	// where stop landed. When stop is the narrow boundary these are the
	// subsections left intact; when it is the full boundary they are the
	// subsections being removed. Callers label them accordingly.
	nestedHeadings []string
}

// findSectionExtent walks body starting from match's heading and determines
// the region the section-scoped splice covers. It is used by replace-section
// (to know what to remove) and by end-of-section (to know where to insert).
//
// Two candidate stop points are tracked:
//   - the FULL stop — the first heading start at level <= match.level, or the
//     close of the containing ac:layout-cell, or the end of the body;
//   - the NARROW stop — the first heading start of ANY level, i.e. the start
//     of the section's first subsection.
//
// includeSubsections selects between them: false (replace-section's default)
// covers only up to the first subsection, so replacing a section does not
// silently delete its subsections; true covers the whole section, which is
// also what end-of-section always wants (its insert belongs after the last
// subsection, not before the first).
//
// Heading stops are only taken at the same layoutCellDepth, macroDepth, and
// unsafeContainerDepth as the target heading. The unsafeContainerDepth check
// matters because the unsafeContainerTags (see splice_walker.go) don't move
// layoutCellDepth or macroDepth, so a heading buried in one of them would
// otherwise be taken as the stop. layoutCellDepth is a per-tag LIFO counter,
// so the first layout-cell close at the target's depth is the target's own
// cell.
//
// Along the way, findSectionExtent collects the element local-names of the
// top-level siblings between the heading and the stop offset — i.e. elements
// that isTopLevelSibling considers direct siblings of the target heading
// (layoutCellDepth, macroDepth, and unsafeContainerDepth all in step with the
// target; see that function for the full three-dimensional rule) — for the
// replaced-element summary used by replace-section.
func findSectionExtent(body string, match headingMatch, includeSubsections bool) (sectionExtent, error) {
	targetLayoutDepth := match.layoutCellDepth
	targetLevel := match.level
	fullStop := len(body) // default: end of body (no-layout case)
	narrowStop := -1      // -1 until a subsection heading is seen
	nestedStart := -1     // open subsection heading awaiting its closing tag
	var (
		nestedHeadings  []string
		topLevelStarted bool
	)
	// topLevelStarted tracks whether we've entered a top-level element. When we
	// see a start-element that isTopLevelSibling reports as a sibling of
	// match, and we're not already inside one, it's a new top-level element.
	// We record its name once, then ignore further starts until we leave it.
	topLevelOpenTag := ""
	// Tags are recorded with their offsets so they can be trimmed to whichever
	// stop is selected below.
	type tagAt struct {
		name string
		off  int
	}
	var tags []tagAt

	walkErr := walkStorage(body, func(ev walkEvent) error {
		// Ignore anything before the heading's closing tag.
		if ev.tokEnd <= match.headingEndOff {
			return nil
		}

		// Check stop conditions first — evaluated on every event.
		if ev.kind == eventHeadingStart &&
			ev.layoutCellDepth == targetLayoutDepth &&
			ev.macroDepth == match.macroDepth &&
			ev.unsafeContainerDepth == match.unsafeContainerDepth {
			if ev.level <= targetLevel {
				fullStop = ev.tokStart
				return errStopWalk
			}
			// A subsection: the narrow stop, and one of the nested headings
			// reported back to the caller.
			if narrowStop < 0 {
				narrowStop = ev.tokStart
			}
			nestedStart = ev.tokStart
		}
		if ev.kind == eventHeadingEnd && nestedStart >= 0 {
			nestedHeadings = append(nestedHeadings, normalizeHeading(extractText(body[nestedStart:ev.tokEnd])))
			nestedStart = -1
		}
		// Exiting the containing layout-cell: stop at its close tag.
		if ev.kind == eventEnd && ev.name == "layout-cell" &&
			ev.layoutCellDepth == targetLayoutDepth && targetLayoutDepth > 0 {
			fullStop = ev.tokStart
			return errStopWalk
		}

		// Track top-level replaced elements for the summary. We count each
		// element isTopLevelSibling reports as a direct sibling of the target
		// heading, once per element.
		switch ev.kind {
		case eventStart, eventHeadingStart:
			if !topLevelStarted && isTopLevelSibling(ev, match) {
				tags = append(tags, tagAt{name: ev.name, off: ev.tokStart})
				topLevelStarted = true
				topLevelOpenTag = ev.name
			}
		case eventEnd, eventHeadingEnd:
			if topLevelStarted && ev.name == topLevelOpenTag &&
				isTopLevelSibling(ev, match) {
				topLevelStarted = false
				topLevelOpenTag = ""
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStopWalk) {
		return sectionExtent{}, fmt.Errorf("walk body: %w", walkErr)
	}

	ext := sectionExtent{stop: fullStop, nestedHeadings: nestedHeadings}
	if !includeSubsections && narrowStop >= 0 {
		ext.stop = narrowStop
	}
	for _, t := range tags {
		if t.off < ext.stop {
			ext.replacedTags = append(ext.replacedTags, t.name)
		}
	}
	return ext, nil
}

// isTopLevelSibling reports whether ev is a direct sibling of match's heading
// for the purpose of the replaced-element summary — i.e. an element at
// match's layoutCellDepth and macroDepth, and at match's
// unsafeContainerDepth.
//
// A top-level sibling that is itself one of the unsafeContainerTags (e.g. a
// <blockquote> or <ac:adf-extension> sibling of the heading) is a special
// case: the walker increments unsafeContainerDepth before reporting a start
// event and reports it pre-decrement on the matching close event (see
// splice_walker.go), so that element's own start/end events are seen one
// level deeper than its siblings' — not at match.unsafeContainerDepth like
// every other top-level sibling, but at match.unsafeContainerDepth+1. Without
// this second branch such a sibling (and its whole subtree) would be
// silently skipped rather than counted.
func isTopLevelSibling(ev walkEvent, match headingMatch) bool {
	if ev.layoutCellDepth != match.layoutCellDepth || ev.macroDepth != match.macroDepth {
		return false
	}
	if ev.unsafeContainerDepth == match.unsafeContainerDepth {
		return true
	}
	return unsafeContainerTags[ev.name] && ev.unsafeContainerDepth == match.unsafeContainerDepth+1
}

// sectionStopAnchor derives the container name and an anchor phrase
// describing where a section-scoped splice (replace-section or
// end-of-section) stopped, per the shared findSectionExtent heuristic: if the
// stop offset lands on a heading start rather than the close of the
// containing ac:layout-cell or the end of the body, describe which heading
// stopped it; otherwise describe it as the end of the container.
//
// stopAtAnyHeading must mirror the extent the caller asked for: a
// replace-section that keeps its subsections stops at the next heading of ANY
// level, and saying "at same or higher level" there is precisely the wording
// that made the old subsection-swallowing behaviour look intentional and safe.
func sectionStopAnchor(body string, stopOff int, targetLayoutDepth int, stopAtAnyHeading bool) (anchor, container string) {
	container = "document root"
	if targetLayoutDepth > 0 {
		container = "ac:layout-cell"
	}
	anchor = "end of " + container
	// A heading stop leaves stopOff short of the body's end and not pointing
	// at the layout-cell's own closing tag; a container-close stop points
	// exactly at "</ac:layout-cell>" (or, with no layout, never stops short).
	if stopOff < len(body) && (targetLayoutDepth == 0 || !strings.HasPrefix(body[stopOff:], "</ac:layout-cell>")) {
		anchor = "before next heading at same or higher level"
		if stopAtAnyHeading {
			anchor = "before next heading of any level"
		}
	}
	return anchor, container
}

// spliceEndOfSection inserts fragment at the END of the named section: after
// the section's existing content, before the next same-or-higher-level
// heading or the close of the containing layout-cell.
//
// Unlike spliceReplaceSection, the target heading itself is not part of the
// inserted region and is never touched — the fragment is inserted whole,
// including any leading heading it may carry. A fragment beginning with a
// heading is the primary use case here (adding a new sibling or child
// section), so — deliberately, unlike spliceReplaceSection — stripLeadingHeading
// is never called: stripping would silently discard the new section's title.
func spliceEndOfSection(body, fragment, heading string) (SpliceResult, error) {
	match, err := locateHeading(body, heading)
	if err != nil {
		return SpliceResult{}, err
	}

	// end-of-section always covers the whole section: the insert belongs after
	// the section's last subsection, not before its first.
	ext, err := findSectionExtent(body, match, true)
	if err != nil {
		return SpliceResult{}, err
	}

	merged := body[:ext.stop] + fragment + body[ext.stop:]

	insertAnchor, container := sectionStopAnchor(body, ext.stop, match.layoutCellDepth, false)

	return SpliceResult{
		Merged: merged,
		Boundary: BoundaryInfo{
			InsertAnchor: insertAnchor,
			Container:    container,
		},
	}, nil
}
