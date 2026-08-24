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
// passed whole rather than field by field: the splice options it carries
// (heading, new heading, include_subsections) all have to reach the context and
// summary helpers, and threading them positionally invites a swap between the
// two adjacent heading strings.
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
		var parts []string
		if len(b.ReplacedElementSummary) > 0 {
			parts = append(parts, "replaces "+strings.Join(b.ReplacedElementSummary, ", "))
		}
		// Nested subsections are named, never folded into the tag histogram —
		// losing (or keeping) a whole subsection is the one outcome the caller
		// must not have to infer from a byte delta.
		if len(b.ReplacedSections) > 0 {
			parts = append(parts, "replaces "+nestedSectionPhrase(b.ReplacedSections))
		}
		if len(b.PreservedSections) > 0 {
			parts = append(parts, "preserves "+nestedSectionPhrase(b.PreservedSections))
		}
		if len(b.AnchorReferences) > 0 {
			parts = append(parts, fmt.Sprintf("breaks %d on-page anchor reference(s)", len(b.AnchorReferences)))
		}
		if len(parts) > 0 {
			summary += " (" + strings.Join(parts, "; ") + ")"
		}
		return summary + "."
	case ModeEndOfSection:
		return fmt.Sprintf("Append to end of section %q.", heading)
	default:
		return ""
	}
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
// splice point. Kept small (≤ maxContextChars each side) to bound the preview
// size. ModeReplaceSection and ModeEndOfSection both stop at a section
// boundary via findSectionExtent, but keep separate cases: their "before"
// differs — replace excludes the section body (it is being replaced),
// end-of-section includes it (it is being kept). item.IncludeSubsections
// applies to replace only, and must match the splice's own extent so the
// "after" snippet starts where the replacement really ends.
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
		// of body if no layout. Simple: show the tail of base.
		return truncBefore(base), ""
	case ModeAfterHeading:
		match, err := locateHeading(base, heading)
		if err != nil {
			return "", ""
		}
		return truncBefore(base[:match.headingEndOff]), truncAfter(base[match.headingEndOff:])
	case ModeReplaceSection:
		// "Before" ends at the heading itself — everything after it is being
		// replaced, so none of the section's existing body appears. With a
		// rename the heading is shown as it WILL be, not as it was: the preview
		// has to show the page the write produces, and the heading is now part
		// of the change. If the section's stop point can't be found, fall back
		// to the heading-only view: there is nothing "after" to show for a
		// replace regardless.
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
		// after_heading. If the stop point can't be found, fall back to no
		// context at all rather than ModeReplaceSection's heading-only view:
		// showing just the heading here would look like a top-of-section
		// insert, which is precisely the confusion this mode exists to avoid.
		match, err := locateHeading(base, heading)
		if err != nil {
			return "", ""
		}
		ext, err := findSectionExtent(base, match, true)
		if err != nil {
			return "", ""
		}
		return truncBefore(base[:ext.stop]), truncAfter(base[ext.stop:])
	default:
		return "", ""
	}
}
