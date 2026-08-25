package confluencemcp

import (
	"fmt"
	"strings"
)

// AppendPreview is the dry-run preview of an append action. Field layout
// matches the JSON shape documented in the append design doc.
type AppendPreview struct {
	PageID        string `json:"page_id"`
	ActionSummary string `json:"action_summary"`
	Position      string `json:"position"`
	Heading       string `json:"heading,omitempty"`
	// NewHeading echoes the requested rename, if any.
	NewHeading string          `json:"new_heading,omitempty"`
	Boundary   BoundaryInfo    `json:"boundary"`
	Fragment   PreviewFragment `json:"fragment"`
	Context    PreviewContext  `json:"context"`
	Sizes      PreviewSizes    `json:"sizes"`
}

// PreviewFragment echoes what the agent sent and what mdconv produced.
type PreviewFragment struct {
	InputFormat      string `json:"input_format"`
	InputBody        string `json:"input_body"`
	StorageOutput    string `json:"storage_output"`
	StorageByteCount int    `json:"storage_byte_count"`
}

// PreviewContext is a snippet of context before and after the splice point.
type PreviewContext struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// PreviewSizes summarises base vs merged body size.
type PreviewSizes struct {
	BaseBodyBytes   int `json:"base_body_bytes"`
	MergedBodyBytes int `json:"merged_body_bytes"`
	DeltaBytes      int `json:"delta_bytes"`
}

// buildPreview builds an AppendPreview for a dry-run result. The write item is
// passed whole rather than field by field, since threading heading and
// new_heading positionally invites a swap between the two adjacent strings.
func buildPreview(
	item WriteItem,
	mode Mode,
	base, merged, fragmentStorage, inputFormat string,
	boundary BoundaryInfo,
) AppendPreview {
	pos := modeString(mode)
	summary := summariseAction(mode, item.Heading, item.NewHeading, boundary)
	before, after := contextAround(base, mode, item)

	return AppendPreview{
		PageID:        item.PageID,
		ActionSummary: summary,
		Position:      pos,
		Heading:       item.Heading,
		NewHeading:    item.NewHeading,
		Boundary:      boundary,
		Fragment: PreviewFragment{
			InputFormat:      inputFormat,
			InputBody:        item.Body,
			StorageOutput:    fragmentStorage,
			StorageByteCount: len(fragmentStorage),
		},
		Context: PreviewContext{
			Before: before,
			After:  after,
		},
		Sizes: PreviewSizes{
			BaseBodyBytes:   len(base),
			MergedBodyBytes: len(merged),
			DeltaBytes:      len(merged) - len(base),
		},
	}
}

func modeString(m Mode) string {
	switch m {
	case ModeEnd:
		return "end"
	case ModeAfterHeading:
		return "after_heading"
	case ModeReplaceSection:
		return "replace_section"
	case ModeEndOfSection:
		return "end_of_section"
	case ModeStart:
		return "start"
	case ModeReplacePreamble:
		return "replace_preamble"
	default:
		return "unknown"
	}
}

func summariseAction(mode Mode, heading, newHeading string, b BoundaryInfo) string {
	switch mode {
	case ModeEnd:
		return "Append to end of page."
	case ModeAfterHeading:
		return fmt.Sprintf("Insert after heading %q.", heading)
	case ModeReplaceSection:
		summary := fmt.Sprintf("Replace content under heading %q", heading)
		if newHeading != "" {
			summary += fmt.Sprintf(" and rename it to %q", newHeading)
		}
		return summary + replacedElementsClause(b) + "."
	case ModeEndOfSection:
		return fmt.Sprintf("Append to end of section %q.", heading)
	case ModeStart:
		return "Insert at start of page."
	case ModeReplacePreamble:
		return "Replace page preamble" + replacedElementsClause(b) + "."
	default:
		return ""
	}
}

// replacedElementsClause renders the "(replaces …; preserves …)" parenthetical
// shared by ModeReplaceSection and ModeReplacePreamble, both of which destroy
// a region of the page and must tell the caller what is in it before the PUT.
// Returns "" when there is nothing to report.
func replacedElementsClause(b BoundaryInfo) string {
	var parts []string
	switch {
	case len(b.ReplacedElementSummary) > 0:
		parts = append(parts, "replaces "+strings.Join(b.ReplacedElementSummary, ", "))
	case b.ReplacedByteCount > 0:
		// A bare-text replaced range produces no walker events, so
		// ReplacedElementSummary is empty even though bytes are about to be
		// destroyed. Name the byte count instead of going silent.
		parts = append(parts, fmt.Sprintf("replaces %d bytes with no locatable elements", b.ReplacedByteCount))
	}
	// Nested subsections are named, never folded into the tag histogram —
	// the caller must not have to infer them from a byte delta.
	if len(b.ReplacedSections) > 0 {
		parts = append(parts, "replaces "+nestedSectionPhrase(b.ReplacedSections))
	}
	if len(b.PreservedSections) > 0 {
		parts = append(parts, "preserves "+nestedSectionPhrase(b.PreservedSections))
	}
	if len(b.AnchorReferences) > 0 {
		parts = append(parts, fmt.Sprintf("breaks %d on-page anchor reference(s)", len(b.AnchorReferences)))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

// nestedSectionPhrase renders a list of nested subsection headings as
// `2 nested sections: "A", "B"`.
func nestedSectionPhrase(names []string) string {
	noun := "nested sections"
	if len(names) == 1 {
		noun = "nested section"
	}
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, fmt.Sprintf("%q", n))
	}
	return fmt.Sprintf("%d %s: %s", len(names), noun, strings.Join(quoted, ", "))
}

// contextAround returns a snippet of base-body context before and after the
// splice point, kept small (≤ maxContextChars each side) to bound the
// preview size. ModeReplaceSection and ModeEndOfSection both stop at a
// section boundary via findSectionExtent, but their "before" differs:
// replace excludes the section body (it is being replaced), end-of-section
// includes it (it is being kept).
func contextAround(base string, mode Mode, item WriteItem) (string, string) {
	const maxContextChars = 400
	heading := item.Heading

	truncBefore := func(s string) string {
		if len(s) > maxContextChars {
			return "…" + s[len(s)-maxContextChars:]
		}
		return s
	}
	truncAfter := func(s string) string {
		if len(s) > maxContextChars {
			return s[:maxContextChars] + "…"
		}
		return s
	}

	switch mode {
	case ModeEnd:
		// Splice goes before the innermost trailing </ac:layout-cell>, or end
		// of body if no layout.
		return truncBefore(base), ""
	case ModeAfterHeading:
		match, err := locateHeading(base, heading)
		if err != nil {
			return "", ""
		}
		return truncBefore(base[:match.headingEndOff]), truncAfter(base[match.headingEndOff:])
	case ModeReplaceSection:
		// "Before" ends at the heading itself — everything after it is being
		// replaced. With a rename the heading is shown as it WILL be, since
		// the preview has to show the page the write produces. Falls back to
		// the heading-only view if the section's stop point can't be found.
		match, err := locateHeading(base, heading)
		if err != nil {
			return "", ""
		}
		prefix := renamedPrefix(base, match, item.NewHeading)
		ext, err := findSectionExtent(base, match, item.IncludeSubsections)
		if err != nil {
			return truncBefore(prefix), ""
		}
		return truncBefore(prefix), truncAfter(base[ext.stop:])
	case ModeEndOfSection:
		// "Before" runs all the way to the section's stop point, including
		// the section's existing body — that's what distinguishes this from
		// after_heading. Falls back to no context (not ReplaceSection's
		// heading-only view) so it can't be mistaken for a top-of-section insert.
		match, err := locateHeading(base, heading)
		if err != nil {
			return "", ""
		}
		ext, err := findSectionExtent(base, match, true)
		if err != nil {
			return "", ""
		}
		return truncBefore(base[:ext.stop]), truncAfter(base[ext.stop:])
	case ModeStart:
		// "After" is the head of the container: byte 0 of the body, or just
		// past the opening <ac:layout-cell> tag.
		container, err := locateContainer(base)
		if err != nil {
			return "", ""
		}
		return "", truncAfter(base[container.start:])
	case ModeReplacePreamble:
		// "Before" includes the container's own opening tags, since those are
		// not part of the replaced range. "After" begins at the first
		// heading, which bounds the preamble and is never touched.
		ext, err := locatePreambleExtent(base)
		if err != nil {
			return "", ""
		}
		return truncBefore(base[:ext.containerStart]), truncAfter(base[ext.firstHeadingStart:])
	default:
		return "", ""
	}
}
