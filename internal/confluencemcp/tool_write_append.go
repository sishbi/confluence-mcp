package confluencemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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
	if (mode == ModeAfterHeading || mode == ModeReplaceSection) && item.Heading == "" {
		return "", fmt.Errorf("heading is required for position %q", item.Position)
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

	// Fetch, splice, and for dry-run return the preview.
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
			item.PageID,
			page.Body.Storage.Value, res.Merged, fragmentStorage,
			mode, item.Heading, res.Boundary,
			item.Body, inputFormat,
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
		return appendSuccessMsg(updated.Title, updated.ID, page.Body.Storage.Value, res.Merged, fragmentStorage), nil
	}

	// Retry once on 409 when the caller did not pin a version. Confluence's
	// read path is eventually consistent — the GET above can return a stale
	// version right after a prior write. Re-fetch, re-splice against the fresh
	// body, and PUT again with the new version.
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
		return appendSuccessMsg(updated2.Title, updated2.ID, page2.Body.Storage.Value, res2.Merged, fragmentStorage), nil
	}
	return "", err
}

// appendSuccessMsg formats the append success line, including the fragment
// size and base→merged body sizes so the caller can see what was sent versus
// what the server assembled.
func appendSuccessMsg(title, id, baseBody, mergedBody, fragment string) string {
	base := len(baseBody)
	merged := len(mergedBody)
	return fmt.Sprintf(
		"Appended to page %q (ID: %s). Fragment sent: %d bytes; page body: %d → %d (Δ%+d).",
		title, id, len(fragment), base, merged, merged-base,
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
		Mode:    mode,
		Heading: item.Heading,
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

