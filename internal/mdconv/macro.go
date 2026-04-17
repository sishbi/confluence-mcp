package mdconv

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	macroOpenTag  = "<ac:structured-macro"
	macroCloseTag = "</ac:structured-macro>"
)

// MacroCategory represents the classification of a Confluence macro.
type MacroCategory string

// Macro category constants.
const (
	CategoryEditableFlat       MacroCategory = "editable-flat"
	CategoryEditableStructured MacroCategory = "editable-structured"
	CategoryOpaque             MacroCategory = "opaque"
	// CategoryOpaqueBody renders the rich-text-body content inline (so tables
	// and other block elements survive) but restores the original XML
	// verbatim on write-back. Used for macros like `details` where the body
	// is structural (a table) but editing semantics are complex.
	CategoryOpaqueBody MacroCategory = "opaque-body"
)

// MacroEntry stores one extracted macro's metadata and original XML.
type MacroEntry struct {
	ID          string        // "m1", "m2", ...
	Name        string        // "info", "expand", "toc", etc.
	Category    MacroCategory // one of the Category* constants
	OriginalXML string        // complete original ac:structured-macro XML
}

// MacroRegistry holds all macros extracted from a single page.
type MacroRegistry struct {
	Entries []MacroEntry
}

// Lookup returns the entry for the given ID, or nil if not found.
func (r *MacroRegistry) Lookup(id string) *MacroEntry {
	if r == nil {
		return nil
	}
	for i := range r.Entries {
		if r.Entries[i].ID == id {
			return &r.Entries[i]
		}
	}
	return nil
}

// mergeRegistries concatenates two registries. Either may be nil; the result
// is nil when both are nil.
func mergeRegistries(a, b *MacroRegistry) *MacroRegistry {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	a.Entries = append(a.Entries, b.Entries...)
	return a
}

// classifyMacro returns the category for a given macro name.
func classifyMacro(name string) MacroCategory {
	switch name {
	case "info", "note", "warning", "tip", "excerpt":
		return CategoryEditableFlat
	case "expand":
		return CategoryEditableStructured
	case "details":
		return CategoryOpaqueBody
	default:
		return CategoryOpaque
	}
}

var (
	reMacroTitle   = regexp.MustCompile(`(?s)<ac:parameter[^>]*ac:name="title"[^>]*>([^<]*)</ac:parameter>`)
	reParamColour  = regexp.MustCompile(`(?s)<ac:parameter[^>]*ac:name="colour"[^>]*>([^<]*)</ac:parameter>`)
	reParamKey     = regexp.MustCompile(`(?s)<ac:parameter[^>]*ac:name="key"[^>]*>([^<]*)</ac:parameter>`)
	reParamDepth   = regexp.MustCompile(`(?s)<ac:parameter[^>]*ac:name="depth"[^>]*>([^<]*)</ac:parameter>`)
	reRichTextBody = regexp.MustCompile(`(?s)<ac:rich-text-body>(.*?)</ac:rich-text-body>`)
	reStripTags    = regexp.MustCompile(`<[^>]+>`)
	reMacroName    = regexp.MustCompile(`ac:name="([^"]*)"`)
	// reAnyParam matches the first <ac:parameter> in a body, regardless of its
	// ac:name attribute (Confluence sometimes emits ac:name="" for anchors).
	reAnyParam = regexp.MustCompile(`(?s)<ac:parameter[^>]*>([^<]*)</ac:parameter>`)
	// reAttachmentFilename pulls the filename from an <ri:attachment>, used by
	// the view-file macro summary.
	reAttachmentFilename = regexp.MustCompile(`<ri:attachment[^>]*ri:filename="([^"]*)"`)
)

// parseMacroName extracts the ac:name attribute value from an opening
// <ac:structured-macro ...> tag string (the tag itself, not including ">").
// Returns "" if the attribute is not found.
func parseMacroName(openTag string) string {
	m := reMacroName.FindStringSubmatch(openTag)
	if m == nil {
		return ""
	}
	return m[1]
}

// findMatchingClose returns the position immediately after the
// </ac:structured-macro> that closes the macro whose opening tag the caller
// has already consumed. startAfterOpen is the index in s from which to begin
// scanning (i.e. the position just after the opening tag's ">").
//
// The algorithm counts depth: depth starts at 1 (the caller's open tag is
// already counted). Each nested <ac:structured-macro is +1; each
// </ac:structured-macro> is -1. When depth reaches 0 the position after
// that closing tag is returned. Returns -1 if no balanced close is found.
//
// Limitation: the scanner treats CDATA content as regular text. If a macro's
// CDATA body contains the literal string "<ac:structured-macro" or
// "</ac:structured-macro>", depth counting will be thrown off and the scan
// will likely return -1, leaving the outer macro unextracted in the output.
// This does not affect code macros (handled in Pass 0) but could affect
// noformat macros whose body contains that literal string. In that pathological
// case the macro degrades gracefully — raw XML remains in the output rather
// than being silently truncated.
func findMatchingClose(s string, startAfterOpen int) int {
	depth := 1
	pos := startAfterOpen
	for depth > 0 {
		// Find next open and next close from current position.
		openIdx := strings.Index(s[pos:], macroOpenTag)
		closeIdx := strings.Index(s[pos:], macroCloseTag)

		hasOpen := openIdx >= 0
		hasClose := closeIdx >= 0

		if !hasClose {
			// No close tag — unbalanced.
			return -1
		}

		if hasOpen && openIdx < closeIdx {
			// Next event is an open tag.
			depth++
			pos += openIdx + len(macroOpenTag)
		} else {
			// Next event is a close tag.
			depth--
			closeEnd := pos + closeIdx + len(macroCloseTag)
			if depth == 0 {
				return closeEnd
			}
			pos = closeEnd
		}
	}
	return -1
}

// extractMacros walks the XHTML and replaces ac:structured-macro elements with
// HTML placeholders, returning the modified string and a registry of the
// extracted macros. Returns nil registry when no macros were found.
//
// Nested macros are handled correctly: the outer macro's OriginalXML contains
// the full (depth-balanced) XML including any inner macros. Inner macros are
// also extracted as separate registry entries by recursing into the outer
// macro's body content. Each macro (outer and inner) appears in the registry
// and receives its own placeholder in the output.
func extractMacros(xhtml string, log *ConversionLog) (string, *MacroRegistry) {
	var registry *MacroRegistry
	counter := 0

	processed, reg := extractMacrosInto(xhtml, log, &counter, nil)
	registry = reg
	return processed, registry
}

// extractMacrosInto is the recursive worker for extractMacros. It scans xhtml
// for top-level <ac:structured-macro> elements, extracts each with correct
// depth balancing, recurses into the body to handle nested macros, and builds
// placeholders. counter is shared across all recursion levels so IDs are
// globally unique and monotonically increasing.
func extractMacrosInto(xhtml string, log *ConversionLog, counter *int, resolver Resolver) (string, *MacroRegistry) {
	var registry *MacroRegistry
	var sb strings.Builder
	pos := 0

	for {
		// Find next top-level open tag.
		openIdx := strings.Index(xhtml[pos:], macroOpenTag)
		if openIdx < 0 {
			// No more macros — append the rest and stop.
			sb.WriteString(xhtml[pos:])
			break
		}
		openStart := pos + openIdx

		// Append everything before this macro.
		sb.WriteString(xhtml[pos:openStart])

		// Find the end of the opening tag (the ">").
		tagEnd := strings.Index(xhtml[openStart:], ">")
		if tagEnd < 0 {
			// Malformed — append from here and stop.
			sb.WriteString(xhtml[openStart:])
			break
		}
		openTagEnd := openStart + tagEnd + 1 // position just after ">"

		openTag := xhtml[openStart:openTagEnd]
		name := parseMacroName(openTag)

		// Find the matching close tag using depth balancing.
		closeEnd := findMatchingClose(xhtml, openTagEnd)
		if closeEnd < 0 {
			// Unbalanced — append from the open tag and stop.
			sb.WriteString(xhtml[openStart:])
			break
		}

		// Full macro XML: openStart..closeEnd.
		fullXML := xhtml[openStart:closeEnd]

		// Body: between opening tag's ">" and the start of the matching "</ac:structured-macro>".
		// The matching close is at closeEnd - len(macroCloseTag).
		closeStart := closeEnd - len(macroCloseTag)
		bodyXML := xhtml[openTagEnd:closeStart]

		if log != nil {
			log.Macro(name)
		}

		// Recurse into the body to extract any nested macros.
		processedBody, innerReg := extractMacrosInto(bodyXML, log, counter, resolver)
		if innerReg != nil {
			if registry == nil {
				registry = &MacroRegistry{}
			}
			registry.Entries = append(registry.Entries, innerReg.Entries...)
		}

		// Now register THIS macro.
		*counter++
		id := fmt.Sprintf("m%d", *counter)
		cat := classifyMacro(name)

		if registry == nil {
			registry = &MacroRegistry{}
		}
		registry.Entries = append(registry.Entries, MacroEntry{
			ID:          id,
			Name:        name,
			Category:    cat,
			OriginalXML: fullXML,
		})

		// Build the placeholder, using the body with inner placeholders substituted.
		var placeholder string
		switch cat {
		case CategoryEditableFlat:
			placeholder = buildEditableFlatPlaceholder(id, name, processedBody)
		case CategoryEditableStructured:
			placeholder = buildEditableStructuredPlaceholder(id, name, processedBody)
		case CategoryOpaqueBody:
			placeholder = buildOpaqueBodyPlaceholder(id, name, processedBody)
		default:
			placeholder = buildOpaquePlaceholder(id, name, processedBody, resolver)
		}
		sb.WriteString(placeholder)

		pos = closeEnd
	}

	if registry == nil {
		return sb.String(), nil
	}
	return sb.String(), registry
}

// macroSentinel returns the sentinel token that will survive HTML-to-Markdown conversion,
// allowing insertMacroComments to locate and replace it with a <!-- macro:mN --> comment.
func macroSentinel(id string) string {
	return "MACROSENTINEL:" + id + ":"
}

// macroEndSentinel returns the end sentinel token that marks the close of a structured
// expand block. After HTML-to-Markdown conversion, insertMacroComments replaces it
// with <!-- /macro:mN -->.
func macroEndSentinel(id string) string {
	return "MACROENDSENTINEL:" + id + ":"
}

// buildEditableFlatPlaceholder renders a blockquote. Panel-style macros
// (info/note/warning/tip/error, plus ADF panel variants) render as GFM alerts
// (`> [!NOTE]`); other editable-flat macros (excerpt) keep the bold-label form.
func buildEditableFlatPlaceholder(id, macroName, macroBody string) string {
	richBody := ""
	if m := reRichTextBody.FindStringSubmatch(macroBody); m != nil {
		richBody = m[1]
	}
	if alert := panelAlertType(macroName); alert != "" {
		return macroSentinel(id) + fmt.Sprintf(`<blockquote><p>[!%s]</p>%s</blockquote>`, alert, richBody)
	}
	label := macroName + ":"
	if len(macroName) > 0 {
		label = strings.ToUpper(macroName[:1]) + macroName[1:] + ":"
	}
	return macroSentinel(id) + fmt.Sprintf(`<blockquote><strong>%s</strong> %s</blockquote>`, label, richBody)
}

// panelAlertType maps a Confluence panel macro name (info/note/warning/tip/error/success)
// to a GFM alert type. Returns "" for names that are not panel-like.
func panelAlertType(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "info", "note":
		return "NOTE"
	case "tip", "success":
		return "TIP"
	case "warning":
		return "WARNING"
	case "error":
		return "CAUTION"
	}
	return ""
}

// expandBorder is the visual delimiter drawn above and below an expand macro's
// body so readers of raw Markdown can see where the collapsible region starts
// and ends. Stripped on write-back by stripExpandBorders so the border does
// not leak into the rich-text-body storage XML.
const expandBorder = "┈┈┈┈┈┈┈┈"

// buildEditableStructuredPlaceholder renders a div with a bold title line,
// a horizontal border above the body, the body, and a matching border below.
// The sentinel is placed inside the <p> so that after html-to-markdown
// conversion it appears on the same line as the title, giving:
//
//	<!-- macro:mN -->**▶ title**
//
//	┈┈┈┈┈┈┈┈
//
//	Body content.
//
//	┈┈┈┈┈┈┈┈
//
//	<!-- /macro:mN -->
func buildEditableStructuredPlaceholder(id, macroName, macroBody string) string {
	title := macroName
	if m := reMacroTitle.FindStringSubmatch(macroBody); m != nil {
		title = m[1]
	}
	richBody := ""
	if m := reRichTextBody.FindStringSubmatch(macroBody); m != nil {
		richBody = m[1]
	}
	return fmt.Sprintf(`<div><p>%s<strong>▶ %s</strong></p><p>%s</p>%s<p>%s</p><p>%s</p></div>`,
		macroSentinel(id), title, expandBorder, richBody, expandBorder, macroEndSentinel(id))
}

// buildOpaquePlaceholder renders a span with a human-readable summary. Some
// macros (view-file, status, children) have custom renderings handled here
// before the generic `[summary]` fallback. When resolver is non-nil and the
// macro is `children`, the placeholder is expanded to a bulleted list of real
// child pages; resolver failures fall back silently to the `[Child pages]`
// summary.
func buildOpaquePlaceholder(id, macroName, macroBody string, resolver Resolver) string {
	switch macroName {
	case "view-file":
		if m := reAttachmentFilename.FindStringSubmatch(macroBody); m != nil {
			filename := m[1]
			href := "attachment:" + url.PathEscape(filename)
			return macroSentinel(id) + fmt.Sprintf(`<a href="%s">%s</a>`, href, filename)
		}
	case "status":
		return macroSentinel(id) + fmt.Sprintf(`<span>%s</span>`, statusBadge(macroBody))
	case "children":
		if resolver != nil {
			if list := resolvedChildrenHTML(macroBody, resolver); list != "" {
				return macroSentinel(id) + list
			}
		}
	}
	summary := opaqueSummary(macroName, macroBody)
	return macroSentinel(id) + fmt.Sprintf(`<span>[%s]</span>`, summary)
}

// resolvedChildrenHTML asks the resolver for child pages and renders them as
// a nested unordered list. Returns "" when the resolver fails or returns no
// children, signalling the caller to fall back to the static placeholder.
// depth is read from the macro's ac:parameter (defaults to 1).
func resolvedChildrenHTML(macroBody string, resolver Resolver) string {
	depth := 1
	if m := reParamDepth.FindStringSubmatch(macroBody); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			depth = n
		}
	}
	// The children macro is always bound to the page it lives on, so we pass
	// an empty parentPageID and let the resolver substitute the current page.
	children, err := resolver.ListChildren("", depth)
	if err != nil || len(children) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<ul>")
	writeChildList(&sb, children)
	sb.WriteString("</ul>")
	return sb.String()
}

func writeChildList(sb *strings.Builder, pages []ChildPage) {
	for _, p := range pages {
		sb.WriteString("<li>")
		if p.URL != "" {
			fmt.Fprintf(sb, `<a href="%s">%s</a>`, htmlAttrEscape(p.URL), htmlTextEscape(p.Title))
		} else {
			sb.WriteString(htmlTextEscape(p.Title))
		}
		if len(p.Children) > 0 {
			sb.WriteString("<ul>")
			writeChildList(sb, p.Children)
			sb.WriteString("</ul>")
		}
		sb.WriteString("</li>")
	}
}

var htmlAttrReplacer = strings.NewReplacer(
	`&`, `&amp;`,
	`"`, `&quot;`,
	`<`, `&lt;`,
	`>`, `&gt;`,
)

var htmlTextReplacer = strings.NewReplacer(
	`&`, `&amp;`,
	`<`, `&lt;`,
	`>`, `&gt;`,
)

func htmlAttrEscape(s string) string { return htmlAttrReplacer.Replace(s) }
func htmlTextEscape(s string) string { return htmlTextReplacer.Replace(s) }

// statusBadge renders a Confluence status lozenge as an emoji-prefixed label.
// Unknown colours fall back to a white circle.
func statusBadge(macroBody string) string {
	title := extractParam(reMacroTitle, macroBody)
	colour := strings.ToLower(extractParam(reParamColour, macroBody))
	dot := statusColourDot(colour)
	switch {
	case title != "" && dot != "":
		return dot + " " + title
	case title != "":
		return "● " + title
	case dot != "":
		return dot + " Status"
	default:
		return "Status"
	}
}

func statusColourDot(colour string) string {
	switch colour {
	case "green":
		return "🟢"
	case "yellow":
		return "🟡"
	case "red":
		return "🔴"
	case "blue":
		return "🔵"
	case "purple":
		return "🟣"
	case "grey", "gray":
		return "⚪"
	default:
		return ""
	}
}

// buildOpaqueBodyPlaceholder emits the macro's rich-text-body inline so nested
// block elements (tables etc.) render in the Markdown. Reuses the expand
// block shape (`**▶ Label:**` title with an end-sentinel) so segmentMarkdown
// can delimit the block reliably. Restore returns the original XML verbatim, so
// any edits to the rendered body are discarded — callers should treat these
// macros as read-only metadata views.
func buildOpaqueBodyPlaceholder(id, macroName, macroBody string) string {
	label := macroName
	if len(macroName) > 0 {
		label = strings.ToUpper(macroName[:1]) + macroName[1:]
	}
	richBody := ""
	if m := reRichTextBody.FindStringSubmatch(macroBody); m != nil {
		richBody = m[1]
	}
	return fmt.Sprintf(`<div><p>%s<strong>▶ %s:</strong></p><p>%s</p>%s<p>%s</p><p>%s</p></div>`,
		macroSentinel(id), label, expandBorder, richBody, expandBorder, macroEndSentinel(id))
}

// extractParam returns the value of the named ac:parameter in body, or "" if not found.
// Uses pre-compiled package-level regexes for known parameter names to avoid per-call compilation.
func extractParam(re *regexp.Regexp, body string) string {
	m := re.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// opaqueSummary returns a short human-readable description for an opaque macro.
func opaqueSummary(name, body string) string {
	switch name {
	case "toc":
		return "Table of Contents"
	case "status":
		title := extractParam(reMacroTitle, body)
		colour := extractParam(reParamColour, body)
		if title != "" && colour != "" {
			return "Status: " + title + " (" + colour + ")"
		}
		if title != "" {
			return "Status: " + title
		}
		return "Status"
	case "code":
		return "Code block"
	case "noformat":
		return "Preformatted text"
	case "anchor":
		if name := extractParam(reAnyParam, body); name != "" {
			return "Anchor: " + name
		}
		return "Anchor"
	case "children":
		return "Child pages"
	case "jira":
		if key := extractParam(reParamKey, body); key != "" {
			return "Jira: " + key
		}
		return "Jira"
	case "view-file":
		if m := reAttachmentFilename.FindStringSubmatch(body); m != nil {
			return "File: " + m[1]
		}
		return "File"
	default:
		text := stripTags(body)
		text = strings.TrimSpace(text)
		if len(text) > 40 {
			text = text[:40] + "..."
		}
		if text != "" {
			return name + ": " + text
		}
		return name
	}
}

// stripTags removes HTML/XML tags, leaving plain text.
func stripTags(s string) string {
	return reStripTags.ReplaceAllString(s, "")
}

var reSentinelToken = regexp.MustCompile(`MACROSENTINEL:(m\d+):`)
var reSentinelEndToken = regexp.MustCompile(`MACROENDSENTINEL:(m\d+):`)

// Segment represents a portion of Markdown text, either plain content or a macro block.
type Segment struct {
	Type    string // "plain" or "macro"
	MacroID string // "" for plain, "m1"/"m2"/... for macro
	Content string // the Markdown text of this segment
}

var reMacroComment = regexp.MustCompile(`<!-- macro:(m\d+) -->`)

// reAnyMacroComment matches both opening `<!-- macro:mN -->` and closing
// `<!-- /macro:mN -->` comments.
var reAnyMacroComment = regexp.MustCompile(`<!-- (/?)macro:(m\d+) -->`)

// renumberMacroMarkers rewrites macro IDs so they appear as m1, m2, ... in
// document order. The pipeline produces IDs in processing order: ADF panels
// are assigned first (handleADFPanelsAsMacros), then structured macros
// depth-first (nested children before their parent). That makes IDs jump
// around in the rendered Markdown, which is confusing for a reader. This
// pass walks the output left-to-right, records each distinct ID in the order
// it first appears, then rewrites both the Markdown comments and the
// registry entry IDs so lookup keys stay in sync.
//
// A no-op when registry is nil or has no entries.
func renumberMacroMarkers(md string, registry *MacroRegistry) (string, *MacroRegistry) {
	if registry == nil || len(registry.Entries) == 0 {
		return md, registry
	}

	oldToNew := make(map[string]string, len(registry.Entries))
	orderedOld := make([]string, 0, len(registry.Entries))
	for _, m := range reAnyMacroComment.FindAllStringSubmatch(md, -1) {
		oldID := m[2]
		if _, seen := oldToNew[oldID]; seen {
			continue
		}
		newID := fmt.Sprintf("m%d", len(orderedOld)+1)
		oldToNew[oldID] = newID
		orderedOld = append(orderedOld, oldID)
	}
	if len(oldToNew) == 0 {
		return md, registry
	}

	renumbered := reAnyMacroComment.ReplaceAllStringFunc(md, func(s string) string {
		sub := reAnyMacroComment.FindStringSubmatch(s)
		newID, ok := oldToNew[sub[2]]
		if !ok {
			return s
		}
		return "<!-- " + sub[1] + "macro:" + newID + " -->"
	})

	oldEntries := make(map[string]MacroEntry, len(registry.Entries))
	for _, e := range registry.Entries {
		oldEntries[e.ID] = e
	}
	newEntries := make([]MacroEntry, 0, len(registry.Entries))
	consumed := make(map[string]struct{}, len(registry.Entries))
	for _, oldID := range orderedOld {
		entry, ok := oldEntries[oldID]
		if !ok {
			continue
		}
		entry.ID = oldToNew[oldID]
		newEntries = append(newEntries, entry)
		consumed[oldID] = struct{}{}
	}
	for _, e := range registry.Entries {
		if _, ok := consumed[e.ID]; ok {
			continue
		}
		newEntries = append(newEntries, e)
	}
	return renumbered, &MacroRegistry{Entries: newEntries}
}

// segmentMarkdown splits a Markdown string at <!-- macro:mN --> comment boundaries.
// Each comment starts a macro segment; everything else is a plain segment.
func segmentMarkdown(md string) []Segment {
	matches := reMacroComment.FindAllStringIndex(md, -1)
	if len(matches) == 0 {
		return []Segment{{Type: "plain", Content: md}}
	}

	var segments []Segment
	pos := 0

	for _, loc := range matches {
		commentStart := loc[0]
		commentEnd := loc[1]

		// Skip if this comment was already consumed by a previous macro's block.
		if commentStart < pos {
			continue
		}

		// Plain segment before this comment (if any).
		if commentStart > pos {
			plain := md[pos:commentStart]
			segments = append(segments, Segment{Type: "plain", Content: plain})
		}

		// Extract macro ID from the comment.
		sub := reMacroComment.FindStringSubmatch(md[commentStart:commentEnd])
		macroID := sub[1]

		// Find where the macro block ends.
		blockEnd := findMacroBlockEnd(md, commentEnd, macroID)

		macroContent := md[commentStart:blockEnd]
		segments = append(segments, Segment{Type: "macro", MacroID: macroID, Content: macroContent})
		pos = blockEnd
	}

	// Trailing plain segment (if any).
	if pos < len(md) {
		segments = append(segments, Segment{Type: "plain", Content: md[pos:]})
	}

	return segments
}

// findMacroBlockEnd determines the end offset of the macro content block starting
// at commentEnd (the character after the --> of a <!-- macro:mN --> comment).
// macroID is the ID of the opening comment (e.g. "m1"), used to locate the matching
// <!-- /macro:mN --> end marker for structured expand blocks.
func findMacroBlockEnd(md string, commentEnd int, macroID string) int {
	rest := md[commentEnd:]

	// Non-newline content follows on the same line. Check if this is a new-format
	// expand block (**▶ title** inline with the comment) or an opaque inline block.
	if len(rest) > 0 && rest[0] != '\n' {
		// New-format expand: inline content starts with "**▶".
		// Consume until the matching <!-- /macro:mN --> end marker, or fall back to
		// next macro comment / EOF for content without an end marker.
		if strings.HasPrefix(rest, "**▶") {
			return consumeExpandBlock(md, commentEnd, macroID)
		}

		// Opaque: non-whitespace content follows on the same line.
		// Consume up to the next macro comment on the same line, or end of line.
		nl := strings.Index(rest, "\n")
		if nl == -1 {
			nl = len(rest)
		}
		// Check if another macro comment starts on this same line.
		nextMacro := reMacroComment.FindStringIndex(rest[:nl])
		if nextMacro != nil {
			return commentEnd + nextMacro[0]
		}
		if nl < len(rest) {
			return commentEnd + nl + 1
		}
		return len(md)
	}

	// Editable: the comment is alone on its line. Skip the newline(s) after the comment.
	// Find the start of the actual content (skip blank lines after comment).
	contentStart := commentEnd
	for contentStart < len(md) && md[contentStart] == '\n' {
		contentStart++
	}
	if contentStart >= len(md) {
		return len(md)
	}

	// Structured with <details> (legacy format): extend to closing </details>.
	if strings.HasPrefix(md[contentStart:], "<details>") {
		idx := strings.Index(md[contentStart:], "</details>")
		if idx != -1 {
			end := contentStart + idx + len("</details>")
			// Consume trailing newlines.
			for end < len(md) && md[end] == '\n' {
				end++
			}
			return end
		}
	}

	// Structured expand — new format: first non-blank line starts with "**▶".
	// Consume until the matching <!-- /macro:mN --> end marker, or fall back.
	// Note: we check for "**▶" before the blockquote check so that a blockquote
	// inside an expand body is not misidentified as the start of a flat macro block.
	if strings.HasPrefix(md[contentStart:], "**▶") {
		return consumeExpandBlock(md, contentStart, macroID)
	}

	// Editable-flat (blockquote): consume lines that start with '>' and blank
	// lines within the blockquote block. Stop at first non-blockquote non-blank line.
	if md[contentStart] == '>' {
		end := contentStart
		for end < len(md) {
			nl := strings.Index(md[end:], "\n")
			if nl == -1 {
				end = len(md)
				break
			}
			lineEnd := end + nl + 1
			// Peek at next line
			nextLineStart := lineEnd
			nextNL := strings.Index(md[nextLineStart:], "\n")
			var nextLine string
			if nextNL == -1 {
				nextLine = md[nextLineStart:]
			} else {
				nextLine = md[nextLineStart : nextLineStart+nextNL]
			}
			end = lineEnd
			// Stop if next line is not blockquote and not blank
			if len(nextLine) > 0 && nextLine[0] != '>' {
				break
			}
			if len(nextLine) == 0 {
				// Blank line — stop the blockquote block here
				break
			}
		}
		return end
	}

	// Opaque (comment on own line): consume the next paragraph so the opaque
	// body is part of the macro segment rather than a trailing plain segment.
	// Stop at the first blank line or at the next macro comment.
	return consumeParagraph(md, contentStart)
}

// consumeParagraph consumes text from contentStart until the first blank line
// or the next `<!-- macro:… -->` / `<!-- /macro:… -->` comment.
func consumeParagraph(md string, contentStart int) int {
	end := contentStart
	for end < len(md) {
		nl := strings.Index(md[end:], "\n")
		if nl == -1 {
			return len(md)
		}
		lineEnd := end + nl + 1
		nextLineStart := lineEnd
		if nextLineStart >= len(md) {
			return lineEnd
		}
		nextNL := strings.Index(md[nextLineStart:], "\n")
		var nextLine string
		if nextNL == -1 {
			nextLine = md[nextLineStart:]
		} else {
			nextLine = md[nextLineStart : nextLineStart+nextNL]
		}
		end = lineEnd
		if strings.TrimSpace(nextLine) == "" {
			break
		}
		trimmed := strings.TrimLeft(nextLine, " \t")
		if strings.HasPrefix(trimmed, "<!-- macro:") || strings.HasPrefix(trimmed, "<!-- /macro:") {
			break
		}
	}
	return end
}

// consumeExpandBlock consumes an expand macro block starting at start, where start
// points to the "**▶" title. macroID is the ID of the opening comment (e.g. "m1").
//
// Primary strategy: look for the matching <!-- /macro:mN --> end marker and consume
// up to and including it (plus any trailing newline). This allows headings inside
// the expand body without premature termination.
//
// Fallback (backward compat, no end marker): extend until the next <!-- macro:mN -->
// open comment or EOF.
func consumeExpandBlock(md string, start int, macroID string) int {
	endMarker := "<!-- /macro:" + macroID + " -->"
	idx := strings.Index(md[start:], endMarker)
	if idx != -1 {
		// Consume up to and including the end marker and one trailing newline.
		end := start + idx + len(endMarker)
		if end < len(md) && md[end] == '\n' {
			end++
		}
		return end
	}

	// Fallback: no end marker found (legacy content or content without end sentinels).
	// Consume until the next <!-- macro:*--> open comment or EOF.
	end := start
	for end < len(md) {
		nl := strings.Index(md[end:], "\n")
		if nl == -1 {
			end = len(md)
			break
		}
		lineEnd := end + nl + 1
		nextLineStart := lineEnd
		if nextLineStart >= len(md) {
			end = lineEnd
			break
		}
		// Stop at the next open macro comment.
		if strings.HasPrefix(md[nextLineStart:], "<!-- macro:") {
			end = lineEnd
			break
		}
		end = lineEnd
	}
	return end
}

// restoreMacro rebuilds the original macro XML from a parsed Markdown segment.
// For editable macros, user edits in the Markdown are incorporated. For opaque
// macros (including opaque-body), the original XML is returned verbatim.
func restoreMacro(entry *MacroEntry, content string) string {
	switch entry.Category {
	case CategoryEditableFlat:
		return restoreEditableFlat(entry, content)
	case CategoryEditableStructured:
		return restoreEditableStructured(entry, content)
	default:
		return entry.OriginalXML // opaque / opaque-body: verbatim restore
	}
}

// reBlockquoteLabel matches the bold label at the start of blockquote content,
// with an optional trailing space (the label may be the sole content of the line).
var reBlockquoteLabel = regexp.MustCompile(`^\*\*[^*]+:\*\* ?$`)

// reBlockquoteLabelWithBody matches a bold label followed by body content on the same line.
var reBlockquoteLabelWithBody = regexp.MustCompile(`^\*\*[^*]+:\*\* (.*)`)

// reBlockquoteAlert matches a GFM alert marker line (the first line of a
// panel-style blockquote). The alert type is reconstructed from the macro
// registry entry on write-back, so the marker is stripped from the body.
var reBlockquoteAlert = regexp.MustCompile(`^\[!(?:NOTE|TIP|WARNING|CAUTION|IMPORTANT)\]$`)

// restoreEditableFlat extracts the user-edited text from a blockquote segment
// and rebuilds the macro XML with the new content replacing the rich-text-body.
func restoreEditableFlat(entry *MacroEntry, content string) string {
	// Collect all blockquote lines, stripping the '> ' prefix and bold label.
	// The label line may be on its own line (label-only) or inline with content.
	var bodyLines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "> ") {
			text := line[2:] // strip '> '
			// Skip lines that are only the bold label (label-only lines).
			if reBlockquoteLabel.MatchString(text) {
				continue
			}
			// Skip GFM alert marker lines ([!NOTE] etc.) — the alert type is
			// implicit in the macro entry and rebuilt from the registry.
			if reBlockquoteAlert.MatchString(text) {
				continue
			}
			// Strip bold label from lines where it precedes body content.
			if m := reBlockquoteLabelWithBody.FindStringSubmatch(text); m != nil {
				text = m[1]
			}
			bodyLines = append(bodyLines, text)
		}
	}
	bodyMD := strings.Join(bodyLines, "\n")
	newBody := ToStorageFormat(bodyMD)
	return rebuildMacroBody(entry.OriginalXML, newBody)
}

// reDetailsBlock matches a <details>/<summary> HTML block (legacy format).
var reDetailsBlock = regexp.MustCompile(`(?s)<details>\s*<summary>(.*?)</summary>(.*?)</details>`)

// reExpandTitle matches the bold expand title line in the new placeholder format.
// Group 1 is the title text (everything between "▶ " and the LAST "**" on the line).
// Uses greedy .* (no newline match by default) so that a title containing internal
// "**bold**" markup is captured in full rather than stopping at the first "**".
var reExpandTitle = regexp.MustCompile(`\*\*▶ (.*)\*\*`)

// reTitleParam matches an ac:parameter with name="title" for replacement.
var reTitleParam = regexp.MustCompile(`(?s)(<ac:parameter[^>]*ac:name="title"[^>]*>)[^<]*(</ac:parameter>)`)

// restoreEditableStructured extracts title and body from an expand macro segment
// and rebuilds the macro XML. Supports two formats:
//  1. Legacy <details>/<summary> format (for backwards compatibility with old cached content).
//  2. New **▶ title** format emitted by buildEditableStructuredPlaceholder.
//
// If neither format is detected, the original XML is returned verbatim since
// title/body boundaries cannot be recovered.
func restoreEditableStructured(entry *MacroEntry, content string) string {
	// 1. Try legacy <details>/<summary> format first.
	if m := reDetailsBlock.FindStringSubmatch(content); m != nil {
		newTitle := strings.TrimSpace(m[1])
		bodyMD := strings.TrimSpace(m[2])
		return applyExpandEdits(entry, newTitle, bodyMD)
	}

	// 2. Try new **▶ title** format.
	// Find the title line.
	titleMatch := reExpandTitle.FindStringSubmatchIndex(content)
	if titleMatch != nil {
		newTitle := content[titleMatch[2]:titleMatch[3]]
		// Everything after the title line's closing "**".
		afterTitle := content[titleMatch[1]:]
		afterTitle = strings.TrimLeft(afterTitle, "\n")

		// Look for the matching end marker <!-- /macro:mN --> to bound the body.
		endMarker := "<!-- /macro:" + entry.ID + " -->"
		var bodyMD string
		if idx := strings.Index(afterTitle, endMarker); idx != -1 {
			bodyMD = strings.TrimSpace(afterTitle[:idx])
		} else {
			// Fallback: no end marker — everything after the title is the body.
			bodyMD = strings.TrimSpace(afterTitle)
		}
		return applyExpandEdits(entry, newTitle, bodyMD)
	}

	// 3. Fallback: cannot recover structure.
	return entry.OriginalXML
}

// reExpandBorderLine matches a line whose only non-whitespace content is a run
// of horizontal-border glyphs (┈) — the delimiter emitted around an expand
// body. Round-trip must strip these before converting the body back to
// storage, otherwise the glyphs accumulate as plain paragraphs.
var reExpandBorderLine = regexp.MustCompile(`(?m)^[\t ]*┈+[\t ]*\r?\n?`)

// stripExpandBorders removes border-only lines from an expand body.
func stripExpandBorders(s string) string {
	return reExpandBorderLine.ReplaceAllString(s, "")
}

// applyExpandEdits updates the title parameter and rich-text-body in the original
// expand macro XML using the provided Markdown title and body.
func applyExpandEdits(entry *MacroEntry, newTitle, bodyMD string) string {
	bodyMD = strings.TrimSpace(stripExpandBorders(bodyMD))
	newBody := ToStorageFormat(bodyMD)
	// Update title parameter.
	xml := reTitleParam.ReplaceAllStringFunc(entry.OriginalXML, func(match string) string {
		sub := reTitleParam.FindStringSubmatch(match)
		return sub[1] + newTitle + sub[2]
	})
	// Update rich-text-body.
	xml = rebuildMacroBody(xml, newBody)
	return xml
}

// reADFContent matches an ADF panel's content wrapper.
var reADFContent = regexp.MustCompile(`(?s)<ac:adf-content>(.*?)</ac:adf-content>`)

// rebuildMacroBody replaces the body of macroXML with newBody. Handles both
// <ac:rich-text-body> (used by ac:structured-macro panels) and <ac:adf-content>
// (used by ac:adf-extension panels).
func rebuildMacroBody(macroXML, newBody string) string {
	if reRichTextBody.MatchString(macroXML) {
		return reRichTextBody.ReplaceAllLiteralString(macroXML, "<ac:rich-text-body>"+newBody+"</ac:rich-text-body>")
	}
	if reADFContent.MatchString(macroXML) {
		return reADFContent.ReplaceAllLiteralString(macroXML, "<ac:adf-content>"+newBody+"</ac:adf-content>")
	}
	return macroXML
}

// reMacroSentinelLine matches a macro sentinel comment (either open or close)
// with any surrounding horizontal whitespace on the same line. Used by
// normalizeMacroSentinelLines to reposition each sentinel on its own line.
var reMacroSentinelLine = regexp.MustCompile(`[\t ]*(<!-- /?macro:m\d+ -->)[\t ]*`)

// normalizeMacroSentinelLines forces every macro sentinel onto its own line
// with blank lines around it. CommonMark treats a line that starts with an
// HTML comment as a raw HTML block, which means any Markdown markup on the
// same line as the comment renders as literal text. Isolating the sentinel
// keeps the rendered Markdown readable while preserving the sentinel for the
// write-back path.
func normalizeMacroSentinelLines(md string) string {
	md = reMacroSentinelLine.ReplaceAllString(md, "\n\n$1\n\n")
	md = reBlankLineRun.ReplaceAllString(md, "\n\n")
	return strings.TrimLeft(md, "\n")
}

// insertMacroComments post-processes Markdown output to replace sentinel tokens
// with HTML comments. Each placeholder builder embeds a sentinel token
// (MACROSENTINEL:mN:) that survives HTML-to-Markdown conversion. Structured expand
// placeholders also embed an end sentinel (MACROENDSENTINEL:mN:). This function
// replaces those tokens with <!-- macro:mN --> and <!-- /macro:mN --> respectively,
// then normalizes each comment onto its own line.
func insertMacroComments(md string, registry *MacroRegistry) string {
	if registry == nil || len(registry.Entries) == 0 {
		return md
	}

	// Replace end sentinel tokens first so the open sentinel regex doesn't overlap.
	md = reSentinelEndToken.ReplaceAllStringFunc(md, func(m string) string {
		sub := reSentinelEndToken.FindStringSubmatch(m)
		return "<!-- /macro:" + sub[1] + " -->"
	})

	// Replace each open sentinel token with a <!-- macro:mN --> comment.
	md = reSentinelToken.ReplaceAllStringFunc(md, func(m string) string {
		sub := reSentinelToken.FindStringSubmatch(m)
		return "<!-- macro:" + sub[1] + " -->"
	})

	return normalizeMacroSentinelLines(md)
}
