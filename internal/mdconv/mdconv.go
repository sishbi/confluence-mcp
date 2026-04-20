// Package mdconv converts between Markdown and Confluence storage format (XHTML).
package mdconv

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	eastast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// md is a package-level goldmark instance with a custom Confluence storage format renderer.
// Priority 100 places the custom renderer ahead of extension renderers (priority 500)
// so our strikethrough handler emits <s> instead of extension.Strikethrough's <del>.
var md = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	goldmark.WithRenderer(
		renderer.NewRenderer(
			renderer.WithNodeRenderers(
				util.Prioritized(&confluenceRenderer{}, 100),
			),
		),
	),
)

// htmlToMdConverter is a shared html-to-markdown converter with strikethrough
// support and <u> passthrough. The converter is safe for concurrent use.
var htmlToMdConverter = func() *converter.Converter {
	c := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			strikethrough.NewStrikethroughPlugin(),
		),
	)
	// Markdown has no native underline, subscript, or superscript; keep these
	// tags as raw HTML so the semantics survive a round trip. Pandoc's `~x~`
	// and `^x^` syntax is not GFM and fails to render in most viewers.
	c.Register.RendererFor("u", converter.TagTypeInline, base.RenderAsHTML, converter.PriorityEarly)
	c.Register.RendererFor("sub", converter.TagTypeInline, base.RenderAsHTML, converter.PriorityEarly)
	c.Register.RendererFor("sup", converter.TagTypeInline, base.RenderAsHTML, converter.PriorityEarly)
	return c
}()

// ToStorageFormat converts a Markdown string to Confluence storage format (XHTML).
// Returns empty string if the input is empty.
func ToStorageFormat(markdown string) string {
	if markdown == "" {
		return ""
	}

	var buf bytes.Buffer
	source := []byte(markdown)
	doc := md.Parser().Parse(text.NewReader(source))
	if err := md.Renderer().Render(&buf, source, doc); err != nil {
		return ""
	}
	return buf.String()
}

// confluenceRenderer renders goldmark AST nodes as Confluence storage format XHTML.
type confluenceRenderer struct{}

func (r *confluenceRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// Block nodes
	reg.Register(ast.KindDocument, r.renderDocument)
	reg.Register(ast.KindParagraph, r.renderParagraph)
	reg.Register(ast.KindHeading, r.renderHeading)
	reg.Register(ast.KindList, r.renderList)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
	reg.Register(ast.KindThematicBreak, r.renderThematicBreak)

	// Inline nodes
	reg.Register(ast.KindText, r.renderText)
	reg.Register(ast.KindEmphasis, r.renderEmphasis)
	reg.Register(ast.KindCodeSpan, r.renderCodeSpan)
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindAutoLink, r.renderAutoLink)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindRawHTML, r.renderRawHTML)
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
	reg.Register(eastast.KindStrikethrough, r.renderStrikethrough)

	// Table extension nodes are handled by goldmark's built-in table extension
	// renderer (registered via extension.Table), which takes priority over custom
	// renderers. No registration needed here.
}

func (r *confluenceRenderer) renderDocument(w util.BufWriter, _ []byte, _ ast.Node, _ bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderParagraph(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	// Suppress <p> wrapper inside tight list items.
	if node.Parent() != nil && node.Parent().Kind() == ast.KindListItem {
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = w.WriteString("<p>")
	} else {
		_, _ = w.WriteString("</p>\n")
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderHeading(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	if entering {
		_, _ = fmt.Fprintf(w, "<h%d>", n.Level)
	} else {
		_, _ = fmt.Fprintf(w, "</h%d>\n", n.Level)
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderList(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.List)
	tag := "ul"
	if n.IsOrdered() {
		tag = "ol"
	}
	if entering {
		_, _ = fmt.Fprintf(w, "<%s>\n", tag)
	} else {
		_, _ = fmt.Fprintf(w, "</%s>\n", tag)
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderListItem(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<li>")
	} else {
		_, _ = w.WriteString("</li>\n")
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	_, _ = w.WriteString(`<ac:structured-macro ac:name="code">`)
	lang := string(n.Language(source))
	if lang != "" {
		_, _ = fmt.Fprintf(w, `<ac:parameter ac:name="language">%s</ac:parameter>`, lang)
	}
	_, _ = w.WriteString(`<ac:plain-text-body><![CDATA[`)
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = w.Write(line.Value(source))
	}
	_, _ = w.WriteString(`]]></ac:plain-text-body>`)
	_, _ = w.WriteString("</ac:structured-macro>\n")
	return ast.WalkSkipChildren, nil
}

func (r *confluenceRenderer) renderCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.CodeBlock)
	_, _ = w.WriteString(`<ac:structured-macro ac:name="code">`)
	_, _ = w.WriteString(`<ac:plain-text-body><![CDATA[`)
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = w.Write(line.Value(source))
	}
	_, _ = w.WriteString(`]]></ac:plain-text-body>`)
	_, _ = w.WriteString("</ac:structured-macro>\n")
	return ast.WalkSkipChildren, nil
}

func (r *confluenceRenderer) renderBlockquote(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<blockquote>\n")
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderThematicBreak(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<hr />\n")
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	value := n.Segment.Value(source)
	// Strip Markdown backslash escapes (e.g. \[ → [, \\ → \).
	// Without this, escape characters accumulate on each round-trip.
	if n.IsRaw() {
		_, _ = w.Write(value)
	} else {
		r.writeEscapedText(w, value)
	}
	if n.SoftLineBreak() {
		_ = w.WriteByte('\n')
	}
	if n.HardLineBreak() {
		_, _ = w.WriteString("<br />\n")
	}
	return ast.WalkContinue, nil
}

// writeEscapedText writes text content, stripping Markdown backslash escapes
// and HTML-escaping special characters.
func (r *confluenceRenderer) writeEscapedText(w util.BufWriter, value []byte) {
	i := 0
	for i < len(value) {
		if value[i] == '\\' && i+1 < len(value) && isMarkdownPunctuation(value[i+1]) {
			// Skip the backslash, write the escaped character
			i++
		}
		switch value[i] {
		case '&':
			_, _ = w.WriteString("&amp;")
		case '<':
			_, _ = w.WriteString("&lt;")
		case '>':
			_, _ = w.WriteString("&gt;")
		case '"':
			_, _ = w.WriteString("&quot;")
		default:
			_ = w.WriteByte(value[i])
		}
		i++
	}
}

// isMarkdownPunctuation returns true if the byte is an ASCII punctuation
// character that can be backslash-escaped in Markdown.
func isMarkdownPunctuation(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') ||
		(c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}

func (r *confluenceRenderer) renderEmphasis(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Emphasis)
	tag := "em"
	if n.Level == 2 {
		tag = "strong"
	}
	if entering {
		_, _ = fmt.Fprintf(w, "<%s>", tag)
	} else {
		_, _ = fmt.Fprintf(w, "</%s>", tag)
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderCodeSpan(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<code>")
	} else {
		_, _ = w.WriteString("</code>")
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderStrikethrough(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<s>")
	} else {
		_, _ = w.WriteString("</s>")
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderLink(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if entering {
		_, _ = fmt.Fprintf(w, `<a href="%s">`, string(n.Destination))
	} else {
		_, _ = w.WriteString("</a>")
	}
	return ast.WalkContinue, nil
}

func (r *confluenceRenderer) renderAutoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.AutoLink)
	url := string(n.URL(source))
	_, _ = fmt.Fprintf(w, `<a href="%s">%s</a>`, url, url)
	return ast.WalkSkipChildren, nil
}

func (r *confluenceRenderer) renderImage(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	_, _ = w.WriteString(`<ac:image>`)
	_, _ = fmt.Fprintf(w, `<ri:url ri:value="%s" />`, string(n.Destination))
	_, _ = w.WriteString(`</ac:image>`)
	return ast.WalkSkipChildren, nil
}

func (r *confluenceRenderer) renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.RawHTML)
	for i := 0; i < n.Segments.Len(); i++ {
		segment := n.Segments.At(i)
		_, _ = w.Write(segment.Value(source))
	}
	return ast.WalkSkipChildren, nil
}

func (r *confluenceRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.HTMLBlock)
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = w.Write(line.Value(source))
	}
	if n.HasClosure() {
		cl := n.ClosureLine
		_, _ = w.Write(cl.Value(source))
	}
	return ast.WalkSkipChildren, nil
}

// Compiled regexps for preprocessConfluenceXML.
// Note: code macro regexes (reCodeMacroWithLang, reCodeMacroNoLang) moved to
// preprocess.go — they now run in Pass 0 before extractMacros.
var (
	// Any remaining ac:structured-macro (unknown macros): strip tags, keep inner text.
	reUnknownMacro = regexp.MustCompile(`(?s)<ac:structured-macro[^>]*>.*?</ac:structured-macro>`)
	// ac:image with ri:url: convert to <img>.
	reACImage = regexp.MustCompile(`(?s)<ac:image[^>]*>.*?<ri:url[^>]*ri:value="([^"]*)"[^>]*/?>.*?</ac:image>`)
	// ac:link with ac:plain-text-link-body: keep the link body text.
	reACLink = regexp.MustCompile(`(?s)<ac:link[^>]*>.*?<ac:plain-text-link-body>(.*?)</ac:plain-text-link-body>.*?</ac:link>`)
	// Any remaining ac: or ri: tags: strip tags, keep inner text.
	reACTags = regexp.MustCompile(`</?(?:ac|ri):[^>]*/?>`)
)

// preprocessConfluenceXML transforms Confluence-specific XML elements into
// standard HTML that html-to-markdown can handle.
// Note: code macros are handled in Pass 0 (preprocessNonMacroElements) before
// this function runs, so they will never reach the unknown-macro branch here.
func preprocessConfluenceXML(s string) string {
	// 1. Unknown macros: strip tags, keep any visible text content.
	s = reUnknownMacro.ReplaceAllStringFunc(s, func(match string) string {
		// Strip all XML tags within the macro, leaving only text nodes.
		inner := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(match, "")
		return strings.TrimSpace(inner)
	})

	// 2. ac:image → <img>.
	s = reACImage.ReplaceAllString(s, `<img src="$1" />`)

	// 3. ac:link with plain-text-link-body → just the text.
	s = reACLink.ReplaceAllString(s, `$1`)

	// 4. Strip any remaining ac:/ri: tags (self-closing or paired).
	s = reACTags.ReplaceAllString(s, "")

	return s
}

// ToStorageFormatWithMacros converts Markdown to Confluence storage format,
// restoring macro XML from the registry for segments annotated with
// <!-- macro:mN --> comments. When registry is nil or the Markdown contains no
// macro comments, falls back to plain ToStorageFormat.
func ToStorageFormatWithMacros(markdown string, registry *MacroRegistry) string {
	if registry == nil || !reMacroComment.MatchString(markdown) {
		return ToStorageFormat(markdown)
	}

	segments := segmentMarkdown(markdown)
	var buf strings.Builder

	for _, seg := range segments {
		if seg.Type == "plain" {
			buf.WriteString(ToStorageFormat(seg.Content))
		} else {
			entry := registry.Lookup(seg.MacroID)
			if entry == nil {
				buf.WriteString(ToStorageFormat(seg.Content))
			} else {
				buf.WriteString(restoreMacro(entry, seg.Content))
			}
		}
	}

	return buf.String()
}

// ToMarkdown converts a Confluence storage format (XHTML) string to Markdown.
// Returns empty string if the input is empty.
func ToMarkdown(storageFormat string) string {
	if storageFormat == "" {
		return ""
	}

	preprocessed := preprocessNonMacroElements(storageFormat, nil, nil)
	preprocessed = preprocessConfluenceXML(preprocessed)
	preprocessed, tables, err := rewriteTables(preprocessed, nil)
	if err != nil {
		return preprocessed
	}

	result, err := htmlToMdConverter.ConvertString(preprocessed)
	if err != nil {
		return preprocessed
	}
	result = restoreTables(result, tables)
	return postprocessMarkdown(result)
}

// ToMarkdownWithMacros converts a Confluence storage format (XHTML) string to
// Markdown, extracting macro metadata into a MacroRegistry. Each macro is
// replaced with a human-readable representation and annotated with a
// <!-- macro:mN --> comment. Returns nil registry when no macros were found.
// The third return value is a ConversionLog with structural metrics; it is nil
// when storageFormat is empty.
//
// This entry point does not consult a Resolver; user mentions stay as
// `@user(accountId)` and the children macro stays as `[Child pages]`. Use
// ToMarkdownWithMacrosResolved to plug in external lookups.
func ToMarkdownWithMacros(storageFormat string) (string, *MacroRegistry, *ConversionLog) {
	return ToMarkdownWithMacrosResolved(storageFormat, nil)
}

// ToMarkdownWithMacrosResolved is the resolver-aware variant of
// ToMarkdownWithMacros. When resolver is non-nil, user mentions are expanded
// to `@DisplayName [accountId]` and (future) the children macro expands to a
// nested Markdown list. nil resolver is equivalent to ToMarkdownWithMacros.
func ToMarkdownWithMacrosResolved(storageFormat string, resolver Resolver) (string, *MacroRegistry, *ConversionLog) {
	if storageFormat == "" {
		return "", nil, nil
	}

	log := NewConversionLog()
	log.InputBytes = len(storageFormat)

	// Pass 0a: Register ADF panels as synthetic macros so write-back can
	// reconstruct the original ac:adf-extension XML. Runs before
	// preprocessNonMacroElements so the standalone handleADFPanels call in
	// that function becomes a no-op.
	counter := 0
	adfPreprocessed, adfRegistry := handleADFPanelsAsMacros(storageFormat, log, &counter)

	// Pass 0b: Remaining non-macro preprocessing (code, layouts, tasks, mentions, etc.).
	preprocessed := preprocessNonMacroElements(adfPreprocessed, log, resolver)

	// Pass 1: Extract macros BEFORE other preprocessing. Counter continues
	// from the ADF pass so macro IDs remain globally unique.
	preprocessed, macroRegistry := extractMacrosInto(preprocessed, log, &counter, resolver)
	registry := mergeRegistries(adfRegistry, macroRegistry)

	// Pass 2: Standard preprocessing (code macros, images, links, remaining ac: tags).
	preprocessed = preprocessConfluenceXML(preprocessed)

	// Pass 2b: Extract tables — html-to-markdown does not emit Markdown tables
	// natively, so we render them ourselves and swap in placeholders.
	preprocessed, tables, err := rewriteTables(preprocessed, log)
	if err != nil {
		return preprocessed, registry, log
	}

	// Pass 3: HTML-to-Markdown conversion.
	result, err := htmlToMdConverter.ConvertString(preprocessed)
	if err != nil {
		result = preprocessed
	}

	// Pass 3a: Restore table placeholders with their Markdown renderings.
	result = restoreTables(result, tables)

	// Pass 3b: Strip library artifacts (<!--THE END--> terminators, excess blank lines).
	result = postprocessMarkdown(result)

	// Pass 4: Insert <!-- macro:mN --> comments.
	if registry != nil {
		result = insertMacroComments(result, registry)
		// Pass 4a: Renumber IDs so they appear in document order. The pipeline
		// assigns IDs in processing order (ADF panels first, then depth-first
		// through nested macros), which leaves the rendered Markdown with
		// non-monotonic IDs. Renumbering keeps the Markdown readable while
		// preserving the registry/comment ID invariant.
		result, registry = renumberMacroMarkers(result, registry)
	}

	log.OutputBytes = len(result)

	return result, registry, log
}
