package confluencemcp

import (
	"errors"
	"fmt"
	"strings"
)

// errStopWalk is a sentinel used internally to stop the walker once we've
// found the stop point.
var errStopWalk = errors.New("stop walk")

// findSectionEnd walks body starting from match's heading and determines the
// stop offset for the section: the byte offset where the section's content
// ends. This is the boundary used both by replace-section (to know what to
// remove) and by end-of-section (to know where to insert).
//
// The stop conditions, evaluated in document order after the heading's
// closing tag:
//   - A heading start at level <= match.level, at the same layoutCellDepth
//     and macroDepth as the target heading — stop at that heading's start.
//   - The close of the containing ac:layout-cell, when the target heading is
//     itself inside a layout-cell (targetLayoutDepth > 0) — stop at the close
//     tag's start.
//   - Otherwise, the stop offset defaults to len(body) (no-layout case).
//
// Along the way, findSectionEnd also collects the element local-names of the
// top-level siblings between the heading and the stop offset (i.e. elements
// at exactly the target's layoutCellDepth & macroDepth), for the
// replaced-element summary used by replace-section.
func findSectionEnd(body string, match headingMatch) (stopOff int, replacedTags []string, err error) {
	targetLayoutDepth := match.layoutCellDepth
	targetMacroDepth := match.macroDepth
	targetLevel := match.level
	var topLevelStarted bool
	stopOff = len(body) // default: end of body (no-layout case)
	// topLevelStarted tracks whether we've entered a top-level element. When we
	// see a start-element whose reported depth (layoutCellDepth, macroDepth)
	// equals the target's, and we're not already inside one, it's a new
	// top-level element. We append its name once, then ignore further starts
	// until we leave it.
	topLevelOpenTag := ""

	walkErr := walkStorage(body, func(ev walkEvent) error {
		// Ignore anything before the heading's closing tag.
		if ev.tokEnd <= match.headingEndOff {
			return nil
		}

		// Check stop conditions first — evaluated on every event.
		if ev.kind == eventHeadingStart &&
			ev.level <= targetLevel &&
			ev.layoutCellDepth == targetLayoutDepth &&
			ev.macroDepth == targetMacroDepth {
			stopOff = ev.tokStart
			return errStopWalk
		}
		// Exiting the containing layout-cell: stop at its close tag.
		if ev.kind == eventEnd && ev.name == "layout-cell" &&
			ev.layoutCellDepth == targetLayoutDepth && targetLayoutDepth > 0 {
			stopOff = ev.tokStart
			return errStopWalk
		}

		// Track top-level replaced elements for the summary. We count each
		// element that starts at exactly targetLayoutDepth & targetMacroDepth
		// (i.e. a sibling of the target heading), once per element.
		switch ev.kind {
		case eventStart, eventHeadingStart:
			if !topLevelStarted &&
				ev.layoutCellDepth == targetLayoutDepth &&
				ev.macroDepth == targetMacroDepth {
				replacedTags = append(replacedTags, ev.name)
				topLevelStarted = true
				topLevelOpenTag = ev.name
			}
		case eventEnd, eventHeadingEnd:
			if topLevelStarted && ev.name == topLevelOpenTag &&
				ev.layoutCellDepth == targetLayoutDepth &&
				ev.macroDepth == targetMacroDepth {
				topLevelStarted = false
				topLevelOpenTag = ""
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStopWalk) {
		return 0, nil, fmt.Errorf("walk body: %w", walkErr)
	}

	return stopOff, replacedTags, nil
}

// sectionStopAnchor derives the container name and an anchor phrase
// describing where a section-scoped splice (replace-section or
// end-of-section) stopped, per the shared findSectionEnd heuristic: if the
// stop offset lands on a heading start rather than the close of the
// containing ac:layout-cell or the end of the body, describe it as "before
// next heading at same or higher level"; otherwise describe it as the end of
// the container. Shared by spliceReplaceSection and spliceEndOfSection so the
// phrasing stays identical across both modes.
func sectionStopAnchor(body string, stopOff int, targetLayoutDepth int) (anchor, container string) {
	container = "document root"
	if targetLayoutDepth > 0 {
		container = "ac:layout-cell"
	}
	anchor = "end of " + container
	// If we stopped at a heading rather than a container close, report that.
	// We can detect this by re-walking the original body to find the element at
	// stopOff — but a simpler heuristic is: if stopOff < end of body, it's a
	// heading stop.
	if stopOff < len(body) && (targetLayoutDepth == 0 || !strings.HasPrefix(body[stopOff:], "</ac:layout-cell>")) {
		anchor = "before next heading at same or higher level"
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

	stopOff, _, err := findSectionEnd(body, match)
	if err != nil {
		return SpliceResult{}, err
	}

	merged := body[:stopOff] + fragment + body[stopOff:]

	insertAnchor, container := sectionStopAnchor(body, stopOff, match.layoutCellDepth)

	return SpliceResult{
		Merged: merged,
		Boundary: BoundaryInfo{
			InsertAnchor: insertAnchor,
			Container:    container,
		},
	}, nil
}
