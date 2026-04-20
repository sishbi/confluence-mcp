package mdconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// helpers

func newLog() *ConversionLog {
	return NewConversionLog()
}

// ── Handler 1: Code macros ────────────────────────────────────────────────────

func TestPreprocessNonMacroElements_CodeWithLang(t *testing.T) {
	input := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[fmt.Println("hi")]]></ac:plain-text-body></ac:structured-macro>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, `<pre><code class="language-go">`)
	assert.Contains(t, result, `fmt.Println("hi")`)
	assert.Contains(t, result, `</code></pre>`)
	assert.NotContains(t, result, "ac:structured-macro")
	assert.Equal(t, 1, log.Elements["code_block"])
}

func TestPreprocessNonMacroElements_CodeNoLang(t *testing.T) {
	input := `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[some code here]]></ac:plain-text-body></ac:structured-macro>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, `<pre><code>some code here</code></pre>`)
	assert.NotContains(t, result, "ac:structured-macro")
	assert.Equal(t, 1, log.Elements["code_block"])
}

func TestPreprocessNonMacroElements_CodeWithLang_CountsMultiple(t *testing.T) {
	block := func(lang, body string) string {
		if lang != "" {
			return `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">` + lang +
				`</ac:parameter><ac:plain-text-body><![CDATA[` + body + `]]></ac:plain-text-body></ac:structured-macro>`
		}
		return `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[` + body + `]]></ac:plain-text-body></ac:structured-macro>`
	}
	input := block("go", "x := 1") + block("", "plain code") + block("python", "print(1)")
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Equal(t, 3, log.Elements["code_block"])
	assert.Contains(t, result, `language-go`)
	assert.Contains(t, result, `language-python`)
}

// ── Handler 2: ADF extension panels ──────────────────────────────────────────

func TestPreprocessNonMacroElements_ADFPanel(t *testing.T) {
	cases := []struct {
		panelType string
		wantLabel string
	}{
		{"note", "Note:"},
		{"warning", "Warning:"},
		{"error", "Error:"},
		{"success", "Success:"},
		{"info", "Info:"},
		{"custom", "Custom:"},
	}

	for _, tc := range cases {
		t.Run(tc.panelType, func(t *testing.T) {
			input := `<ac:adf-extension>` +
				`<ac:adf-node type="panel">` +
				`<ac:adf-attribute key="panel-type">` + tc.panelType + `</ac:adf-attribute>` +
				`<ac:adf-content><p>Panel body text.</p></ac:adf-content>` +
				`</ac:adf-node>` +
				`<ac:adf-fallback><div>fallback</div></ac:adf-fallback>` +
				`</ac:adf-extension>`
			log := newLog()

			result := preprocessNonMacroElements(input, log, nil)

			assert.Contains(t, result, `<blockquote><strong>`+tc.wantLabel+`</strong>`)
			assert.Contains(t, result, `Panel body text.`)
			assert.NotContains(t, result, "ac:adf-extension")
			assert.NotContains(t, result, "fallback")
			assert.Equal(t, 1, log.Elements["adf_panel"])
		})
	}
}

func TestPreprocessNonMacroElements_ADFPanel_Multiple(t *testing.T) {
	panel := func(ptype, body string) string {
		return `<ac:adf-extension><ac:adf-node type="panel">` +
			`<ac:adf-attribute key="panel-type">` + ptype + `</ac:adf-attribute>` +
			`<ac:adf-content>` + body + `</ac:adf-content>` +
			`</ac:adf-node></ac:adf-extension>`
	}
	input := panel("note", "First") + panel("warning", "Second")
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Equal(t, 2, log.Elements["adf_panel"])
	assert.Contains(t, result, "Note:")
	assert.Contains(t, result, "Warning:")
	assert.Contains(t, result, "First")
	assert.Contains(t, result, "Second")
}

// ── Handler 3: Layout sections ────────────────────────────────────────────────

func TestPreprocessNonMacroElements_LayoutColumns_FixedWidth(t *testing.T) {
	input := `<ac:layout-section ac:type="fixed-width">` +
		`<ac:layout-cell><p>Single column content.</p></ac:layout-cell>` +
		`</ac:layout-section>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, "Single column content.")
	assert.NotContains(t, result, "ac:layout-section")
	assert.NotContains(t, result, "Column 1")
	assert.Equal(t, 1, log.Elements["layout"])
}

func TestPreprocessNonMacroElements_LayoutColumns_TwoEqual(t *testing.T) {
	input := `<ac:layout-section ac:type="two_equal">` +
		`<ac:layout-cell><p>Left column.</p></ac:layout-cell>` +
		`<ac:layout-cell><p>Right column.</p></ac:layout-cell>` +
		`</ac:layout-section>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, "┈┈ Column 1 ┈┈")
	assert.Contains(t, result, "┈┈ Column 2 ┈┈")
	assert.Contains(t, result, "Left column.")
	assert.Contains(t, result, "Right column.")
	assert.NotContains(t, result, "ac:layout-section")
	assert.Equal(t, 1, log.Elements["layout"])
}

func TestPreprocessNonMacroElements_LayoutColumns_ThreeEqual(t *testing.T) {
	input := `<ac:layout-section ac:type="three_equal">` +
		`<ac:layout-cell><p>Col A.</p></ac:layout-cell>` +
		`<ac:layout-cell><p>Col B.</p></ac:layout-cell>` +
		`<ac:layout-cell><p>Col C.</p></ac:layout-cell>` +
		`</ac:layout-section>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, "┈┈ Column 1 ┈┈")
	assert.Contains(t, result, "┈┈ Column 2 ┈┈")
	assert.Contains(t, result, "┈┈ Column 3 ┈┈")
	assert.Equal(t, 1, log.Elements["layout"])
}

// ── Handler 4: Task lists ─────────────────────────────────────────────────────

func TestPreprocessNonMacroElements_TaskList(t *testing.T) {
	input := `<ac:task-list>` +
		`<ac:task>` +
		`<ac:task-id>1</ac:task-id>` +
		`<ac:task-status>complete</ac:task-status>` +
		`<ac:task-body><span class="placeholder-inline-tasks">Done item</span></ac:task-body>` +
		`</ac:task>` +
		`<ac:task>` +
		`<ac:task-id>2</ac:task-id>` +
		`<ac:task-status>incomplete</ac:task-status>` +
		`<ac:task-body><span class="placeholder-inline-tasks">Pending item</span></ac:task-body>` +
		`</ac:task>` +
		`</ac:task-list>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, `<ul>`)
	assert.Contains(t, result, `<li>[x] Done item</li>`)
	assert.Contains(t, result, `<li>[ ] Pending item</li>`)
	assert.NotContains(t, result, "ac:task")
	assert.NotContains(t, result, "<input")
	assert.Equal(t, 1, log.Elements["task_list"])
}

func TestPreprocessNonMacroElements_TaskList_InlineFormatting(t *testing.T) {
	input := `<ac:task-list>` +
		`<ac:task>` +
		`<ac:task-status>complete</ac:task-status>` +
		`<ac:task-body><strong>Important</strong> task</ac:task-body>` +
		`</ac:task>` +
		`</ac:task-list>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, `<strong>Important</strong>`)
	assert.Contains(t, result, "task")
	assert.Equal(t, 1, log.Elements["task_list"])
}

// End-to-end Markdown-level tests: task-list checkbox state survives the full
// ToMarkdown pipeline, including html-to-markdown conversion and
// postprocessMarkdown cleanup.

func TestTaskListCheckboxState(t *testing.T) {
	input := `<ac:task-list>` +
		`<ac:task><ac:task-status>incomplete</ac:task-status>` +
		`<ac:task-body>Uncompleted item</ac:task-body></ac:task>` +
		`<ac:task><ac:task-status>complete</ac:task-status>` +
		`<ac:task-body>Completed item</ac:task-body></ac:task>` +
		`</ac:task-list>`

	got := ToMarkdown(input)

	assert.Contains(t, got, "- [ ] Uncompleted item")
	assert.Contains(t, got, "- [x] Completed item")
}

func TestTaskListInlineFormatting(t *testing.T) {
	input := `<ac:task-list>` +
		`<ac:task><ac:task-status>incomplete</ac:task-status>` +
		`<ac:task-body>Task with <code>inline</code> code</ac:task-body></ac:task>` +
		`</ac:task-list>`

	got := ToMarkdown(input)

	assert.Contains(t, got, "- [ ] Task with `inline` code")
}

// ── Handler 5: Date nodes ─────────────────────────────────────────────────────

func TestPreprocessNonMacroElements_DateNode(t *testing.T) {
	t.Run("self-closing", func(t *testing.T) {
		input := `Before <time datetime="2024-01-15" /> after.`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		assert.Contains(t, result, "2024-01-15")
		assert.NotContains(t, result, "<time")
		assert.Equal(t, 1, log.Elements["date"])
	})

	t.Run("paired", func(t *testing.T) {
		input := `See <time datetime="2025-06-30"></time> for details.`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		assert.Contains(t, result, "2025-06-30")
		assert.NotContains(t, result, "<time")
		assert.Equal(t, 1, log.Elements["date"])
	})

	t.Run("multiple", func(t *testing.T) {
		input := `<time datetime="2024-01-01" /> and <time datetime="2024-12-31" />`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		assert.Contains(t, result, "2024-01-01")
		assert.Contains(t, result, "2024-12-31")
		assert.Equal(t, 2, log.Elements["date"])
	})
}

// Colspan annotations are emitted by rewriteTables/expandGrid (see table.go),
// not by preprocess, so the old colspan preprocessor was removed — it stripped
// the colspan attribute before the grid expander could honour it, yielding
// under-wide rows.

// ── Handler 6: User mentions ──────────────────────────────────────────────────

func TestPreprocessNonMacroElements_UserMention(t *testing.T) {
	input := `<p>hi <ac:link><ri:user ri:account-id="712020:abc" ri:local-id="1" /></ac:link> there</p>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, "@user(712020:abc)")
	assert.NotContains(t, result, "ri:user")
	assert.NotContains(t, result, "ac:link")
	assert.Equal(t, 1, log.Elements["user_mention"])
}

// ── Handler 7: Emoticons ──────────────────────────────────────────────────────

func TestPreprocessNonMacroElements_Emoticon(t *testing.T) {
	t.Run("fallback unicode", func(t *testing.T) {
		input := `<p>Hi <ac:emoticon ac:name="smile" ac:emoji-shortname=":slight_smile:" ac:emoji-id="1f642" ac:emoji-fallback="🙂" /> there</p>`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		assert.Contains(t, result, "🙂")
		assert.NotContains(t, result, "ac:emoticon")
		assert.Equal(t, 1, log.Elements["emoticon"])
	})

	t.Run("shortname mapped to unicode when fallback is a shortcode", func(t *testing.T) {
		input := `<p><ac:emoticon ac:name="warning" ac:emoji-shortname=":warning:" ac:emoji-fallback=":warning:" /></p>`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		// Known shortname → unicode glyph (not the raw `:warning:` literal).
		assert.Contains(t, result, "⚠️")
		assert.NotContains(t, result, ":warning:")
		assert.Equal(t, 1, log.Elements["emoticon"])
	})

	t.Run("unknown shortcode fallback passes through", func(t *testing.T) {
		input := `<p><ac:emoticon ac:name="custom" ac:emoji-fallback=":totally_new_icon:" /></p>`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		assert.Contains(t, result, ":totally_new_icon:")
		assert.Equal(t, 1, log.Elements["emoticon"])
	})
}

// ── Handler 8: Attachment images ──────────────────────────────────────────────

func TestPreprocessNonMacroElements_AttachmentImage(t *testing.T) {
	input := `<ac:image ac:alt="Diagram"><ri:attachment ri:filename="diagram.png" ri:version-at-save="1" /></ac:image>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, `src="attachment:diagram.png"`)
	assert.Contains(t, result, `alt="Diagram"`)
	assert.NotContains(t, result, "ac:image")
	assert.Equal(t, 1, log.Elements["image_attachment"])
}

// ── Handler 9: Anchor links ───────────────────────────────────────────────────

func TestPreprocessNonMacroElements_AnchorLink(t *testing.T) {
	input := `<p>See <ac:link ac:anchor="top"><ac:link-body>Jump to top</ac:link-body></ac:link></p>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	assert.Contains(t, result, `<a href="#top">Jump to top</a>`)
	assert.Equal(t, 1, log.Elements["anchor_link"])
}

// ── Handler 10: Subscript / superscript ───────────────────────────────────────

func TestPreprocessNonMacroElements_SubSup(t *testing.T) {
	input := `<p>H<sub>2</sub>O and X<sup>2</sup></p>`
	log := newLog()

	result := preprocessNonMacroElements(input, log, nil)

	// Tags are passed through to html-to-markdown, which emits them verbatim.
	assert.Contains(t, result, "<sub>2</sub>")
	assert.Contains(t, result, "<sup>2</sup>")
	assert.Equal(t, 1, log.Elements["subscript"])
	assert.Equal(t, 1, log.Elements["superscript"])
}

// ── Handler 11: HTML comment artifacts ───────────────────────────────────────

func TestPreprocessNonMacroElements_StripComments(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		input := `<p>Content</p><!--THE END-->`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		assert.NotContains(t, result, "<!--THE END-->")
		assert.Contains(t, result, "<p>Content</p>")
		assert.Equal(t, 1, log.Elements["comment_artifact"])
	})

	t.Run("multiple", func(t *testing.T) {
		input := `<!--THE END--><p>Middle</p><!--THE END-->`
		log := newLog()

		result := preprocessNonMacroElements(input, log, nil)

		assert.NotContains(t, result, "<!--THE END-->")
		assert.Contains(t, result, "Middle")
		assert.Equal(t, 2, log.Elements["comment_artifact"])
	})
}

// ── Nil log safety ────────────────────────────────────────────────────────────

func TestPreprocessNonMacroElements_NilLog_DoesNotPanic(t *testing.T) {
	input := `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro>` +
		`<time datetime="2024-01-01" />` +
		`<!--THE END-->`

	// Should not panic with nil log.
	assert.NotPanics(t, func() {
		_ = preprocessNonMacroElements(input, nil, nil)
	})
}
