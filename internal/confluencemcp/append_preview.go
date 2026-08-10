package confluencemcp

import (
	"fmt"
	"strings"
)

// AppendPreview is the dry-run preview of an append action. Field layout
// matches the JSON shape documented in the append design doc.
type AppendPreview struct {
	PageID        string          `json:"page_id"`
	ActionSummary string          `json:"action_summary"`
	Position      string          `json:"position"`
	Heading       string          `json:"heading,omitempty"`
	Boundary      BoundaryInfo    `json:"boundary"`
	Fragment      PreviewFragment `json:"fragment"`
	Context       PreviewContext  `json:"context"`
	Sizes         PreviewSizes    `json:"sizes"`
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

// buildPreview builds an AppendPreview for a dry-run result.
func buildPreview(
	pageID string,
	base, merged, fragmentStorage string,
	mode Mode,
	heading string,
	boundary BoundaryInfo,
	inputBody, inputFormat string,
) AppendPreview {
	pos := modeString(mode)
	summary := summariseAction(mode, heading, boundary)
	before, after := contextAround(base, mode, heading)

	return AppendPreview{
		PageID:        pageID,
		ActionSummary: summary,
		Position:      pos,
		Heading:       heading,
		Boundary: boundary,
		Fragment: PreviewFragment{
			InputFormat:      inputFormat,
			InputBody:        inputBody,
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

func summariseAction(mode Mode, heading string, b BoundaryInfo) string {
	switch mode {
	case ModeEnd:
		return "Append to end of page."
	case ModeAfterHeading:
		return fmt.Sprintf("Insert after heading %q.", heading)
	case ModeReplaceSection:
		summary := fmt.Sprintf("Replace content under heading %q", heading)
		if len(b.ReplacedElementSummary) > 0 {
			summary += " (replaces " + strings.Join(b.ReplacedElementSummary, ", ") + ")"
		}
		return summary + "."
	case ModeEndOfSection:
		return fmt.Sprintf("Append to end of section %q.", heading)
	default:
		return ""
	}
}

// contextAround returns a snippet of base-body context before and after the
// splice point. Kept small (≤ maxContextChars each side) to bound the preview
// size. ModeReplaceSection and ModeEndOfSection both stop at the section's end
// via findSectionEnd, but keep separate cases: their "before" differs —
// replace excludes the section body (it is being replaced), end-of-section
// includes it (it is being kept).
func contextAround(base string, mode Mode, heading string) (string, string) {
	const maxContextChars = 400

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
		// replaced, so none of the section's existing body appears. If the
		// section's stop point can't be found, fall back to the heading-only
		// view: there is nothing "after" to show for a replace regardless.
		match, err := locateHeading(base, heading)
		if err != nil {
			return "", ""
		}
		stopOff, _, err := findSectionEnd(base, match)
		if err != nil {
			return truncBefore(base[:match.headingEndOff]), ""
		}
		return truncBefore(base[:match.headingEndOff]), truncAfter(base[stopOff:])
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
		stopOff, _, err := findSectionEnd(base, match)
		if err != nil {
			return "", ""
		}
		return truncBefore(base[:stopOff]), truncAfter(base[stopOff:])
	default:
		return "", ""
	}
}
