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
	// the heading itself) up to the next same-or-higher-level heading or the end
	// of the containing layout-cell.
	ModeReplaceSection
	// ModeEndOfSection inserts the fragment at the END of a named section:
	// after the section's existing content, before the next
	// same-or-higher-level heading or the close of the containing layout-cell.
	ModeEndOfSection
)

// SpliceOptions configures a Splice call.
type SpliceOptions struct {
	Mode    Mode
	Heading string
}

// BoundaryInfo describes where a splice landed and, for replace-section, what
// was removed. Fields are populated per mode; unused fields are left zero.
// JSON tags match the preview shape documented in the append design doc.
type BoundaryInfo struct {
	// InsertAnchor is populated for ModeEnd and ModeAfterHeading.
	InsertAnchor string `json:"insert_anchor,omitempty"`
	// StartAnchor and EndAnchor describe the replaced range for ModeReplaceSection.
	StartAnchor string `json:"start_anchor,omitempty"`
	EndAnchor   string `json:"end_anchor,omitempty"`
	// Container names the structural container the splice happened inside.
	Container string `json:"container"`
	// CrossesLayout is always false in successful splices (the rule forbids it)
	// but the field is present for explicit confirmation in dry-run output.
	CrossesLayout bool `json:"crosses_layout"`
	// ReplacedByteCount is the byte length of the removed region (replace only).
	ReplacedByteCount int `json:"replaced_byte_count,omitempty"`
	// ReplacedElementSummary is a tag-count histogram of top-level replaced
	// elements, e.g. ["<p> x 2", "<ul> x 1"].
	ReplacedElementSummary []string `json:"replaced_element_summary,omitempty"`
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
		return spliceReplaceSection(body, fragment, opts.Heading)
	case ModeEndOfSection:
		return spliceEndOfSection(body, fragment, opts.Heading)
	default:
		return SpliceResult{}, ErrNotImplemented
	}
}
