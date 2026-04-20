// Command anonymise reads a Confluence page saved from Chrome ("Save Page As > Web Page, Complete")
// and produces an anonymised gzipped test fixture.
//
// Usage:
//
//	go run ./cmd/anonymise -input saved-page.html -output internal/mdconv/testdata/anonymised-example.json.gz -name "complex-page"
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"golang.org/x/net/html"

	"github.com/sishbi/confluence-mcp/internal/mdconv/testgen"
)

func main() {
	inputPath := flag.String("input", "", "path to Confluence page HTML (Chrome 'Save Page As')")
	outputPath := flag.String("output", "", "output path for gzipped fixture (.json.gz)")
	name := flag.String("name", "anonymous", "fixture name")
	flag.Parse()

	if *inputPath == "" || *outputPath == "" {
		fmt.Fprintln(os.Stderr, "usage: anonymise -input <page.html> -output <file.json.gz> [-name <name>]")
		os.Exit(1)
	}

	inputData, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading input: %v\n", err)
		os.Exit(1)
	}

	doc, err := html.Parse(strings.NewReader(string(inputData)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "parsing HTML: %v\n", err)
		os.Exit(1)
	}

	contentNode := findContentNode(doc)
	if contentNode == nil {
		fmt.Fprintln(os.Stderr, "warning: could not find Confluence content div, using entire <body>")
		contentNode = findBody(doc)
	}
	if contentNode == nil {
		fmt.Fprintln(os.Stderr, "error: no content found in HTML")
		os.Exit(1)
	}

	anonymiseNode(contentNode)

	var sb strings.Builder
	for c := contentNode.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&sb, c)
	}
	anonymised := sb.String()

	fp := testgen.FingerprintStorageFormat(anonymised)
	fmt.Fprintf(os.Stderr, "fingerprint: headings=%v codeBlocks=%d links=%d listItems=%d textLen=%d\n",
		fp.HeadingCount, fp.CodeBlockCount, fp.LinkCount, fp.ListItemCount, fp.TextLength)

	fixture := &testgen.Fixture{
		Name:    *name,
		Source:  "anonymised from Confluence page (Chrome Save Page As)",
		Content: anonymised,
	}

	if err := testgen.SaveFixture(*outputPath, fixture); err != nil {
		fmt.Fprintf(os.Stderr, "saving fixture: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "wrote anonymised fixture to %s (%d bytes)\n", *outputPath, len(anonymised))
}

func findContentNode(doc *html.Node) *html.Node {
	selectors := []struct {
		tag   string
		attr  string
		value string
	}{
		{"div", "id", "main-content"},
		{"div", "class", "wiki-content"},
		{"div", "id", "content-body"},
		{"article", "", ""},
	}

	for _, sel := range selectors {
		if node := findBySelector(doc, sel.tag, sel.attr, sel.value); node != nil {
			return node
		}
	}
	return nil
}

func findBySelector(n *html.Node, tag, attr, value string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		if attr == "" {
			return n
		}
		for _, a := range n.Attr {
			if a.Key == attr && (value == "" || strings.Contains(a.Val, value)) {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if result := findBySelector(c, tag, attr, value); result != nil {
			return result
		}
	}
	return nil
}

func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "body" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if result := findBody(c); result != nil {
			return result
		}
	}
	return nil
}

// preserveAttrs are structural attributes whose values should not be anonymised.
// Everything else (URLs, titles, alt text, data-* values) gets anonymised.
var preserveAttrs = map[string]bool{
	"class": true, "id": true, "type": true, "role": true,
	"rel": true, "target": true, "method": true, "name": true,
	"colspan": true, "rowspan": true, "scope": true, "dir": true,
	"lang": true, "charset": true, "media": true, "sizes": true,
	"width": true, "height": true, "tabindex": true, "disabled": true,
	"hidden": true, "open": true, "checked": true, "selected": true,
	"readonly": true, "required": true, "multiple": true,
	"autocomplete": true, "autofocus": true, "novalidate": true,
	"draggable": true, "contenteditable": true, "spellcheck": true,
}

func anonymiseNode(n *html.Node) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "code", "pre", "script", "style":
			return
		}
		// Anonymise all attribute values except structural ones
		for i := range n.Attr {
			if preserveAttrs[n.Attr[i].Key] || n.Attr[i].Val == "" {
				continue
			}
			val := n.Attr[i].Val
			if looksLikeURL(val) {
				n.Attr[i].Val = anonymiseURL(val)
			} else if len(strings.TrimSpace(val)) > 0 {
				n.Attr[i].Val = anonymiseText(val)
			}
		}
	}

	if n.Type == html.TextNode {
		trimmed := strings.TrimSpace(n.Data)
		if trimmed != "" {
			n.Data = anonymiseText(n.Data)
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		anonymiseNode(c)
	}
}

// looksLikeURL returns true if the value appears to be a URL.
func looksLikeURL(val string) bool {
	return strings.HasPrefix(val, "http://") ||
		strings.HasPrefix(val, "https://") ||
		strings.HasPrefix(val, "//") ||
		strings.HasPrefix(val, "/wiki/") ||
		strings.HasPrefix(val, "/download/")
}

// anonymiseURL replaces a URL with a deterministic fake URL.
// Preserves the scheme, replaces hostname and path segments.
func anonymiseURL(original string) string {
	if original == "" || original == "#" {
		return original
	}

	// Hash the original URL to get a deterministic seed
	h := sha256.Sum256([]byte(original))
	seed := int64(h[0])<<56 | int64(h[1])<<48 | int64(h[2])<<40 |
		int64(h[3])<<32 | int64(h[4])<<24 | int64(h[5])<<16 |
		int64(h[6])<<8 | int64(h[7])
	r := rand.New(rand.NewSource(seed))

	// Preserve scheme if present
	scheme := ""
	rest := original
	if idx := strings.Index(original, "://"); idx != -1 {
		scheme = original[:idx+3]
		rest = original[idx+3:]
	}

	// Generate a fake path with the same number of segments
	parts := strings.Split(rest, "/")
	for i := range parts {
		if parts[i] != "" {
			parts[i] = wordBank[r.Intn(len(wordBank))]
		}
	}

	if scheme != "" {
		return scheme + "example.com/" + strings.Join(parts, "/")
	}
	return strings.Join(parts, "/")
}

var wordBank = []string{
	"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
	"india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
	"quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray",
	"yankee", "zulu", "apex", "beacon", "cipher", "drift", "ember", "flux",
	"glyph", "haze", "ivory", "jade", "karma", "lumen", "nexus", "orbit",
	"prism", "quartz", "ridge", "spark", "trace", "unity", "vault", "weave",
}

func anonymiseText(original string) string {
	trimmed := strings.TrimSpace(original)
	if trimmed == "" {
		return original
	}

	h := sha256.Sum256([]byte(trimmed))
	seed := int64(h[0])<<56 | int64(h[1])<<48 | int64(h[2])<<40 |
		int64(h[3])<<32 | int64(h[4])<<24 | int64(h[5])<<16 |
		int64(h[6])<<8 | int64(h[7])
	r := rand.New(rand.NewSource(seed))

	leading := original[:len(original)-len(strings.TrimLeft(original, " \t\n\r"))]
	trailing := original[len(strings.TrimRight(original, " \t\n\r")):]

	words := strings.Fields(trimmed)
	replaced := make([]string, len(words))
	for i := range words {
		replaced[i] = wordBank[r.Intn(len(wordBank))]
	}

	if len(replaced) > 0 && len(words[0]) > 0 &&
		words[0][0] >= 'A' && words[0][0] <= 'Z' {
		replaced[0] = strings.ToUpper(replaced[0][:1]) + replaced[0][1:]
	}

	return leading + strings.Join(replaced, " ") + trailing
}
