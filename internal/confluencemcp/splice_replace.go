package confluencemcp

import (
	"errors"
	"fmt"
	"strings"
)

// sentinel used internally to stop the walker once we've found the stop point.
var errStopWalk = errors.New("stop walk")

// spliceReplaceSection replaces the content under the target heading up to
// the stop point defined by the section rules. See
// internal/confluencemcp/.../design doc for the full rule.
func spliceReplaceSection(body, fragment, heading string) (SpliceResult, error) {
	match, err := locateHeading(body, heading)
	if err != nil {
		return SpliceResult{}, err
	}

	// The replaced region starts at match.headingEndOff (just after </hN>) and
	// extends up to the stop offset. We also collect top-level element names
	// seen between the heading end and the stop, for the replaced-element
	// summary.

	targetLayoutDepth := match.layoutCellDepth
	targetMacroDepth := match.macroDepth
	targetLevel := match.level
	var (
		stopOff         = len(body) // default: end of body (no-layout case)
		replacedTags    []string    // element local-names at the immediate depth
		topLevelStarted bool
	)
	// topLevelStarted tracks whether we've entered a top-level element. When we
	// see a start-element whose reported depth (layoutCellDepth, macroDepth)
	// equals the target's, and we're not already inside one, it's a new
	// top-level element. We increment count then ignore further starts until
	// we leave it.
	topLevelOpenTag := ""

	err = walkStorage(body, func(ev walkEvent) error {
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
	if err != nil && !errors.Is(err, errStopWalk) {
		return SpliceResult{}, fmt.Errorf("walk body: %w", err)
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
