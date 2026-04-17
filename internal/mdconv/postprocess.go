package mdconv

import "regexp"

var (
	// reTheEndComment matches the list-terminator comments emitted by the
	// html-to-markdown library (any internal whitespace variant). The
	// stripped region extends to the next non-blank line so we don't leave
	// orphaned blank lines where the comment sat alone on a line.
	reTheEndComment = regexp.MustCompile(`(?m)^[ \t]*<!--\s*THE END\s*-->[ \t]*\n?`)

	// reBlankLineRun collapses any run of 3+ consecutive newlines down to 2
	// (i.e. one blank line between paragraphs). This compensates for gaps
	// left by stripped comments and for the library's occasional over-padding.
	reBlankLineRun = regexp.MustCompile(`\n{3,}`)

	// reEscapedTaskMarker un-escapes the `\[x]` / `\[ ]` task-list sentinels
	// that html-to-markdown produces because `[` is a Markdown metachar. The
	// preprocessor emits literal `[x]`/`[ ]` inside <li> precisely so GFM task
	// lists round-trip; this undoes the defensive escape.
	reEscapedTaskMarker = regexp.MustCompile(`(?m)^(\s*[-*]\s+)\\\[( |x)\] `)

	// reEscapedAlert un-escapes the `\[!TYPE]` token that html-to-markdown
	// emits for our panel placeholders. Emitting `<blockquote><p>[!NOTE]</p>…`
	// renders as `> \[!NOTE]` because `[` is a metachar; GFM alert syntax
	// requires the unescaped `[!NOTE]` form.
	reEscapedAlert = regexp.MustCompile(`\\\[!(NOTE|TIP|WARNING|CAUTION|IMPORTANT)\]`)
)

// postprocessMarkdown cleans up artifacts that the html-to-markdown library
// emits after conversion: the <!--THE END--> list terminators, runs of blank
// lines wider than the paragraph separator, and escaped task-list sentinels.
func postprocessMarkdown(md string) string {
	md = reTheEndComment.ReplaceAllString(md, "")
	md = reBlankLineRun.ReplaceAllString(md, "\n\n")
	md = reEscapedTaskMarker.ReplaceAllString(md, "$1[$2] ")
	md = reEscapedAlert.ReplaceAllString(md, "[!$1]")
	return md
}
