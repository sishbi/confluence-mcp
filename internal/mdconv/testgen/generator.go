package testgen

import (
	"fmt"
	"math/rand"
	"strings"
)

// DocConfig controls the shape and content of a generated document.
type DocConfig struct {
	Seed        int64 // Seed for deterministic generation
	Sections    int   // Number of h2 sections to generate
	Complexity  int   // 1=basic, 2=+tables/code/links, 3=+panels/macros/images
	TargetBytes int   // If > 0, keep adding sections until doc reaches this size
}

// words is a list of ~120 technical words for text generation.
var words = []string{
	"adapter", "algorithm", "annotation", "api", "architecture", "array",
	"assertion", "authentication", "authorization", "backend", "batch",
	"benchmark", "binding", "buffer", "builder", "cache", "callback",
	"channel", "client", "cluster", "codec", "commit", "component",
	"concurrency", "config", "connector", "consumer", "container", "context",
	"controller", "convention", "converter", "credential", "daemon", "database",
	"deadline", "dependency", "deployment", "directive", "dispatcher", "domain",
	"driver", "endpoint", "entity", "environment", "error", "event",
	"exception", "executor", "extension", "factory", "filter", "format",
	"framework", "function", "gateway", "goroutine", "handler", "header",
	"heap", "hook", "hostname", "index", "injection", "interface",
	"iterator", "kernel", "library", "lifecycle", "listener", "logger",
	"manifest", "marshaller", "message", "metadata", "middleware", "migration",
	"module", "monitor", "mutex", "namespace", "network", "observer",
	"operator", "orchestrator", "parameter", "parser", "payload", "pipeline",
	"plugin", "pointer", "protocol", "provider", "proxy", "queue",
	"reactor", "receiver", "registry", "replica", "repository", "request",
	"resolver", "response", "retry", "router", "scheduler", "schema",
	"semaphore", "server", "service", "session", "signal", "snapshot",
	"socket", "strategy", "stream", "struct", "subscriber", "supervisor",
	"template", "timeout", "token", "transaction", "transformer", "validator",
	"version", "watcher", "webhook", "worker",
}

// headingNames is a list of ~30 section heading names.
var headingNames = []string{
	"Overview", "Architecture", "Configuration", "Installation", "Usage",
	"Authentication", "Authorization", "Data Model", "API Reference",
	"Error Handling", "Performance", "Security", "Testing", "Deployment",
	"Monitoring", "Troubleshooting", "Migration", "Examples", "Best Practices",
	"Limitations", "Dependencies", "Contributing", "Changelog", "Glossary",
	"Background", "Design Decisions", "Retry Logic", "Caching Strategy",
	"Rate Limiting", "Observability",
}

// firstNames and lastNames for generating person names.
var firstNames = []string{
	"Alice", "Bob", "Carol", "Dave", "Eve", "Frank", "Grace", "Henry",
	"Isabel", "Jack", "Karen", "Liam", "Maria", "Noah", "Olivia", "Paul",
}

var lastNames = []string{
	"Adams", "Baker", "Chen", "Davis", "Evans", "Foster", "Garcia", "Hall",
	"Iyer", "Jones", "Kim", "Lee", "Marsh", "Nguyen", "Patel", "Quinn",
}

// langs is a list of code languages for code blocks.
var langs = []string{"go", "java", "python", "bash", "json", "yaml", "sql"}

// GenerateStorageFormat produces a deterministic Confluence storage format XHTML
// document from the given config. Calling it twice with the same config always
// returns the same string.
func GenerateStorageFormat(cfg DocConfig) string {
	r := rand.New(rand.NewSource(cfg.Seed)) //nolint:gosec

	sections := cfg.Sections
	if sections <= 0 {
		sections = 3
	}
	complexity := cfg.Complexity
	if complexity <= 0 {
		complexity = 1
	}

	var sb strings.Builder

	// Document title (h1)
	title := generateTitle(r)
	fmt.Fprintf(&sb, "<h1>%s</h1>\n", title)
	fmt.Fprintf(&sb, "<p>%s</p>\n", generateSentence(r, 8, 15))

	usedHeadings := make(map[string]bool)

	for i := 0; i < sections; i++ {
		heading := pickUniqueHeading(r, usedHeadings)
		fmt.Fprintf(&sb, "<h2>%s</h2>\n", heading)
		writeSection(&sb, r, complexity)
	}

	// TargetBytes: keep adding sections until we reach the target size.
	if cfg.TargetBytes > 0 {
		for sb.Len() < cfg.TargetBytes {
			heading := pickUniqueHeading(r, usedHeadings)
			fmt.Fprintf(&sb, "<h2>%s</h2>\n", heading)
			writeSection(&sb, r, complexity)
		}
	}

	return sb.String()
}

// writeSection writes a section body at the given complexity level.
func writeSection(sb *strings.Builder, r *rand.Rand, complexity int) {
	// Level 1: paragraphs with bold/italic, bullet lists
	writeLevel1Section(sb, r)

	if complexity >= 2 {
		writeLevel2Section(sb, r)
	}
	if complexity >= 3 {
		writeLevel3Section(sb, r)
	}
}

// writeLevel1Section writes paragraphs with inline formatting and a bullet list.
func writeLevel1Section(sb *strings.Builder, r *rand.Rand) {
	// 2-3 paragraphs
	paraCount := 2 + r.Intn(2)
	for i := 0; i < paraCount; i++ {
		fmt.Fprintf(sb, "<p>%s</p>\n", generateParagraph(r))
	}

	// Bullet list (3-5 items)
	itemCount := 3 + r.Intn(3)
	sb.WriteString("<ul>\n")
	for i := 0; i < itemCount; i++ {
		fmt.Fprintf(sb, "<li>%s</li>\n", generateSentence(r, 4, 9))
	}
	sb.WriteString("</ul>\n")
}

// writeLevel2Section writes ordered lists, code blocks, links, and tables.
func writeLevel2Section(sb *strings.Builder, r *rand.Rand) {
	// Ordered list (2-4 items)
	itemCount := 2 + r.Intn(3)
	sb.WriteString("<ol>\n")
	for i := 0; i < itemCount; i++ {
		fmt.Fprintf(sb, "<li>%s</li>\n", generateSentence(r, 4, 9))
	}
	sb.WriteString("</ol>\n")

	// Code block
	lang := langs[r.Intn(len(langs))]
	fmt.Fprintf(sb, `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">%s</ac:parameter><ac:plain-text-body><![CDATA[%s]]></ac:plain-text-body></ac:structured-macro>`+"\n",
		lang, generateCodeBlock(r, lang))

	// Paragraph with a link
	linkText := generatePhrase(r, 2, 4)
	linkURL := fmt.Sprintf("https://example.com/%s", generateIdentifier(r))
	fmt.Fprintf(sb, "<p>For more information see <a href=\"%s\">%s</a> for details.</p>\n",
		linkURL, linkText)

	// Table (2-3 columns, 2-4 rows)
	colCount := 2 + r.Intn(2)
	rowCount := 2 + r.Intn(3)
	sb.WriteString("<table>\n<tbody>\n")
	// Header row
	sb.WriteString("<tr>\n")
	for c := 0; c < colCount; c++ {
		fmt.Fprintf(sb, "<th>%s</th>\n", generatePhrase(r, 1, 2))
	}
	sb.WriteString("</tr>\n")
	// Data rows
	for row := 0; row < rowCount; row++ {
		sb.WriteString("<tr>\n")
		for c := 0; c < colCount; c++ {
			fmt.Fprintf(sb, "<td>%s</td>\n", generatePhrase(r, 1, 3))
		}
		sb.WriteString("</tr>\n")
	}
	sb.WriteString("</tbody>\n</table>\n")
}

// writeLevel3Section writes info/warning panels, expand macros, images, user mentions, and status macros.
func writeLevel3Section(sb *strings.Builder, r *rand.Rand) {
	// Info/warning/note/tip panel
	panelTypes := []string{"info", "warning", "note", "tip"}
	panelType := panelTypes[r.Intn(len(panelTypes))]
	fmt.Fprintf(sb, `<ac:structured-macro ac:name="%s"><ac:rich-text-body><p>%s</p></ac:rich-text-body></ac:structured-macro>`+"\n",
		panelType, generateSentence(r, 6, 12))

	// Expand macro
	expandTitle := generatePhrase(r, 2, 4)
	fmt.Fprintf(sb, `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">%s</ac:parameter><ac:rich-text-body><p>%s</p></ac:rich-text-body></ac:structured-macro>`+"\n",
		expandTitle, generateSentence(r, 8, 14))

	// Image
	imageURL := fmt.Sprintf("https://example.com/images/%s.png", generateIdentifier(r))
	fmt.Fprintf(sb, `<ac:image><ri:url ri:value="%s" /></ac:image>`+"\n", imageURL)

	// User mention
	accountID := generateAccountID(r)
	name := generatePersonName(r)
	fmt.Fprintf(sb, `<p>Contact <ac:link><ri:user ri:account-id="%s" /><ac:plain-text-link-body>%s</ac:plain-text-link-body></ac:link> for details.</p>`+"\n",
		accountID, name)

	// Status macro
	statusColors := []string{"Green", "Blue", "Yellow", "Red", "Grey"}
	statusColor := statusColors[r.Intn(len(statusColors))]
	statusTitles := []string{"DONE", "IN PROGRESS", "TODO", "BLOCKED", "REVIEW"}
	statusTitle := statusTitles[r.Intn(len(statusTitles))]
	fmt.Fprintf(sb, `<ac:structured-macro ac:name="status"><ac:parameter ac:name="colour">%s</ac:parameter><ac:parameter ac:name="title">%s</ac:parameter></ac:structured-macro>`+"\n",
		statusColor, statusTitle)
}

// generateTitle returns a capitalised title phrase.
func generateTitle(r *rand.Rand) string {
	return generateSentence(r, 3, 6)
}

// generateSentence returns a phrase with the first word capitalised (no trailing punctuation).
func generateSentence(r *rand.Rand, minWords, maxWords int) string {
	n := minWords + r.Intn(maxWords-minWords+1)
	parts := make([]string, n)
	for i := range parts {
		w := words[r.Intn(len(words))]
		if i == 0 {
			w = strings.ToUpper(w[:1]) + w[1:]
		}
		parts[i] = w
	}
	return strings.Join(parts, " ")
}

// generatePhrase returns a lowercase multi-word phrase.
func generatePhrase(r *rand.Rand, minWords, maxWords int) string {
	n := minWords + r.Intn(maxWords-minWords+1)
	parts := make([]string, n)
	for i := range parts {
		parts[i] = words[r.Intn(len(words))]
	}
	return strings.Join(parts, " ")
}

// generateIdentifier returns two words joined with an underscore.
func generateIdentifier(r *rand.Rand) string {
	return words[r.Intn(len(words))] + "_" + words[r.Intn(len(words))]
}

// generateParagraph returns a paragraph with embedded bold and italic spans.
func generateParagraph(r *rand.Rand) string {
	var sb strings.Builder

	// Opening sentence
	sb.WriteString(generateSentence(r, 5, 10))
	sb.WriteString(". ")

	// Bold span
	fmt.Fprintf(&sb, "<strong>%s</strong>", generatePhrase(r, 2, 4))
	sb.WriteString(" ")
	sb.WriteString(generatePhrase(r, 3, 6))
	sb.WriteString(". ")

	// Italic span
	fmt.Fprintf(&sb, "<em>%s</em>", generatePhrase(r, 2, 4))
	sb.WriteString(" ")
	sb.WriteString(generatePhrase(r, 3, 7))
	sb.WriteString(".")

	return sb.String()
}

// generateCodeBlock returns pseudo-code lines appropriate for the given language.
func generateCodeBlock(r *rand.Rand, lang string) string {
	var lines []string
	lineCount := 3 + r.Intn(5)

	switch lang {
	case "go":
		lines = append(lines, fmt.Sprintf("func %s() error {", generateIdentifier(r)))
		for i := 0; i < lineCount; i++ {
			lines = append(lines, fmt.Sprintf("    %s := %s()", generateIdentifier(r), words[r.Intn(len(words))]))
		}
		lines = append(lines, "    return nil", "}")
	case "python":
		lines = append(lines, fmt.Sprintf("def %s():", generateIdentifier(r)))
		for i := 0; i < lineCount; i++ {
			lines = append(lines, fmt.Sprintf("    %s = %s()", generateIdentifier(r), words[r.Intn(len(words))]))
		}
		lines = append(lines, "    return None")
	case "bash":
		for i := 0; i < lineCount; i++ {
			lines = append(lines, fmt.Sprintf("%s --%s %s", words[r.Intn(len(words))], words[r.Intn(len(words))], words[r.Intn(len(words))]))
		}
	case "json":
		lines = append(lines, "{")
		for i := 0; i < lineCount; i++ {
			sep := ","
			if i == lineCount-1 {
				sep = ""
			}
			lines = append(lines, fmt.Sprintf(`  "%s": "%s"%s`, words[r.Intn(len(words))], words[r.Intn(len(words))], sep))
		}
		lines = append(lines, "}")
	case "yaml":
		for i := 0; i < lineCount; i++ {
			lines = append(lines, fmt.Sprintf("%s: %s", words[r.Intn(len(words))], words[r.Intn(len(words))]))
		}
	case "sql":
		table := generateIdentifier(r)
		col1 := words[r.Intn(len(words))]
		col2 := words[r.Intn(len(words))]
		lines = append(lines,
			fmt.Sprintf("SELECT %s, %s", col1, col2),
			fmt.Sprintf("FROM %s", table),
			fmt.Sprintf("WHERE %s = '%s'", col1, words[r.Intn(len(words))]),
			"LIMIT 100;",
		)
	default: // java, etc.
		lines = append(lines, fmt.Sprintf("public class %s {", generateIdentifier(r)))
		for i := 0; i < lineCount; i++ {
			lines = append(lines, fmt.Sprintf("    private %s %s;", words[r.Intn(len(words))], words[r.Intn(len(words))]))
		}
		lines = append(lines, "}")
	}

	return strings.Join(lines, "\n")
}

// generatePersonName returns a random full name from the name lists.
func generatePersonName(r *rand.Rand) string {
	return firstNames[r.Intn(len(firstNames))] + " " + lastNames[r.Intn(len(lastNames))]
}

// generateAccountID returns a fake Confluence account ID (format: 5xxxxxx:uuid-like).
func generateAccountID(r *rand.Rand) string {
	return fmt.Sprintf("%d%d%d%d%d%d%d:%08x-%04x-%04x-%04x-%012x",
		r.Intn(9)+1, r.Intn(10), r.Intn(10), r.Intn(10), r.Intn(10), r.Intn(10), r.Intn(10),
		r.Uint32(), r.Uint32()&0xffff, r.Uint32()&0xffff, r.Uint32()&0xffff, r.Uint64()&0xffffffffffff,
	)
}

// pickUniqueHeading picks a heading name not yet used in this document.
// Falls back to a numbered heading if all names are exhausted.
func pickUniqueHeading(r *rand.Rand, used map[string]bool) string {
	// Shuffle attempts to find an unused heading
	candidates := make([]string, len(headingNames))
	copy(candidates, headingNames)
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, h := range candidates {
		if !used[h] {
			used[h] = true
			return h
		}
	}
	// All headings used — generate a unique numbered one
	n := len(used) + 1
	h := fmt.Sprintf("Section %d", n)
	used[h] = true
	return h
}
