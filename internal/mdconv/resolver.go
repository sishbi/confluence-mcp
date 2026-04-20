package mdconv

// Resolver pulls external references while converting storage format to
// Markdown. A nil Resolver falls back to the pure-text representation
// (`@user(accountId)` for mentions, `[Child pages]` for the children macro),
// which is what the standalone `ToMarkdownWithMacros` entry point uses.
//
// Implementations live outside this package — mdconv stays free of any
// Confluence API dependency. Callers (tool_read.go) construct a resolver from
// the Confluence client and pass it via ToMarkdownWithMacrosResolved.
type Resolver interface {
	// ResolveUser returns the display name for an account id. The second
	// return value is false when the lookup fails, in which case the caller
	// should fall back to the account id.
	ResolveUser(accountID string) (displayName string, ok bool)

	// ListChildren returns the immediate (or nested, if depth > 1) child
	// pages of the given parent page id. Implementations should cap depth
	// (Confluence itself caps the children endpoint) and cache results for
	// the duration of a single conversion. Returns a non-nil error on
	// lookup failure; callers fall back to the `[Child pages]` placeholder.
	ListChildren(parentPageID string, depth int) ([]ChildPage, error)
}

// ChildPage is a minimal view of a Confluence child page used to render the
// children macro as a nested bulleted list of Markdown links.
type ChildPage struct {
	ID       string
	Title    string
	URL      string
	Children []ChildPage
}
