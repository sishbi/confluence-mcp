package confluencemcp

import (
	"fmt"
	"regexp"
)

// Confluence addresses a heading by its text in two places: the ac:anchor
// attribute of an in-page or cross-page ac:link, and the anchor macro's single
// unnamed parameter. Both break when the heading is renamed.
var (
	reLinkAnchor  = regexp.MustCompile(`<ac:link[^>]*\sac:anchor="([^"]*)"`)
	reAnchorMacro = regexp.MustCompile(`(?s)<ac:structured-macro[^>]*\sac:name="anchor"[^>]*>(.*?)</ac:structured-macro>`)
	reMacroParam  = regexp.MustCompile(`(?s)<ac:parameter[^>]*>(.*?)</ac:parameter>`)
)

// findAnchorReferences returns a description of every on-page anchor reference
// naming headingText. Advisory only: a rename reports these rather than
// rewriting them, because rewriting would edit the page outside the section the
// caller named — widening a replace_section past what its preview shows. It
// cannot see references on other pages.
func findAnchorReferences(body, headingText string) []string {
	want := normalizeHeading(headingText)
	var refs []string

	for _, m := range reLinkAnchor.FindAllStringSubmatch(body, -1) {
		if normalizeHeading(m[1]) == want {
			refs = append(refs, fmt.Sprintf(`ac:link ac:anchor=%q`, headingText))
		}
	}
	for _, m := range reAnchorMacro.FindAllStringSubmatch(body, -1) {
		for _, p := range reMacroParam.FindAllStringSubmatch(m[1], -1) {
			if normalizeHeading(extractText(p[1])) == want {
				refs = append(refs, fmt.Sprintf("anchor macro %q", headingText))
			}
		}
	}
	return refs
}
