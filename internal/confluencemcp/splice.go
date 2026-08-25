package confluencemcp

import "errors"

// Mode selects how a fragment is spliced into a page's storage body.
type Mode int

const (
	// ModeEnd inserts the fragment at the end of the body, inside the innermost
	// trailing ac:layout-cell if present.
	ModeEnd Mode = iota
	// ModeAfterHeading inserts the fragment at the TOP of a named section:
	// immediately after the heading's closing tag, directly beneath the
	// heading and ABOVE the section's existing content.
	ModeAfterHeading
	// ModeReplaceSection replaces the content under a named heading (exclusive of
	// the heading itself) up to the next heading of any level, or the end of the
	// containing layout-cell. SpliceOptions.IncludeSubsections widens the range
	// to the next same-or-higher-level heading, replacing the section's
	// subsections too.
	ModeReplaceSection
	// ModeEndOfSection inserts the fragment at the END of a named section:
	// after the section's existing content, before the next
	// same-or-higher-level heading or the close of the containing layout-cell.
	ModeEndOfSection
	// ModeStart inserts the fragment at the very start of the CONTAINER: byte 0
	// of the body, or just past the opening tag of the first <ac:layout-cell>
	// when the body has a layout wrapper. Unlike every other mode it needs no
	// heading — a headless page still has a well-defined "start".
	ModeStart
	// ModeReplacePreamble replaces the CONTAINER's preamble — everything from
	// its start up to (not including) its first locatable heading — with the
	// fragment. Unlike ModeStart, this needs that first heading to bound the
	// replaced range, so a page with no heading at all is an error.
	ModeReplacePreamble
)

// SpliceOptions configures a Splice call.
type SpliceOptions struct {
	Mode    Mode
	Heading string
	// IncludeSubsections applies to ModeReplaceSection only: replace the
	// section's nested subsections as well as its own content.
	IncludeSubsections bool
	// NewHeading renames the target heading's text. ModeReplaceSection only;
	// empty means the heading is left untouched. Plain text — it is escaped
	// before it reaches the storage body.
	NewHeading string
}

// HeadingRename records a heading rename that a splice performed.
type HeadingRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// BoundaryInfo describes where a splice landed and, for the replacing modes,
// what was removed. Fields are populated per mode; unused fields are left zero.
// JSON tags match the preview shape documented in the append design doc.
type BoundaryInfo struct {
	// InsertAnchor is populated by the inserting modes: ModeEnd,
	// ModeAfterHeading, ModeEndOfSection, and ModeStart.
	InsertAnchor string `json:"insert_anchor,omitempty"`
	// StartAnchor and EndAnchor describe the replaced range, for
	// ModeReplaceSection and ModeReplacePreamble.
	StartAnchor string `json:"start_anchor,omitempty"`
	EndAnchor   string `json:"end_anchor,omitempty"`
	// Container names the structural container the splice happened inside.
	Container string `json:"container"`
	// CrossesLayout is always false in successful splices (the rule forbids it)
	// but the field is present for explicit confirmation in dry-run output.
	CrossesLayout bool `json:"crosses_layout"`
	// ReplacedByteCount is the byte length of the removed region (replace only).
	ReplacedByteCount int `json:"replaced_byte_count,omitempty"`
	// ReplacedElementSummary is a histogram of the top-level elements removed,
	// e.g. ["<p> x 2", `macro "toc" x 1`] — macros named, not counted as bare
	// tags, so the caller sees which macro is about to be destroyed.
	ReplacedElementSummary []string `json:"replaced_element_summary,omitempty"`
	// ReplacedSections and PreservedSections name the target section's nested
	// subsection headings (ModeReplaceSection only). Exactly one is populated,
	// per the requested extent, so the caller can see which way the boundary
	// fell.
	ReplacedSections  []string `json:"replaced_sections,omitempty"`
	PreservedSections []string `json:"preserved_sections,omitempty"`
	// HeadingRenamed is populated only when the splice renamed the target
	// heading (ModeReplaceSection only).
	HeadingRenamed *HeadingRename `json:"heading_renamed,omitempty"`
	// AnchorReferences describes on-page anchor references to the OLD heading
	// text that a rename breaks. Advisory: they are reported, never rewritten.
	AnchorReferences []string `json:"anchor_references,omitempty"`
}

// SpliceResult is the output of a successful Splice call.
type SpliceResult struct {
	Merged   string
	Boundary BoundaryInfo
}

// Splice errors.
var (
	ErrHeadingNotFound          = errors.New("heading_not_found")
	ErrHeadingInUnsafeContainer = errors.New("heading_in_unsafe_container")
	ErrAmbiguousHeading         = errors.New("ambiguous_heading")
	ErrNotImplemented           = errors.New("not_implemented")
	// Rename errors. All three are raised before any body is assembled, so a
	// rejected rename never leaves a half-edited page.
	ErrRenameNoOp         = errors.New("rename_no_op")
	ErrRenameAmbiguous    = errors.New("rename_ambiguous")
	ErrHeadingHasChildren = errors.New("heading_has_children")
	// ErrNoHeadingOnPage is returned by ModeReplacePreamble when the container
	// (the first layout-cell, else the whole document) holds no locatable
	// heading, leaving the preamble with no boundary to stop at.
	ErrNoHeadingOnPage = errors.New("no_heading_on_page")
	// Boundary-imbalance errors, one per replacing mode: a plain wrapper (e.g.
	// <div>) opens inside the replaced range and closes outside it, so
	// splicing would delete the opening tag and orphan the closing one. Rare
	// in a Confluence-authored body (its own wrapper tags all move one of the
	// walker's depth counters) but reachable via format="storage"; the caller
	// should use action "update" instead.
	ErrPreambleBoundaryUnbalanced = errors.New("preamble_boundary_unbalanced")
	ErrSectionBoundaryUnbalanced  = errors.New("section_boundary_unbalanced")
)

// Splice inserts or replaces content in a Confluence storage-format body
// according to opts. The input body is not modified; the merged body is
// returned in the result.
func Splice(body, fragment string, opts SpliceOptions) (SpliceResult, error) {
	switch opts.Mode {
	case ModeEnd:
		return spliceEnd(body, fragment)
	case ModeAfterHeading:
		return spliceAfterHeading(body, fragment, opts.Heading)
	case ModeReplaceSection:
		return spliceReplaceSection(body, fragment, opts)
	case ModeEndOfSection:
		return spliceEndOfSection(body, fragment, opts.Heading)
	case ModeStart:
		return spliceStart(body, fragment)
	case ModeReplacePreamble:
		return spliceReplacePreamble(body, fragment)
	default:
		return SpliceResult{}, ErrNotImplemented
	}
}
