package mdconv

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ── Handler 1: Code macros ────────────────────────────────────────────────────

// reCodeLangParam extracts the language parameter value from a code macro.
var reCodeLangParam = regexp.MustCompile(
	`(?s)<ac:parameter[^>]*ac:name="language"[^>]*>([^<]*)</ac:parameter>`,
)

// reCodeCDATA extracts the CDATA body from a code macro.
var reCodeCDATA = regexp.MustCompile(
	`(?s)<ac:plain-text-body><!\[CDATA\[(.*?)]]></ac:plain-text-body>`,
)

// convertCodeMacro converts a single code macro block (already isolated to one
// macro) to a <pre><code> HTML element, returning the replacement string.
func convertCodeMacro(block string, log *ConversionLog) string {
	cdataSub := reCodeCDATA.FindStringSubmatch(block)
	if cdataSub == nil {
		log.Skip("code_block: no CDATA body found")
		return block
	}
	body := cdataSub[1]

	langSub := reCodeLangParam.FindStringSubmatch(block)
	if langSub != nil {
		lang := strings.TrimSpace(langSub[1])
		log.Element("code_block")
		return `<pre><code class="language-` + lang + `">` + body + `</code></pre>`
	}

	log.Element("code_block")
	return `<pre><code>` + body + `</code></pre>`
}

// handleCodeMacros converts all code macros in s to <pre><code> HTML.
// We iterate manually from left to right to avoid cross-macro matching:
// for each match found by the outer regex we locate the EARLIEST closing tag
// so that adjacent macros are processed as independent blocks.
func handleCodeMacros(s string, log *ConversionLog) string {
	const openTag = `<ac:structured-macro`
	const closeTag = `</ac:structured-macro>`
	codeMacroPrefix := `ac:name="code"`

	var result strings.Builder
	remaining := s

	for {
		// Find the next opening tag.
		startIdx := strings.Index(remaining, openTag)
		if startIdx == -1 {
			result.WriteString(remaining)
			break
		}

		// Find the end of the opening tag (>).
		openEnd := strings.Index(remaining[startIdx:], ">")
		if openEnd == -1 {
			result.WriteString(remaining)
			break
		}
		openEnd += startIdx // absolute index of '>'
		openTagFull := remaining[startIdx : openEnd+1]

		// Check if this is a code macro.
		if !strings.Contains(openTagFull, codeMacroPrefix) {
			// Not a code macro — emit up to and including the opening tag and continue.
			result.WriteString(remaining[:openEnd+1])
			remaining = remaining[openEnd+1:]
			continue
		}

		// Emit everything before this macro.
		result.WriteString(remaining[:startIdx])

		// Find the earliest </ac:structured-macro> after the opening tag.
		closeStart := strings.Index(remaining[openEnd+1:], closeTag)
		if closeStart == -1 {
			// No closing tag — emit as-is and stop.
			result.WriteString(remaining[startIdx:])
			remaining = ""
			break
		}
		closeStart += openEnd + 1 // absolute index of '<' in closing tag
		closeEnd := closeStart + len(closeTag)

		block := remaining[startIdx:closeEnd]
		result.WriteString(convertCodeMacro(block, log))
		remaining = remaining[closeEnd:]
	}

	return result.String()
}

// ── Handler 2: ADF extension panels ──────────────────────────────────────────

// reADFExtension matches the full ac:adf-extension block, including the
// fallback div that Confluence appends inside the same outer tag.
var reADFExtension = regexp.MustCompile(
	`(?s)<ac:adf-extension>.*?<ac:adf-node[^>]*type="panel"[^>]*>` +
		`.*?<ac:adf-attribute[^>]*key="panel-type"[^>]*>([^<]*)</ac:adf-attribute>` +
		`.*?<ac:adf-content>(.*?)</ac:adf-content>` +
		`.*?</ac:adf-extension>`,
)

// adfPanelLabel maps Confluence panel types to display labels.
func adfPanelLabel(panelType string) string {
	switch strings.ToLower(strings.TrimSpace(panelType)) {
	case "note":
		return "Note:"
	case "warning":
		return "Warning:"
	case "error":
		return "Error:"
	case "success":
		return "Success:"
	default:
		t := strings.TrimSpace(panelType)
		if t == "" {
			return "Note:"
		}
		return strings.ToUpper(t[:1]) + t[1:] + ":"
	}
}

func handleADFPanels(s string, log *ConversionLog) string {
	return reADFExtension.ReplaceAllStringFunc(s, func(match string) string {
		sub := reADFExtension.FindStringSubmatch(match)
		if sub == nil {
			log.Skip("adf_panel: malformed")
			return match
		}
		panelType := strings.TrimSpace(sub[1])
		body := strings.TrimSpace(sub[2])
		label := adfPanelLabel(panelType)
		log.Element("adf_panel")
		return `<blockquote><strong>` + label + `</strong> ` + body + `</blockquote>`
	})
}

// handleADFPanelsAsMacros is the macro-aware variant of handleADFPanels.
// Each ADF panel is registered as a MacroEntry so ToStorageFormatWithMacros
// can rebuild the original ac:adf-extension XML on write-back. counter is
// shared with extractMacros so macro IDs remain globally unique.
func handleADFPanelsAsMacros(s string, log *ConversionLog, counter *int) (string, *MacroRegistry) {
	var registry *MacroRegistry
	result := reADFExtension.ReplaceAllStringFunc(s, func(match string) string {
		sub := reADFExtension.FindStringSubmatch(match)
		if sub == nil {
			log.Skip("adf_panel: malformed")
			return match
		}
		panelType := strings.TrimSpace(sub[1])
		body := strings.TrimSpace(sub[2])
		log.Element("adf_panel")

		*counter++
		id := fmt.Sprintf("m%d", *counter)
		if registry == nil {
			registry = &MacroRegistry{}
		}
		registry.Entries = append(registry.Entries, MacroEntry{
			ID:          id,
			Name:        "adf-panel",
			Category:    CategoryEditableFlat,
			OriginalXML: match,
		})
		log.Macro("adf-panel:" + panelType)
		if alert := panelAlertType(panelType); alert != "" {
			return macroSentinel(id) + `<blockquote><p>[!` + alert + `]</p>` + body + `</blockquote>`
		}
		label := adfPanelLabel(panelType)
		return macroSentinel(id) + `<blockquote><strong>` + label + `</strong> ` + body + `</blockquote>`
	})
	return result, registry
}

// ── Handler 3: Layout sections ────────────────────────────────────────────────

// reLayoutSection matches an ac:layout-section block. Nested layout sections
// are not supported — the non-greedy `(.*?)` will close on the first inner
// </ac:layout-section>, truncating the outer section's content.
var reLayoutSection = regexp.MustCompile(
	`(?s)<ac:layout-section[^>]*ac:type="([^"]*)"[^>]*>(.*?)</ac:layout-section>`,
)

// reLayoutCell matches an ac:layout-cell block within a layout section.
var reLayoutCell = regexp.MustCompile(
	`(?s)<ac:layout-cell[^>]*>(.*?)</ac:layout-cell>`,
)

func handleLayoutSections(s string, log *ConversionLog) string {
	return reLayoutSection.ReplaceAllStringFunc(s, func(match string) string {
		sub := reLayoutSection.FindStringSubmatch(match)
		if sub == nil {
			log.Skip("layout: malformed section")
			return match
		}
		layoutType := strings.TrimSpace(sub[1])
		inner := sub[2]

		cells := reLayoutCell.FindAllStringSubmatch(inner, -1)
		if len(cells) == 0 {
			// No cells found — strip wrappers and return inner content.
			log.Element("layout")
			return inner
		}

		isFixedWidth := layoutType == "fixed-width"
		if isFixedWidth {
			// Single column: strip wrapper tags, keep inner content.
			var sb strings.Builder
			for _, cell := range cells {
				sb.WriteString(strings.TrimSpace(cell[1]))
			}
			log.Element("layout")
			return sb.String()
		}

		// Multi-column: wrap each cell between horizontal borders so the column
		// boundary is visible in the rendered Markdown. The border glyphs are
		// transient visual aids; round-tripping layouts back to storage format
		// is not supported, so there is no inverse parse.
		var sb strings.Builder
		for i, cell := range cells {
			cellContent := strings.TrimSpace(cell[1])
			sb.WriteString(`<p>┈┈ Column `)
			sb.WriteString(strconv.Itoa(i + 1))
			sb.WriteString(` ┈┈</p>`)
			sb.WriteString(cellContent)
		}
		sb.WriteString(`<p>┈┈┈┈┈┈┈┈</p>`)
		log.Element("layout")
		return sb.String()
	})
}

// ── Handler 4: Task lists ─────────────────────────────────────────────────────

// reTaskList matches an ac:task-list block.
var reTaskList = regexp.MustCompile(`(?s)<ac:task-list[^>]*>(.*?)</ac:task-list>`)

// reTask matches a single ac:task within a task list.
var reTask = regexp.MustCompile(`(?s)<ac:task[^>]*>(.*?)</ac:task>`)

// reTaskStatus matches the task status.
var reTaskStatus = regexp.MustCompile(`(?s)<ac:task-status[^>]*>(.*?)</ac:task-status>`)

// reTaskBody matches the task body.
var reTaskBody = regexp.MustCompile(`(?s)<ac:task-body[^>]*>(.*?)</ac:task-body>`)

// rePlaceholderSpan matches the inline-tasks placeholder span wrapper.
var rePlaceholderSpan = regexp.MustCompile(`(?s)<span[^>]*class="placeholder-inline-tasks"[^>]*>(.*?)</span>`)

func handleTaskLists(s string, log *ConversionLog) string {
	return reTaskList.ReplaceAllStringFunc(s, func(listMatch string) string {
		listSub := reTaskList.FindStringSubmatch(listMatch)
		if listSub == nil {
			log.Skip("task_list: malformed outer list")
			return listMatch
		}
		inner := listSub[1]

		var items strings.Builder
		reTask.ReplaceAllStringFunc(inner, func(taskMatch string) string {
			taskSub := reTask.FindStringSubmatch(taskMatch)
			if taskSub == nil {
				return taskMatch
			}
			taskInner := taskSub[1]

			// Extract status.
			statusSub := reTaskStatus.FindStringSubmatch(taskInner)
			status := ""
			if statusSub != nil {
				status = strings.TrimSpace(statusSub[1])
			}

			// Extract body.
			bodySub := reTaskBody.FindStringSubmatch(taskInner)
			body := ""
			if bodySub != nil {
				body = strings.TrimSpace(bodySub[1])
				// Strip placeholder-inline-tasks span wrapper.
				body = rePlaceholderSpan.ReplaceAllString(body, "$1")
				body = strings.TrimSpace(body)
			}

			// Emit literal [x] / [ ] sentinels. html-to-markdown drops raw
			// <input type="checkbox"> elements, but preserves text, so this
			// survives conversion and renders as a GFM task-list item.
			marker := "[ ] "
			if strings.EqualFold(status, "complete") {
				marker = "[x] "
			}
			items.WriteString(`<li>` + marker + body + `</li>`)
			return ""
		})

		log.Element("task_list")
		return `<ul>` + items.String() + `</ul>`
	})
}

// ── Handler 5: Date nodes ─────────────────────────────────────────────────────

// reDateNode matches both self-closing <time datetime="..." /> and paired
// <time datetime="..."></time> forms.
var reDateNode = regexp.MustCompile(`<time\s+datetime="([^"]*)"[^>]*/?>(?:</time>)?`)

func handleDateNodes(s string, log *ConversionLog) string {
	return reDateNode.ReplaceAllStringFunc(s, func(match string) string {
		sub := reDateNode.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		log.Element("date")
		return sub[1]
	})
}

// ── Handler 6: User mentions ──────────────────────────────────────────────────

// reUserMention matches an ac:link wrapping an ri:user reference. The ac:link
// wrapper may contain only the ri:user element (no link-body).
var reUserMention = regexp.MustCompile(
	`(?s)<ac:link[^>]*>\s*<ri:user[^>]*ri:account-id="([^"]*)"[^>]*/?>\s*</ac:link>`,
)

func handleUserMentions(s string, log *ConversionLog, resolver Resolver) string {
	return reUserMention.ReplaceAllStringFunc(s, func(match string) string {
		sub := reUserMention.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		log.Element("user_mention")
		accountID := sub[1]
		if resolver != nil {
			if name, ok := resolver.ResolveUser(accountID); ok && name != "" {
				return "@" + name + " [" + accountID + "]"
			}
		}
		return "@user(" + accountID + ")"
	})
}

// ── Handler 7: Emoticons ──────────────────────────────────────────────────────

// reEmoticon matches an ac:emoticon element (self-closing or paired). Prefers
// ac:emoji-fallback (usually the unicode glyph), falls back to ac:emoji-shortname,
// then ac:name.
var reEmoticon = regexp.MustCompile(`(?s)<ac:emoticon[^>]*/?>(?:</ac:emoticon>)?`)
var reEmoticonFallback = regexp.MustCompile(`ac:emoji-fallback="([^"]*)"`)
var reEmoticonShortname = regexp.MustCompile(`ac:emoji-shortname="([^"]*)"`)
var reEmoticonName = regexp.MustCompile(`ac:name="([^"]*)"`)

// emojiShortnameUnicode maps Atlassian's custom / extended shortnames to a
// Unicode glyph. Confluence emits `ac:emoji-fallback=":cross_mark:"` for icons
// that predate the standard unicode set, leaving the raw shortcode in the
// Markdown. Viewers rarely resolve those shortcodes, so we substitute a glyph
// that conveys the same meaning.
var emojiShortnameUnicode = map[string]string{
	":cross_mark:":          "❌",
	":white_check_mark:":    "✅",
	":check_mark:":          "✔️",
	":warning:":             "⚠️",
	":information_source:":  "ℹ️",
	":no_entry:":            "⛔",
	":no_entry_sign:":       "🚫",
	":heavy_check_mark:":    "✔️",
	":heavy_multiplication_x:": "✖️",
	":question:":            "❓",
	":exclamation:":         "❗",
	":star:":                "⭐",
	":thumbsup:":            "👍",
	":thumbsdown:":          "👎",
	":rocket:":              "🚀",
	":fire:":                "🔥",
	":bulb:":                "💡",
	":memo:":                "📝",
	":lock:":                "🔒",
	":unlock:":              "🔓",
	":eyes:":                "👀",
	":tada:":                "🎉",
	":hourglass:":           "⌛",
	":clock:":               "🕒",
	":zap:":                 "⚡",
}

// isLikelyShortcode reports whether v looks like a `:shortname:` literal rather
// than a rendered glyph.
func isLikelyShortcode(v string) bool {
	return len(v) >= 2 && v[0] == ':' && v[len(v)-1] == ':'
}

func handleEmoticons(s string, log *ConversionLog) string {
	return reEmoticon.ReplaceAllStringFunc(s, func(match string) string {
		log.Element("emoticon")
		if m := reEmoticonFallback.FindStringSubmatch(match); m != nil && m[1] != "" {
			fallback := m[1]
			if isLikelyShortcode(fallback) {
				if glyph, ok := emojiShortnameUnicode[fallback]; ok {
					return glyph
				}
			}
			return fallback
		}
		if m := reEmoticonShortname.FindStringSubmatch(match); m != nil && m[1] != "" {
			short := m[1]
			if glyph, ok := emojiShortnameUnicode[short]; ok {
				return glyph
			}
			return short
		}
		if m := reEmoticonName.FindStringSubmatch(match); m != nil && m[1] != "" {
			name := ":" + m[1] + ":"
			if glyph, ok := emojiShortnameUnicode[name]; ok {
				return glyph
			}
			return name
		}
		return ""
	})
}

// ── Handler 8: Images with ri:attachment ──────────────────────────────────────

// reACImageAttachment matches an ac:image whose inner reference is an
// ri:attachment (the other common form alongside ri:url, which is handled by
// preprocessConfluenceXML).
var reACImageAttachment = regexp.MustCompile(
	`(?s)<ac:image([^>]*)>\s*<ri:attachment[^>]*ri:filename="([^"]*)"[^>]*/?>\s*(?:</ri:attachment>)?\s*</ac:image>`,
)
var reACImageAlt = regexp.MustCompile(`ac:alt="([^"]*)"`)

func handleAttachmentImages(s string, log *ConversionLog) string {
	return reACImageAttachment.ReplaceAllStringFunc(s, func(match string) string {
		sub := reACImageAttachment.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		attrs := sub[1]
		filename := sub[2]
		alt := filename
		if m := reACImageAlt.FindStringSubmatch(attrs); m != nil && m[1] != "" {
			alt = m[1]
		}
		log.Element("image_attachment")
		return `<img alt="` + alt + `" src="attachment:` + filename + `" />`
	})
}

// ── Handler 9: ac:link with ac:anchor (in-page navigation) ────────────────────

// reAnchorLink matches an ac:link that targets a named anchor within the page.
// The link body may be either <ac:link-body> or <ac:plain-text-link-body>.
var reAnchorLink = regexp.MustCompile(
	`(?s)<ac:link[^>]*ac:anchor="([^"]*)"[^>]*>\s*` +
		`(?:<ac:link-body>(.*?)</ac:link-body>|<ac:plain-text-link-body>(?:<!\[CDATA\[)?(.*?)(?:]]>)?</ac:plain-text-link-body>)?\s*` +
		`</ac:link>`,
)

func handleAnchorLinks(s string, log *ConversionLog) string {
	return reAnchorLink.ReplaceAllStringFunc(s, func(match string) string {
		sub := reAnchorLink.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		anchor := sub[1]
		body := sub[2]
		if body == "" {
			body = sub[3]
		}
		if body == "" {
			body = anchor
		}
		log.Element("anchor_link")
		return `<a href="#` + anchor + `">` + body + `</a>`
	})
}

// ── Handler 10: Subscript and superscript ─────────────────────────────────────

// Pandoc-style `~x~` / `^x^` markers are not GFM and do not render in IntelliJ
// or GitHub. mdconv.go registers <sub> and <sup> as raw HTML pass-through in
// html-to-markdown, so we leave the tags alone and only record telemetry.
var reSub = regexp.MustCompile(`(?s)<sub>(.*?)</sub>`)
var reSup = regexp.MustCompile(`(?s)<sup>(.*?)</sup>`)

func handleSubSup(s string, log *ConversionLog) string {
	for range reSub.FindAllStringIndex(s, -1) {
		log.Element("subscript")
	}
	for range reSup.FindAllStringIndex(s, -1) {
		log.Element("superscript")
	}
	return s
}

// ── Handler 11: HTML comment artifacts ───────────────────────────────────────

// reCommentArtifact matches the literal <!--THE END--> comment.
var reCommentArtifact = regexp.MustCompile(`<!--THE END-->`)

func handleCommentArtifacts(s string, log *ConversionLog) string {
	return reCommentArtifact.ReplaceAllStringFunc(s, func(_ string) string {
		log.Element("comment_artifact")
		return ""
	})
}

// ── Entry point ───────────────────────────────────────────────────────────────

// preprocessNonMacroElements converts non-macro Confluence elements (code macros,
// ADF panels, task lists, date nodes, layouts, colspans, HTML comment artifacts)
// into standard HTML before extractMacros runs. Handlers increment log counters
// when log is non-nil. The optional resolver is consulted when expanding
// user mentions to display names — nil falls back to `@user(accountId)`.
//
// For macro-aware conversion, callers run handleADFPanelsAsMacros BEFORE
// invoking this function so ADF panels are registered as MacroEntries and
// replaced with sentinels; by the time handleADFPanels runs here, no
// ac:adf-extension elements remain and it becomes a no-op.
func preprocessNonMacroElements(s string, log *ConversionLog, resolver Resolver) string {
	// 1. Code macros — must run first so extractMacros never sees them.
	s = handleCodeMacros(s, log)
	// 2. ADF extension panels (no-op in macro-aware path — already handled).
	s = handleADFPanels(s, log)
	// 3. Layout sections — strip before task lists (tasks can appear inside cells).
	s = handleLayoutSections(s, log)
	// 4. Task lists.
	s = handleTaskLists(s, log)
	// 5. Date nodes.
	s = handleDateNodes(s, log)
	// 6. Attachment images — must run before extractMacros so view-file macros
	//    that wrap <ri:attachment> inside a parameter aren't touched.
	s = handleAttachmentImages(s, log)
	// 7. Anchor in-page links.
	s = handleAnchorLinks(s, log)
	// 8. User mentions.
	s = handleUserMentions(s, log, resolver)
	// 9. Emoticons.
	s = handleEmoticons(s, log)
	// 10. Sub/sup.
	s = handleSubSup(s, log)
	// 11. HTML comment artifacts.
	s = handleCommentArtifacts(s, log)
	return s
}
