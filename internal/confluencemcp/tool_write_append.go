package confluencemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

// writeAppend handles the "append" action: insert or replace a fragment in an
// existing page without requiring the agent to send the full body.
func (h *handlers) writeAppend(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.PageID == "" {
		return "", fmt.Errorf("page_id is required for append")
	}
	if item.Body == "" {
		return "", fmt.Errorf("body is required for append")
	}
	mode, err := parseMode(item.Position)
	if err != nil {
		return "", err
	}
	// Every position declares, via positionFieldRules, whether it requires a
	// heading and whether it permits include_subsections / new_heading. A field
	// supplied to a position that does not consume it is a hard error in both
	// directions, rather than being silently dropped.
	rules, ok := positionFieldRules[mode]
	if !ok {
		return "", fmt.Errorf("internal error: no field rules registered for position %q", positionLabel(item.Position))
	}
	label := positionLabel(item.Position)
	switch {
	case rules.headingRequired && item.Heading == "":
		return "", fmt.Errorf("heading is required for position %q", label)
	case !rules.headingRequired && item.Heading != "":
		return "", fmt.Errorf("heading is not valid for position %q", label)
	}
	if item.IncludeSubsections && !rules.includeSubsectionsAllowed {
		return "", fmt.Errorf(`include_subsections is only valid for position "replace_section"`)
	}
	if item.NewHeading != "" && !rules.newHeadingAllowed {
		return "", fmt.Errorf(`new_heading is only valid for position "replace_section"`)
	}

	// Convert fragment to storage if the agent sent markdown. The fragment is
	// the same across retries — only the base body changes on a stale version.
	var fragmentStorage string
	switch item.Format {
	case "storage":
		fragmentStorage = item.Body
	default:
		fragmentStorage = mdconv.ToStorageFormat(item.Body)
	}

	page, res, err := h.fetchAndSplice(ctx, item, mode, fragmentStorage)
	if err != nil {
		return "", err
	}

	if dryRun {
		inputFormat := item.Format
		if inputFormat == "" {
			inputFormat = "markdown"
		}
		preview := buildPreview(
			item, mode,
			page.Body.Storage.Value, res.Merged, fragmentStorage, inputFormat,
			res.Boundary,
		)
		data, jerr := json.MarshalIndent(preview, "", "  ")
		if jerr != nil {
			return "", fmt.Errorf("marshal preview: %w", jerr)
		}
		return fmt.Sprintf("Would append to page %s:\n```json\n%s\n```", item.PageID, string(data)), nil
	}

	// If the caller supplied a specific version, enforce it strictly and do
	// NOT retry — they are asserting the exact version they want to write on.
	if item.VersionNumber > 0 && item.VersionNumber != page.Version.Number {
		return "", fmt.Errorf("version_conflict: supplied version %d does not match current %d", item.VersionNumber, page.Version.Number)
	}

	updated, err := h.client.UpdatePage(ctx, item.PageID, appendPayload(item.PageID, page, res.Merged))
	if err == nil {
		h.cache.evict(item.PageID)
		return appendSuccessMsg(updated.Title, updated.ID, page.Body.Storage.Value, res.Merged, fragmentStorage, res.Boundary), nil
	}

	// Retry once on 409 when the caller did not pin a version: Confluence's
	// read path is eventually consistent, so the GET above can return a stale
	// version right after a prior write.
	if item.VersionNumber == 0 && is409(err) {
		h.logger().WarnContext(ctx, "append_retry_on_409", "page_id", item.PageID)
		page2, res2, ferr := h.fetchAndSplice(ctx, item, mode, fragmentStorage)
		if ferr != nil {
			return "", ferr
		}
		updated2, uerr := h.client.UpdatePage(ctx, item.PageID, appendPayload(item.PageID, page2, res2.Merged))
		if uerr != nil {
			return "", uerr
		}
		h.cache.evict(item.PageID)
		return appendSuccessMsg(updated2.Title, updated2.ID, page2.Body.Storage.Value, res2.Merged, fragmentStorage, res2.Boundary), nil
	}
	return "", err
}

// appendSuccessMsg formats the append success line, including the fragment
// size and base→merged body sizes so the caller can see what was sent versus
// what the server assembled. A replace_preamble or replace_section write also
// names what it destroyed, and a replace names the nested subsections it
// removed or kept, rather than leaving the byte delta as the only clue.
func appendSuccessMsg(title, id, baseBody, mergedBody, fragment string, b BoundaryInfo) string {
	base := len(baseBody)
	merged := len(mergedBody)
	// Only the element histogram is folded into the opening sentence — nested
	// sections and broken anchors get their own dedicated sentences below,
	// which name them rather than counting them.
	var replaced string
	if len(b.ReplacedElementSummary) > 0 {
		replaced = " (replaces " + strings.Join(b.ReplacedElementSummary, ", ") + ")"
	}
	msg := fmt.Sprintf(
		"Appended to page %q (ID: %s). Fragment sent: %d bytes; page body: %d → %d (Δ%+d)%s.",
		title, id, len(fragment), base, merged, merged-base, replaced,
	)
	// A bare-text replaced range produces no walker events, so
	// ReplacedElementSummary is empty even though bytes were destroyed. Name
	// the byte count instead of going silent.
	if len(b.ReplacedElementSummary) == 0 && b.ReplacedByteCount > 0 {
		msg += fmt.Sprintf(" Replaced %d bytes with no locatable elements (e.g. bare text).", b.ReplacedByteCount)
	}
	if len(b.ReplacedSections) > 0 {
		msg += fmt.Sprintf(" Replaced %s.", nestedSectionPhrase(b.ReplacedSections))
	}
	if len(b.PreservedSections) > 0 {
		msg += fmt.Sprintf(" Preserved %s.", nestedSectionPhrase(b.PreservedSections))
	}
	if b.HeadingRenamed != nil {
		msg += fmt.Sprintf(" Renamed heading %q → %q.", b.HeadingRenamed.From, b.HeadingRenamed.To)
		if len(b.AnchorReferences) > 0 {
			msg += " " + anchorWarningPhrase(b.AnchorReferences, b.HeadingRenamed.From)
		}
	}
	return msg
}

// anchorWarningPhrase renders the on-page anchor references a rename breaks.
// Named rather than counted-only, for the same reason nestedSectionPhrase names
// subsections: a count alone leaves the caller unable to act on it.
func anchorWarningPhrase(refs []string, oldHeading string) string {
	noun, verb := "anchor references", "point"
	if len(refs) == 1 {
		noun, verb = "anchor reference", "points"
	}
	return fmt.Sprintf(
		"Warning: %d on-page %s to %q now %s at a heading that no longer exists: %s.",
		len(refs), noun, oldHeading, verb, strings.Join(refs, ", "),
	)
}

// fetchAndSplice fetches the page's current storage body and applies the
// splice. Returned separately from the payload build so retries can re-run it
// against the freshly-read body.
func (h *handlers) fetchAndSplice(ctx context.Context, item WriteItem, mode Mode, fragmentStorage string) (*confluence.Page, SpliceResult, error) {
	page, err := h.client.GetPage(ctx, item.PageID)
	if err != nil {
		return nil, SpliceResult{}, fmt.Errorf("fetch page: %w", err)
	}
	res, err := Splice(page.Body.Storage.Value, fragmentStorage, SpliceOptions{
		Mode:               mode,
		Heading:            item.Heading,
		IncludeSubsections: item.IncludeSubsections,
		NewHeading:         item.NewHeading,
	})
	if err != nil {
		return nil, SpliceResult{}, err
	}
	return page, res, nil
}

// appendPayload builds the UpdatePage payload for an append, bumping the
// page's version by one.
func appendPayload(pageID string, page *confluence.Page, merged string) map[string]any {
	return map[string]any{
		"id":     pageID,
		"status": "current",
		"title":  page.Title,
		"version": map[string]any{
			"number": page.Version.Number + 1,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          merged,
				"representation": "storage",
			},
		},
	}
}

// is409 reports whether err is a Confluence APIError with a 409 Conflict
// status. This covers the StaleStateException that surfaces when the GET
// replica returned a version that the write path rejects.
func is409(err error) bool {
	var apiErr *confluence.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// positionStrings lists every position string parseMode accepts, other than
// the empty string (a shorthand for "end", not a label of its own). Single
// source of truth for the "unknown position" error message below and the
// test coverage tables in tool_write_append_test.go.
var positionStrings = []string{
	"end", "after_heading", "end_of_section", "replace_section", "start", "replace_preamble",
}

// parseMode converts the user-facing position string to a Mode.
func parseMode(position string) (Mode, error) {
	switch position {
	case "", "end":
		return ModeEnd, nil
	case "after_heading":
		return ModeAfterHeading, nil
	case "end_of_section":
		return ModeEndOfSection, nil
	case "replace_section":
		return ModeReplaceSection, nil
	case "start":
		return ModeStart, nil
	case "replace_preamble":
		return ModeReplacePreamble, nil
	default:
		return 0, fmt.Errorf(
			"unknown position %q — use: %s",
			position, strings.Join(positionStrings, ", "),
		)
	}
}

// positionFields declares which optional WriteItem fields a position
// consumes: whether a heading is required (every other position REJECTS one
// outright), and whether include_subsections / new_heading are permitted at
// all. positionFieldRules must carry one entry per Mode — a mode missing
// from the table fails the writeAppend lookup loudly rather than silently
// applying a default.
type positionFields struct {
	headingRequired           bool
	includeSubsectionsAllowed bool
	newHeadingAllowed         bool
}

var positionFieldRules = map[Mode]positionFields{
	ModeEnd:             {},
	ModeAfterHeading:    {headingRequired: true},
	ModeEndOfSection:    {headingRequired: true},
	ModeReplaceSection:  {headingRequired: true, includeSubsectionsAllowed: true, newHeadingAllowed: true},
	ModeStart:           {},
	ModeReplacePreamble: {},
}

// positionLabel returns position as written by the caller, defaulting the
// empty string to "end" (its meaning) so field-validation error messages
// always name a position rather than an empty pair of quotes.
func positionLabel(position string) string {
	if position == "" {
		return "end"
	}
	return position
}

