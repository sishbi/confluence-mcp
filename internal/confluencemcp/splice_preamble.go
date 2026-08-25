package confluencemcp

import (
	"errors"
	"fmt"
)

// containerInfo describes the structural container a start-of-body or
// preamble-scoped splice operates within: the first <ac:layout-cell> in the
// body when the body has a layout wrapper, and the document root otherwise.
type containerInfo struct {
	// start is the byte offset just past the container's own opening tag (0
	// for the document root, which has no opening tag of its own).
	start int
	// end is the byte offset of the container's own closing tag (len(body)
	// for the document root).
	end int
	// layoutCellDepth is the layoutCellDepth reported at the container's
	// opening tag: 0 for the document root, 1 for a layout-cell.
	layoutCellDepth int
	// name is the display name used in BoundaryInfo.Container.
	name string
}

// locateContainer finds the innermost LEADING container a start-of-body or
// preamble splice targets: the FIRST <ac:layout-cell> in the body if one
// exists, and the document root otherwise.
//
// This is the mirror image of spliceEnd, which takes the LAST layout-cell —
// an intentional asymmetry: on a two-column page, "end" writes into the
// right (last) cell, "start" or a preamble edit into the left (first) one.
// Layout cells are siblings, never nested, so the first cell's own closing
// tag is simply the first "layout-cell" end event after its opening tag.
func locateContainer(body string) (containerInfo, error) {
	info := containerInfo{end: len(body), name: "document root"}
	var haveLayoutCell, closed bool

	err := walkStorage(body, func(ev walkEvent) error {
		switch ev.kind {
		case eventStart:
			if !haveLayoutCell && ev.name == "layout-cell" {
				haveLayoutCell = true
				info.start = ev.tokEnd
				info.layoutCellDepth = ev.layoutCellDepth
				info.name = "ac:layout-cell"
			}
		case eventEnd:
			if haveLayoutCell && !closed && ev.name == "layout-cell" &&
				ev.layoutCellDepth == info.layoutCellDepth {
				if ev.tokStart == ev.tokEnd {
					// A self-closing <ac:layout-cell/> reports its synthetic end
					// token at tokStart == tokEnd. Testing ev.tokStart ==
					// info.start instead would also match a genuine empty pair
					// (<ac:layout-cell></ac:layout-cell>), which is a valid,
					// usable container — only the zero-width check tells the
					// two apart. Splicing into a true self-closing cell would
					// land the fragment outside any cell (an invalid direct
					// child of <ac:layout-section>), so keep looking for the
					// next cell instead.
					haveLayoutCell = false
					info = containerInfo{end: len(body), name: "document root"}
					return nil
				}
				info.end = ev.tokStart
				closed = true
			}
		}
		return nil
	})
	if err != nil {
		return containerInfo{}, err
	}
	return info, nil
}

// preambleExtent describes the region of a page's body that precedes its
// first heading — the "preamble" — and the container it is bounded by.
//
// containerStart and containerEnd bound the container itself (see
// containerInfo). firstHeadingStart is the offset of the first heading
// inside that container that locateHeading would ever be able to find;
// firstHeadingText is its normalised text, for reporting.
type preambleExtent struct {
	containerStart    int
	containerEnd      int
	firstHeadingStart int
	firstHeadingText  string
	layoutCellDepth   int
	container         string
}

// locatePreambleExtent finds the region between a page's container and its
// first locatable heading, for the two preamble-editing splice modes
// (ModeStart and ModeReplacePreamble).
//
// The boundary uses the same candidacy test as locateHeading — macroDepth ==
// 0 and unsafeContainerDepth == 0 — so a heading buried in a macro or an
// unsafe container (see splice_walker.go) can never end the preamble: it
// could never be located by name either, and letting it end the preamble
// anyway would make the two mechanisms disagree about the page's structure.
//
// The preamble never crosses a layout cell: only headings within
// [containerStart, containerEnd) count. With none in range, ErrNoHeadingOnPage
// is returned rather than a range spilling into the next cell.
func locatePreambleExtent(body string) (preambleExtent, error) {
	container, err := locateContainer(body)
	if err != nil {
		return preambleExtent{}, err
	}

	ext := preambleExtent{
		containerStart:    container.start,
		containerEnd:      container.end,
		firstHeadingStart: -1,
		layoutCellDepth:   container.layoutCellDepth,
		container:         container.name,
	}

	var (
		pendingStart     = -1
		pendingCandidate bool
	)

	err = walkStorage(body, func(ev walkEvent) error {
		switch ev.kind {
		case eventHeadingStart:
			if ext.firstHeadingStart >= 0 {
				return nil
			}
			if ev.tokStart >= container.start && ev.tokStart < container.end &&
				ev.macroDepth == 0 && ev.unsafeContainerDepth == 0 {
				pendingStart = ev.tokStart
				pendingCandidate = true
			} else {
				pendingCandidate = false
			}
		case eventHeadingEnd:
			if pendingCandidate {
				ext.firstHeadingStart = pendingStart
				ext.firstHeadingText = normalizeHeading(extractText(body[pendingStart:ev.tokEnd]))
				pendingCandidate = false
			}
		}
		return nil
	})
	if err != nil {
		return preambleExtent{}, err
	}

	if ext.firstHeadingStart < 0 {
		return preambleExtent{}, ErrNoHeadingOnPage
	}
	return ext, nil
}

// depths returns the preamble's container's own three-dimensional depth key:
// macroDepth and unsafeContainerDepth are always 0, since a container is by
// definition never itself inside a macro or an unsafe container, and
// layoutCellDepth is the depth recorded at the container's own opening tag.
func (e preambleExtent) depths() depthKey {
	return depthKey{layoutCellDepth: e.layoutCellDepth}
}

// spliceStart inserts fragment at the very start of the CONTAINER: byte 0 of
// the body, or just past the opening tag of the first <ac:layout-cell> when
// the body has a layout wrapper.
//
// Unlike locatePreambleExtent, spliceStart needs only the container half —
// "the start of the container" is well-defined on a page with no headings at
// all, so a headless body is not an error here, unlike ModeReplacePreamble
// which needs a heading to bound the replaced range.
func spliceStart(body, fragment string) (SpliceResult, error) {
	container, err := locateContainer(body)
	if err != nil {
		return SpliceResult{}, err
	}

	merged := body[:container.start] + fragment + body[container.start:]

	insertAnchor := "start of body (no layout wrapper)"
	if container.layoutCellDepth > 0 {
		insertAnchor = "after opening <ac:layout-cell>"
	}

	return SpliceResult{
		Merged: merged,
		Boundary: BoundaryInfo{
			InsertAnchor: insertAnchor,
			Container:    container.name,
		},
	}, nil
}

// collectTopLevelSiblings walks body over [start, stop) and returns, in
// document order, each element isTopLevelSibling(ev, target) reports as a
// direct sibling of target — the same three-dimensional rule
// findSectionExtent uses for a section-scoped replace, applied here to a
// preamble's container instead of a heading.
//
// A plain wrapper (e.g. <div>) moves none of the three depth counters, so an
// element of the SAME name nested inside it reports an identical depth key —
// isTopLevelSibling alone cannot tell an outer wrapper from one nested inside
// it. openDepth tracks unclosed starts of the current top-level sibling's tag
// name since it began, incrementing on a same-name start and decrementing on
// a same-name end; the sibling counts as closed, and a new one becomes
// eligible, only once it returns to zero.
//
// The second return value reports whether the walk ended with an unclosed
// top-level sibling still open — i.e. stop fell strictly inside an element
// that started before it but whose own closing tag lies at or beyond stop.
// That shape means [start, stop) is not "balanced": splicing exactly that
// range would delete the sibling's opening tag while leaving its closing tag
// behind, orphaned. A section-scoped replace can never produce this (its
// range is bounded by construction — see findSectionExtent), but a
// preamble's range is bounded by an unrelated heading's position, which can
// land inside an open wrapper the container/heading machinery never looks
// at (e.g. a <div>). Callers that can receive an unbalanced range must check
// this and refuse rather than splice.
func collectTopLevelSiblings(body string, start, stop int, target depthKey) ([]replacedElement, bool, error) {
	var (
		tags            []replacedElement
		topLevelOpenTag string
		openDepth       int
	)

	err := walkStorage(body, func(ev walkEvent) error {
		if ev.tokStart < start {
			return nil
		}
		// A self-closing element's synthetic end event shares tokEnd with its
		// own start event, which can equal stop exactly. That end event still
		// belongs to [start, stop) — only a START-kind event at exactly stop
		// really crosses the boundary (typically the heading that defines
		// stop); an END-kind event there must still be processed, or an
		// adjacent self-closing element would look unclosed and wrongly trip
		// the balance guard below.
		startsHere := ev.kind == eventStart || ev.kind == eventHeadingStart
		if ev.tokStart > stop || (ev.tokStart == stop && startsHere) {
			return errStopWalk
		}
		switch ev.kind {
		case eventStart, eventHeadingStart:
			switch {
			case openDepth == 0 && isTopLevelSibling(ev, target):
				tags = append(tags, replacedElement{name: ev.name, macroName: ev.macroName})
				topLevelOpenTag = ev.name
				openDepth = 1
			case openDepth > 0 && ev.name == topLevelOpenTag:
				openDepth++
			}
		case eventEnd, eventHeadingEnd:
			if openDepth > 0 && ev.name == topLevelOpenTag {
				openDepth--
				if openDepth == 0 {
					topLevelOpenTag = ""
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, false, fmt.Errorf("walk body: %w", err)
	}
	return tags, openDepth > 0, nil
}

// spliceReplacePreamble replaces a page's preamble — everything from the
// container's start up to (not including) its first locatable heading — with
// fragment. The heading itself and everything from it onward are untouched.
//
// Unlike spliceReplaceSection, fragment is never checked for a leading
// heading to strip: there is no preserved target heading here to duplicate,
// so a fragment opening with one (e.g. a scope statement under its own H1)
// is ordinary preamble content, inserted whole.
func spliceReplacePreamble(body, fragment string) (SpliceResult, error) {
	ext, err := locatePreambleExtent(body)
	if err != nil {
		return SpliceResult{}, err
	}

	tags, unbalanced, err := collectTopLevelSiblings(body, ext.containerStart, ext.firstHeadingStart, ext.depths())
	if err != nil {
		return SpliceResult{}, err
	}
	if unbalanced {
		return SpliceResult{}, fmt.Errorf(
			"%w: the first heading sits inside an element that opens before it in the preamble but does not close before it — "+
				"replacing the preamble would delete that element's opening tag and orphan its closing tag; use action \"update\" instead",
			ErrPreambleBoundaryUnbalanced,
		)
	}

	merged := body[:ext.containerStart] + fragment + body[ext.firstHeadingStart:]

	return SpliceResult{
		Merged: merged,
		Boundary: BoundaryInfo{
			StartAnchor:            "start of container",
			EndAnchor:              fmt.Sprintf("before first heading %q", ext.firstHeadingText),
			Container:              ext.container,
			CrossesLayout:          false,
			ReplacedByteCount:      ext.firstHeadingStart - ext.containerStart,
			ReplacedElementSummary: summariseTags(tags),
		},
	}, nil
}
