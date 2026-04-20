// Package testgen provides deterministic document generation and content
// fingerprinting for verifying Markdown <-> Confluence storage format conversion fidelity.
package testgen

import (
	"crypto/sha256"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
)

// gmParser is a package-level goldmark parser for reuse across FingerprintMarkdown calls.
var gmParser = goldmark.New().Parser()

// ContentFingerprint captures the semantic structure of a document,
// enabling round-trip verification without byte-level equality.
type ContentFingerprint struct {
	TextHash       [32]byte                      // SHA256 of all text content concatenated
	HeadingCount   [6]int                        // count by level (h1=index 0 .. h6=index 5)
	CodeBlockCount int
	LinkCount      int
	ListItemCount  int
	BoldCount      int
	ItalicCount    int
	TextLength     int
	MacroCount     map[string]int                // macro name → count
	Sections       map[string]ContentFingerprint // per-section, keyed by heading text
}

// FingerprintStorageFormat extracts a ContentFingerprint from Confluence storage format XHTML.
func FingerprintStorageFormat(storageFormat string) ContentFingerprint {
	if storageFormat == "" {
		return ContentFingerprint{}
	}

	// Pre-process: replace ac: and ri: namespace prefixes so the HTML parser
	// treats them as regular elements (HTML5 parser doesn't handle XML namespaces).
	preprocessed := strings.NewReplacer(
		"ac:structured-macro", "ac-structured-macro",
		"ac:parameter", "ac-parameter",
		"ac:plain-text-body", "ac-plain-text-body",
		"ac:rich-text-body", "ac-rich-text-body",
		"ac:link", "ac-link",
		"ac:image", "ac-image",
		"ac:name=", "data-ac-name=",
		"ri:url", "ri-url",
		"ri:value=", "data-ri-value=",
		"ri:user", "ri-user",
		"ri:account-id=", "data-ri-account-id=",
	).Replace(storageFormat)

	doc, err := html.Parse(strings.NewReader(preprocessed))
	if err != nil {
		return ContentFingerprint{}
	}

	fp := ContentFingerprint{
		Sections:   make(map[string]ContentFingerprint),
		MacroCount: make(map[string]int),
	}
	var allText strings.Builder

	var currentSection string
	sectionTexts := make(map[string]*strings.Builder)
	sectionFPs := make(map[string]*ContentFingerprint)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := n.Data

			if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' {
				level := int(tag[1] - '1')
				fp.HeadingCount[level]++
				headingText := extractText(n)
				currentSection = headingText
				sectionTexts[currentSection] = &strings.Builder{}
				sfp := &ContentFingerprint{}
				sfp.HeadingCount[level]++
				sectionFPs[currentSection] = sfp
			}

			switch tag {
			case "strong", "b":
				fp.BoldCount++
				if sfp, ok := sectionFPs[currentSection]; ok {
					sfp.BoldCount++
				}
			case "em", "i":
				fp.ItalicCount++
				if sfp, ok := sectionFPs[currentSection]; ok {
					sfp.ItalicCount++
				}
			case "a":
				fp.LinkCount++
				if sfp, ok := sectionFPs[currentSection]; ok {
					sfp.LinkCount++
				}
			case "li":
				fp.ListItemCount++
				if sfp, ok := sectionFPs[currentSection]; ok {
					sfp.ListItemCount++
				}
			case "ac-structured-macro":
				for _, attr := range n.Attr {
					if attr.Key == "data-ac-name" {
						if attr.Val == "code" {
							fp.CodeBlockCount++
							if sfp, ok := sectionFPs[currentSection]; ok {
								sfp.CodeBlockCount++
							}
						}
						if fp.MacroCount == nil {
							fp.MacroCount = make(map[string]int)
						}
						fp.MacroCount[attr.Val]++
					}
				}
			}
		}

		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				allText.WriteString(text)
				allText.WriteString(" ")
				if sb, ok := sectionTexts[currentSection]; ok {
					sb.WriteString(text)
					sb.WriteString(" ")
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	fullText := strings.TrimSpace(allText.String())
	fp.TextLength = len(fullText)
	if fullText != "" {
		fp.TextHash = sha256.Sum256([]byte(fullText))
	}

	for name, sb := range sectionTexts {
		sfp := sectionFPs[name]
		text := strings.TrimSpace(sb.String())
		sfp.TextLength = len(text)
		if text != "" {
			sfp.TextHash = sha256.Sum256([]byte(text))
		}
		sfp.Sections = nil
		fp.Sections[name] = *sfp
	}

	return fp
}

// FingerprintMarkdown extracts a ContentFingerprint from a Markdown document.
func FingerprintMarkdown(md string) ContentFingerprint {
	if md == "" {
		return ContentFingerprint{}
	}

	src := []byte(md)
	reader := text.NewReader(src)
	doc := gmParser.Parse(reader)

	fp := ContentFingerprint{Sections: make(map[string]ContentFingerprint)}
	var allText strings.Builder

	var currentSection string
	sectionTexts := make(map[string]*strings.Builder)
	sectionFPs := make(map[string]*ContentFingerprint)

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node := n.(type) {
		case *ast.Heading:
			level := node.Level - 1 // Level is 1-based; index is 0-based
			fp.HeadingCount[level]++
			headingText := extractNodeText(node, src)
			currentSection = headingText
			sectionTexts[currentSection] = &strings.Builder{}
			sfp := &ContentFingerprint{}
			sfp.HeadingCount[level]++
			sectionFPs[currentSection] = sfp

		case *ast.Emphasis:
			if node.Level == 2 {
				fp.BoldCount++
				if sfp, ok := sectionFPs[currentSection]; ok {
					sfp.BoldCount++
				}
			} else {
				fp.ItalicCount++
				if sfp, ok := sectionFPs[currentSection]; ok {
					sfp.ItalicCount++
				}
			}

		case *ast.Link:
			fp.LinkCount++
			if sfp, ok := sectionFPs[currentSection]; ok {
				sfp.LinkCount++
			}

		case *ast.FencedCodeBlock:
			fp.CodeBlockCount++
			if sfp, ok := sectionFPs[currentSection]; ok {
				sfp.CodeBlockCount++
			}

		case *ast.ListItem:
			fp.ListItemCount++
			if sfp, ok := sectionFPs[currentSection]; ok {
				sfp.ListItemCount++
			}

		case *ast.Text:
			segment := node.Segment
			t := strings.TrimSpace(string(segment.Value(src)))
			if t != "" {
				allText.WriteString(t)
				allText.WriteString(" ")
				if sb, ok := sectionTexts[currentSection]; ok {
					sb.WriteString(t)
					sb.WriteString(" ")
				}
			}

		case *ast.String:
			t := strings.TrimSpace(string(node.Value))
			if t != "" {
				allText.WriteString(t)
				allText.WriteString(" ")
				if sb, ok := sectionTexts[currentSection]; ok {
					sb.WriteString(t)
					sb.WriteString(" ")
				}
			}
		}

		return ast.WalkContinue, nil
	})

	fullText := strings.TrimSpace(allText.String())
	fp.TextLength = len(fullText)
	if fullText != "" {
		fp.TextHash = sha256.Sum256([]byte(fullText))
	}

	for name, sb := range sectionTexts {
		sfp := sectionFPs[name]
		t := strings.TrimSpace(sb.String())
		sfp.TextLength = len(t)
		if t != "" {
			sfp.TextHash = sha256.Sum256([]byte(t))
		}
		sfp.Sections = nil
		fp.Sections[name] = *sfp
	}

	return fp
}

// extractNodeText returns the concatenated text content of a goldmark AST node.
func extractNodeText(n ast.Node, src []byte) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := child.(*ast.Text); ok {
			sb.Write(t.Segment.Value(src))
		}
		if s, ok := child.(*ast.String); ok {
			sb.Write(s.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(sb.String())
}

// extractText returns the concatenated text content of a node and its children.
func extractText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}
