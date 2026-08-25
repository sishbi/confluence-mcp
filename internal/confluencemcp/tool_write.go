package confluencemcp

import (
	"context"
	"encoding/json"
	"errors"
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
	// "end_of_section", "replace_section", "start", "replace_preamble".
	// Heading is required for after_heading, end_of_section, and
	// replace_section, and REJECTED for every other position.
	Position string `json:"position,omitempty"`
	Heading  string `json:"heading,omitempty"`
	// IncludeSubsections applies to position "replace_section" only. By default
	// a replace stops at the section's first subsection, leaving subsections
	// intact; set it to replace the whole section, subsections included.
	IncludeSubsections bool `json:"include_subsections,omitempty"`
	// NewHeading renames the target heading while replacing its content.
	// Position "replace_section" only. Plain text — it is escaped before it
	// reaches the page, and the heading's level is unchanged.
	NewHeading string `json:"new_heading,omitempty"`
	// ParentCommentID identifies the comment being replied to. Required for
	// reply_comment; sent as parentCommentId on the wire and never alongside
	// page_id — Confluence's reply create models reject the two together (D4).
	ParentCommentID string `json:"parent_comment_id,omitempty"`
	// CommentType selects which comment API a reply targets, "footer" or
	// "inline". Required for reply_comment — the two are distinct endpoints
	// and the type cannot be inferred from the parent ID alone.
	CommentType string `json:"comment_type,omitempty"`
}

// WriteArgs holds the arguments for the confluence_write tool.
type WriteArgs struct {
	Action string      `json:"action"`
	Items  []WriteItem `json:"items"`
	DryRun bool        `json:"dry_run,omitempty"`
}

// writeActionNames lists every write action, in the order presented to
// callers: the tool description prose (server.go) and the unknown-action
// error message below. validActions is derived from this slice so the two
// cannot drift; TestValidActionsMatchesPermittedWriteFields
// (tool_write_test.go) pins validActions against permittedWriteFields.
var writeActionNames = []string{
	"create", "update", "append", "delete", "comment", "edit_comment", "reply_comment", "add_label", "remove_label",
}

var validActions = func() map[string]bool {
	m := make(map[string]bool, len(writeActionNames))
	for _, name := range writeActionNames {
		m[name] = true
	}
	return m
}()

// handleWrite dispatches write operations for each item.
func (h *handlers) handleWrite(ctx context.Context, _ *mcp.CallToolRequest, args WriteArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Items) == 0 {
		return textResult("items must not be empty", true), nil, nil
	}
	if !validActions[args.Action] {
		return textResult(fmt.Sprintf("unknown action %q — use: %s", args.Action, strings.Join(writeActionNames, ", ")), true), nil, nil
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
	if err := validateWriteItemFields(action, item); err != nil {
		return "", err
	}

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
	case "reply_comment":
		return h.writeReplyComment(ctx, item, dryRun)
	case "add_label":
		return h.writeAddLabel(ctx, item, dryRun)
	case "remove_label":
		return h.writeRemoveLabel(ctx, item, dryRun)
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
}

// writeFieldSpec names a WriteItem field by its JSON key and reports whether
// a given item supplies it.
type writeFieldSpec struct {
	name string
	set  func(WriteItem) bool
}

// writeFields enumerates every WriteItem field once, in struct-declaration
// order, so permittedWriteFields below has a single place to name each field.
var writeFields = []writeFieldSpec{
	{"space_id", func(i WriteItem) bool { return i.SpaceID != "" }},
	{"page_id", func(i WriteItem) bool { return i.PageID != "" }},
	{"title", func(i WriteItem) bool { return i.Title != "" }},
	{"body", func(i WriteItem) bool { return i.Body != "" }},
	{"format", func(i WriteItem) bool { return i.Format != "" }},
	{"parent_id", func(i WriteItem) bool { return i.ParentID != "" }},
	{"status", func(i WriteItem) bool { return i.Status != "" }},
	{"version_number", func(i WriteItem) bool { return i.VersionNumber != 0 }},
	{"comment_id", func(i WriteItem) bool { return i.CommentID != "" }},
	{"label", func(i WriteItem) bool { return i.Label != "" }},
	{"position", func(i WriteItem) bool { return i.Position != "" }},
	{"heading", func(i WriteItem) bool { return i.Heading != "" }},
	{"include_subsections", func(i WriteItem) bool { return i.IncludeSubsections }},
	{"new_heading", func(i WriteItem) bool { return i.NewHeading != "" }},
	{"parent_comment_id", func(i WriteItem) bool { return i.ParentCommentID != "" }},
	{"comment_type", func(i WriteItem) bool { return i.CommentType != "" }},
}

// permittedWriteFields maps each action to the WriteItem fields its handler
// method actually reads (D6). One struct backs every action and the tool
// schema is reflected from it, so a field supplied to an action that does
// not consume it would otherwise be silently accepted and dropped.
var permittedWriteFields = map[string]map[string]bool{
	"create":        writeFieldSet("space_id", "title", "body", "format", "parent_id", "status", "page_id"),
	"update":        writeFieldSet("page_id", "title", "body", "format", "version_number", "status"),
	"append":        writeFieldSet("page_id", "body", "format", "position", "heading", "version_number", "include_subsections", "new_heading"),
	"delete":        writeFieldSet("page_id"),
	"comment":       writeFieldSet("page_id", "body"),
	"edit_comment":  writeFieldSet("comment_id", "body", "version_number"),
	"reply_comment": writeFieldSet("parent_comment_id", "comment_type", "body", "format"),
	"add_label":     writeFieldSet("page_id", "label"),
	"remove_label":  writeFieldSet("page_id", "label"),
}

func writeFieldSet(names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// writeFieldHints names, for a rejected field, the field an agent probably
// meant instead — e.g. parent_id (create's field) vs parent_comment_id
// (reply's), a confusable pair.
var writeFieldHints = map[string]string{
	"parent_id": "parent_comment_id",
}

// writeFieldExplanations overrides the generic rejection message for a
// specific (action, field) pair, keyed "action:field". Use this only when the
// generic "not a valid field" message would be misleading because the field
// IS valid on other actions (e.g. format, which works on create, update,
// append, reply_comment).
var writeFieldExplanations = map[string]string{
	"comment:format":      `format is not supported for action "comment" — comment bodies are always converted from Markdown; raw XHTML in comments is not yet supported`,
	"edit_comment:format": `format is not supported for action "edit_comment" — comment bodies are always converted from Markdown; raw XHTML in comments is not yet supported`,
}

// validateWriteItemFields rejects any field an item supplies that its
// action's handler does not consume — whether the field is one the action
// never uses (e.g. comment_type on comment) or the wrong one of a confusable
// pair (e.g. parent_id on reply_comment); both are hard errors (D6).
func validateWriteItemFields(action string, item WriteItem) error {
	permitted := permittedWriteFields[action]
	for _, f := range writeFields {
		if !f.set(item) || permitted[f.name] {
			continue
		}
		if explanation, ok := writeFieldExplanations[action+":"+f.name]; ok {
			return errors.New(explanation)
		}
		if hint, ok := writeFieldHints[f.name]; ok {
			return fmt.Errorf("%s is not a valid field for action %q — did you mean %s?", f.name, action, hint)
		}
		return fmt.Errorf("%s is not a valid field for action %q", f.name, action)
	}
	return nil
}

func (h *handlers) writeCreate(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.PageID != "" && (item.Format == "storage" || !reMacroCommentCheck.MatchString(item.Body)) {
		return "", fmt.Errorf(`page_id is only valid for action "create" when body carries <!-- macro:mN --> sentinels (it names the source page whose macro registry to reuse) — drop it, or use action "update" to modify an existing page`)
	}

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

// writeReplyComment handles the "reply_comment" action: post a reply to an
// existing footer or inline comment (D1 — replies only, no new anchored
// inline comments). Body handling is deliberately local rather than shared
// with writeComment/writeEditComment, since a reply sends no page_id at all
// (D4) and those two rely on item.PageID for macro-registry lookups.
func (h *handlers) writeReplyComment(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.ParentCommentID == "" {
		return "", fmt.Errorf("parent_comment_id is required for reply_comment")
	}
	// Missing and invalid are reported separately: "is required" misdescribes a
	// value that was supplied but wrong, and the caller's next action differs.
	if item.CommentType == "" {
		return "", fmt.Errorf("comment_type is required for reply_comment — use %q or %q", commentFooter, commentInline)
	}
	if item.CommentType != string(commentFooter) && item.CommentType != string(commentInline) {
		return "", fmt.Errorf("invalid comment_type %q for reply_comment — use %q or %q", item.CommentType, commentFooter, commentInline)
	}
	if item.Body == "" {
		return "", fmt.Errorf("body is required for reply_comment")
	}
	kind := commentKind(item.CommentType)

	var storageBody string
	if item.Format == "storage" {
		storageBody = item.Body
	} else {
		storageBody = mdconv.ToStorageFormat(item.Body)
	}

	if dryRun {
		return fmt.Sprintf("Would add %s reply to comment %s:\n%s", kind, item.ParentCommentID, storageBody), nil
	}

	if kind == commentFooter {
		comment, err := h.client.AddFooterCommentReply(ctx, item.ParentCommentID, storageBody)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Added %s reply %s to comment %s", kind, comment.ID, item.ParentCommentID), nil
	}

	comment, err := h.client.AddInlineCommentReply(ctx, item.ParentCommentID, storageBody)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added %s reply %s to comment %s", kind, comment.ID, item.ParentCommentID), nil
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
