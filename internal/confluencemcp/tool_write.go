package confluencemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

var reMacroCommentCheck = regexp.MustCompile(`<!-- macro:m\d+ -->`)

// WriteItem holds the arguments for a single write operation.
type WriteItem struct {
	SpaceID       string `json:"space_id,omitempty"`
	PageID        string `json:"page_id,omitempty"`
	Title         string `json:"title,omitempty"`
	Body          string `json:"body,omitempty"`
	Format        string `json:"format,omitempty"` // "markdown" (default) or "storage"
	ParentID      string `json:"parent_id,omitempty"`
	Status        string `json:"status,omitempty"`
	VersionNumber int    `json:"version_number,omitempty"`
	CommentID     string `json:"comment_id,omitempty"`
	Label         string `json:"label,omitempty"`
	// Append-specific. Position is one of "end" (default), "after_heading",
	// "replace_section". Heading is required for the latter two.
	Position string `json:"position,omitempty"`
	Heading  string `json:"heading,omitempty"`
}

// WriteArgs holds the arguments for the confluence_write tool.
type WriteArgs struct {
	Action string      `json:"action"`
	Items  []WriteItem `json:"items"`
	DryRun bool        `json:"dry_run,omitempty"`
}

var validActions = map[string]bool{
	"create":       true,
	"update":       true,
	"append":       true,
	"delete":       true,
	"comment":      true,
	"edit_comment": true,
	"add_label":    true,
	"remove_label": true,
}

// handleWrite dispatches write operations for each item.
func (h *handlers) handleWrite(ctx context.Context, _ *mcp.CallToolRequest, args WriteArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Items) == 0 {
		return textResult("items must not be empty", true), nil, nil
	}
	if !validActions[args.Action] {
		return textResult(fmt.Sprintf("unknown action %q — use: create, update, append, delete, comment, edit_comment, add_label, remove_label", args.Action), true), nil, nil
	}

	h.logger().InfoContext(ctx, "write_action",
		"action", args.Action,
		"items", len(args.Items),
		"dry_run", args.DryRun,
	)

	var sb strings.Builder
	prefix := len(args.Items) > 1

	for i, item := range args.Items {
		msg, err := h.dispatchWriteItem(ctx, args.Action, item, args.DryRun)
		if err != nil {
			if prefix {
				fmt.Fprintf(&sb, "[%d] error: %v\n", i+1, err)
			} else {
				return textResult(fmt.Sprintf("error: %v", err), true), nil, nil
			}
			continue
		}
		if prefix {
			fmt.Fprintf(&sb, "[%d] %s\n", i+1, msg)
		} else {
			sb.WriteString(msg)
		}
	}

	return textResult(strings.TrimRight(sb.String(), "\n"), false), nil, nil
}

// dispatchWriteItem routes a single item to the appropriate handler method.
func (h *handlers) dispatchWriteItem(ctx context.Context, action string, item WriteItem, dryRun bool) (string, error) {
	switch action {
	case "create":
		return h.writeCreate(ctx, item, dryRun)
	case "update":
		return h.writeUpdate(ctx, item, dryRun)
	case "append":
		return h.writeAppend(ctx, item, dryRun)
	case "delete":
		return h.writeDelete(ctx, item, dryRun)
	case "comment":
		return h.writeComment(ctx, item, dryRun)
	case "edit_comment":
		return h.writeEditComment(ctx, item, dryRun)
	case "add_label":
		return h.writeAddLabel(ctx, item, dryRun)
	case "remove_label":
		return h.writeRemoveLabel(ctx, item, dryRun)
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
}

func (h *handlers) writeCreate(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	payload := map[string]any{
		"spaceId": item.SpaceID,
		"title":   item.Title,
	}
	if item.Body != "" {
		var storageBody string
		if item.Format == "storage" {
			storageBody = item.Body
		} else if reMacroCommentCheck.MatchString(item.Body) {
			registry := h.ensureMacroRegistry(ctx, item.PageID)
			storageBody = mdconv.ToStorageFormatWithMacros(item.Body, registry)
		} else {
			storageBody = mdconv.ToStorageFormat(item.Body)
		}
		payload["body"] = map[string]any{
			"storage": map[string]any{
				"value":          storageBody,
				"representation": "storage",
			},
		}
	}
	if item.ParentID != "" {
		payload["parentId"] = item.ParentID
	}
	if item.Status != "" {
		payload["status"] = item.Status
	}

	if dryRun {
		return dryRunJSON("create", payload), nil
	}

	page, err := h.client.CreatePage(ctx, payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created page %q (ID: %s)", page.Title, page.ID), nil
}

func (h *handlers) writeUpdate(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.VersionNumber <= 0 {
		return "", fmt.Errorf("version_number is required for update and must be > 0")
	}

	payload := map[string]any{
		"id":     item.PageID,
		"status": "current",
		"title":  item.Title,
		"version": map[string]any{
			"number": item.VersionNumber + 1,
		},
	}
	if item.Body != "" {
		var storageBody string
		if item.Format == "storage" {
			storageBody = item.Body
		} else if reMacroCommentCheck.MatchString(item.Body) {
			registry := h.ensureMacroRegistry(ctx, item.PageID)
			storageBody = mdconv.ToStorageFormatWithMacros(item.Body, registry)
		} else {
			storageBody = mdconv.ToStorageFormat(item.Body)
		}
		payload["body"] = map[string]any{
			"storage": map[string]any{
				"value":          storageBody,
				"representation": "storage",
			},
		}
	}
	if item.Status != "" {
		payload["status"] = item.Status
	}

	if dryRun {
		return dryRunJSON("update page "+item.PageID, payload), nil
	}

	page, err := h.client.UpdatePage(ctx, item.PageID, payload)
	if err != nil {
		return "", err
	}
	h.cache.evict(item.PageID)
	return fmt.Sprintf("Updated page %q (ID: %s)", page.Title, page.ID), nil
}

func (h *handlers) writeDelete(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.PageID == "" {
		return "", fmt.Errorf("page_id is required for delete")
	}

	if dryRun {
		return fmt.Sprintf("Would delete page %s", item.PageID), nil
	}

	if err := h.client.DeletePage(ctx, item.PageID); err != nil {
		return "", err
	}
	h.cache.evict(item.PageID)
	return fmt.Sprintf("Deleted page %s", item.PageID), nil
}

func (h *handlers) writeComment(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.PageID == "" {
		return "", fmt.Errorf("page_id is required for comment")
	}
	if item.Body == "" {
		return "", fmt.Errorf("body is required for comment")
	}

	storageBody := mdconv.ToStorageFormat(item.Body)

	if dryRun {
		return fmt.Sprintf("Would add comment to page %s:\n%s", item.PageID, storageBody), nil
	}

	comment, err := h.client.AddComment(ctx, item.PageID, storageBody)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added comment %s to page %s", comment.ID, item.PageID), nil
}

func (h *handlers) writeEditComment(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.CommentID == "" {
		return "", fmt.Errorf("comment_id is required for edit_comment")
	}
	if item.VersionNumber <= 0 {
		return "", fmt.Errorf("version_number is required for edit_comment and must be > 0")
	}

	storageBody := mdconv.ToStorageFormat(item.Body)
	nextVersion := item.VersionNumber + 1

	if dryRun {
		return fmt.Sprintf("Would update comment %s to version %d", item.CommentID, nextVersion), nil
	}

	comment, err := h.client.UpdateComment(ctx, item.CommentID, storageBody, nextVersion)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Updated comment %s", comment.ID), nil
}

func (h *handlers) writeAddLabel(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.PageID == "" {
		return "", fmt.Errorf("page_id is required for add_label")
	}
	if item.Label == "" {
		return "", fmt.Errorf("label is required for add_label")
	}

	if dryRun {
		return fmt.Sprintf("Would add label %q to page %s", item.Label, item.PageID), nil
	}

	label, err := h.client.AddPageLabel(ctx, item.PageID, item.Label)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added label %q (ID: %s) to page %s", label.Name, label.ID, item.PageID), nil
}

func (h *handlers) writeRemoveLabel(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.PageID == "" {
		return "", fmt.Errorf("page_id is required for remove_label")
	}
	if item.Label == "" {
		return "", fmt.Errorf("label is required for remove_label")
	}

	if dryRun {
		return fmt.Sprintf("Would remove label %q from page %s", item.Label, item.PageID), nil
	}

	if err := h.client.RemovePageLabel(ctx, item.PageID, item.Label); err != nil {
		return "", err
	}
	return fmt.Sprintf("Removed label %q from page %s", item.Label, item.PageID), nil
}

// dryRunJSON formats a "Would <action>" message with the payload as indented JSON.
func dryRunJSON(action string, payload map[string]any) string {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("Would %s (could not marshal payload: %v)", action, err)
	}
	return fmt.Sprintf("Would %s:\n```json\n%s\n```", action, string(data))
}
