package confluencemcp

import (
	"errors"
	"fmt"
	"strings"
)

// errStopWalk is a sentinel used internally to stop the walker once we've
// found the stop point.
var errStopWalk = errors.New("stop walk")

// replacedElement is one top-level sibling collected for the replaced-element
// summary: its local name, and its macroName when it is a named
// structured-macro (empty otherwise).
type replacedElement struct {
	name      string
	macroName string
}

// sectionExtent describes the region a section-scoped splice covers.
type sectionExtent struct {
	// stop is the byte offset where the covered region ends.
	stop int
	// replacedTags holds the top-level siblings between the heading and stop,
	// in document order.
	replacedTags []replacedElement
	// nestedHeadings holds the text of the deeper headings inside the FULL
	// section (up to the next same-or-higher-level heading), regardless of
	// where stop landed: subsections left intact when stop is the narrow
	// boundary, subsections being removed when it is the full boundary.
	// Callers label them accordingly.
	nestedHeadings []string
	// unbalanced is true when a sibling between the heading and stop opens
	// but does not close before stop (see collectTopLevelSiblings). A caller
	// that REPLACES [headingEndOff, stop) must refuse when this is set; a
	// caller that only INSERTS at stop is unaffected.
	unbalanced bool
}

// findSectionExtent walks body starting from match's heading and determines
// the region the section-scoped splice covers. It is used by replace-section
// (to know what to remove) and by end-of-section (to know where to insert).
//
// Two candidate stops are tracked: the FULL stop (first heading start at
// level <= match.level, the containing ac:layout-cell's close, or end of
// body) and the NARROW stop (first heading start of ANY level, i.e. the
// section's first subsection). includeSubsections selects between them:
// false (replace-section's default) so replacing a section never silently
// deletes its subsections; true always for end-of-section, whose insert
// belongs after the last subsection, not before the first.
//
// Heading stops are only taken at the target heading's layoutCellDepth,
// macroDepth, and unsafeContainerDepth — a heading buried in a macro or an
// unsafe container (see splice_walker.go) is never a candidate, matching
// locateHeading, so the two mechanisms agree on where the page's real
// structure starts. layoutCellDepth is a per-tag LIFO counter, so the first
// layout-cell close at the target's depth is the target's own cell.
//
// Once the stop is chosen, a second pass over [match.headingEndOff, ext.stop)
// via collectTopLevelSiblings gathers the top-level siblings in that range,
// using the same three-dimensional isTopLevelSibling rule a preamble-scoped
// replace uses (see splice_preamble.go), applied here to a heading anchor
// instead of a container.
func findSectionExtent(body string, match headingMatch, includeSubsections bool) (sectionExtent, error) {
	targetLayoutDepth := match.layoutCellDepth
	targetLevel := match.level
	fullStop := len(body) // default: end of body (no-layout case)
	narrowStop := -1      // -1 until a subsection heading is seen
	nestedStart := -1     // open subsection heading awaiting its closing tag
	var nestedHeadings []string

	walkErr := walkStorage(body, func(ev walkEvent) error {
		// Ignore anything before the heading's closing tag.
		if ev.tokEnd <= match.headingEndOff {
			return nil
		}

		// Check stop conditions — evaluated on every event.
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
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStopWalk) {
		return sectionExtent{}, fmt.Errorf("walk body: %w", walkErr)
	}

	ext := sectionExtent{stop: fullStop, nestedHeadings: nestedHeadings}
	if !includeSubsections && narrowStop >= 0 {
		ext.stop = narrowStop
	}

	// The chosen stop can land inside an unclosed plain wrapper (e.g. <div>),
	// which moves none of the three counters this walk keys on — the same
	// shape ErrPreambleBoundaryUnbalanced guards against. collectTopLevelSiblings'
	// unbalanced flag catches it; only a REPLACING caller (spliceReplaceSection)
	// needs to refuse on it — an INSERTING caller (spliceEndOfSection) places
	// its fragment at stop, a genuine token boundary, without touching
	// anything before it, so it is unaffected.
	tags, unbalanced, err := collectTopLevelSiblings(body, match.headingEndOff, ext.stop, match.depths())
	if err != nil {
		return sectionExtent{}, err
	}
	ext.replacedTags = tags
	ext.unbalanced = unbalanced
	return ext, nil
}

// depthKey is the three-dimensional depth an anchor sits at — a heading (see
// headingMatch.depths) or a preamble's container (see containerInfo.depths)
// — that isTopLevelSibling compares an event's own depth against to decide
// whether it is a direct sibling of that anchor.
type depthKey struct {
	layoutCellDepth      int
	macroDepth           int
	unsafeContainerDepth int
}

// depths returns match's own three-dimensional depth, the anchor
// isTopLevelSibling compares against for a section-scoped splice. A located
// heading is always "safe" (see locateHeading), so its macroDepth and
// unsafeContainerDepth are always 0 — only layoutCellDepth varies.
func (m headingMatch) depths() depthKey {
	return depthKey{
		layoutCellDepth:      m.layoutCellDepth,
		macroDepth:           m.macroDepth,
		unsafeContainerDepth: m.unsafeContainerDepth,
	}
}

// isTopLevelSibling reports whether ev is a direct sibling of the anchor at
// target — an element at target's layoutCellDepth and macroDepth, and at
// target's unsafeContainerDepth.
//
// A top-level sibling that is itself a structured-macro or one of the
// unsafeContainerTags is a special case: the walker increments that depth
// counter before reporting the element's own start event and reports it
// pre-decrement on the matching close event (see splice_walker.go), so the
// element's own start/end events are seen one level deeper than its
// siblings' — at target's depth+1, not target's own depth. Without these
// branches such a sibling, and its whole subtree, would be silently skipped
// rather than counted.
func isTopLevelSibling(ev walkEvent, target depthKey) bool {
	if ev.layoutCellDepth != target.layoutCellDepth {
		return false
	}
	if ev.name == "structured-macro" &&
		ev.macroDepth == target.macroDepth+1 &&
		ev.unsafeContainerDepth == target.unsafeContainerDepth {
		return true
	}
	if ev.macroDepth != target.macroDepth {
		return false
	}
	if ev.unsafeContainerDepth == target.unsafeContainerDepth {
		return true
	}
	return unsafeContainerTags[ev.name] && ev.unsafeContainerDepth == target.unsafeContainerDepth+1
}

// sectionStopAnchor derives the container name and an anchor phrase
// describing where a section-scoped splice (replace-section or
// end-of-section) stopped: if the stop offset lands on a heading start rather
// than the close of the containing ac:layout-cell or the end of the body,
// name which heading stopped it; otherwise describe it as the end of the
// container.
//
// stopAtAnyHeading must mirror the extent the caller asked for: a
// replace-section that keeps its subsections stops at the next heading of ANY
// level, not same-or-higher, and the anchor text must say so.
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
// Unlike spliceReplaceSection, the heading is untouched and
// stripLeadingHeading is never called: a fragment here is typically a new
// sibling or child section starting with its own heading, which must survive.
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
