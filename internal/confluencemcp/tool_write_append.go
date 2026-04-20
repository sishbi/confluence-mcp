package confluencemcp

import (
	"context"
	"encoding/json"
	"fmt"

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
	if (mode == ModeAfterHeading || mode == ModeReplaceSection) && item.Heading == "" {
		return "", fmt.Errorf("heading is required for position %q", item.Position)
	}

	// Resolve base body. For append we need raw storage, not the converted
	// markdown the cache holds. Always fetch fresh.
	page, err := h.client.GetPage(ctx, item.PageID)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}
	base := page.Body.Storage.Value

	// Convert fragment to storage if the agent sent markdown.
	var fragmentStorage string
	switch item.Format {
	case "storage":
		fragmentStorage = item.Body
	default:
		fragmentStorage = mdconv.ToStorageFormat(item.Body)
	}

	// Splice.
	res, err := Splice(base, fragmentStorage, SpliceOptions{
		Mode:    mode,
		Heading: item.Heading,
	})
	if err != nil {
		return "", err
	}

	// Dry-run: build and return preview JSON.
	if dryRun {
		inputFormat := item.Format
		if inputFormat == "" {
			inputFormat = "markdown"
		}
		preview := buildPreview(
			item.PageID,
			base, res.Merged, fragmentStorage,
			mode, item.Heading, res.Boundary,
			item.Body, inputFormat,
		)
		data, jerr := json.MarshalIndent(preview, "", "  ")
		if jerr != nil {
			return "", fmt.Errorf("marshal preview: %w", jerr)
		}
		return fmt.Sprintf("Would append to page %s:\n```json\n%s\n```", item.PageID, string(data)), nil
	}

	// Real update. Use the page's current version.
	payload := map[string]any{
		"id":     item.PageID,
		"status": "current",
		"title":  page.Title,
		"version": map[string]any{
			"number": page.Version.Number + 1,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          res.Merged,
				"representation": "storage",
			},
		},
	}
	if item.VersionNumber > 0 && item.VersionNumber != page.Version.Number {
		return "", fmt.Errorf("version_conflict: supplied version %d does not match current %d", item.VersionNumber, page.Version.Number)
	}

	updated, err := h.client.UpdatePage(ctx, item.PageID, payload)
	if err != nil {
		return "", err
	}
	h.cache.evict(item.PageID)
	return fmt.Sprintf("Appended to page %q (ID: %s)", updated.Title, updated.ID), nil
}

// parseMode converts the user-facing position string to a Mode.
func parseMode(position string) (Mode, error) {
	switch position {
	case "", "end":
		return ModeEnd, nil
	case "after_heading":
		return ModeAfterHeading, nil
	case "replace_section":
		return ModeReplaceSection, nil
	default:
		return 0, fmt.Errorf("unknown position %q — use: end, after_heading, replace_section", position)
	}
}

