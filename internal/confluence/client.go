// Package confluence wraps Confluence Cloud REST API calls with retry logic.
package confluence

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config holds the configuration for the Confluence client.
type Config struct {
	URL        string
	Email      string
	APIToken   string
	MaxRetries int
	BaseDelay  time.Duration
	Logger     *slog.Logger
}

// Client is a Confluence REST API client with retry logic.
type Client struct {
	http    http.Client
	baseURL string
	auth    string
	cfg     Config
	log     *slog.Logger
}

// New creates a new Confluence client from the given config.
func New(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("confluence URL is required")
	}
	if cfg.Email == "" || cfg.APIToken == "" {
		return nil, fmt.Errorf("confluence email and API token are required")
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	raw := cfg.Email + ":" + cfg.APIToken
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))

	return &Client{
		http:    http.Client{Timeout: 30 * time.Second},
		baseURL: strings.TrimRight(cfg.URL, "/"),
		auth:    auth,
		cfg:     cfg,
		log:     logger.With("component", "confluence"),
	}, nil
}

// doRequest builds and executes an HTTP request.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	reqURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return c.http.Do(req)
}

// shouldRetry reports whether the given HTTP status code warrants a retry.
func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusBadGateway ||
		statusCode == http.StatusServiceUnavailable
}

// backoff returns the duration to wait before the next retry attempt.
// It respects the Retry-After header when present, otherwise uses exponential backoff.
func (c *Client) backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				return time.Duration(secs) * time.Second
			}
		}
	}
	// Exponential backoff: BaseDelay * 2^attempt
	d := c.cfg.BaseDelay
	for i := 0; i < attempt; i++ {
		d *= 2
	}
	return d
}

// retry executes fn up to MaxRetries+1 times, retrying on retryable status codes.
func (c *Client) retry(ctx context.Context, fn func() (*http.Response, error)) (*http.Response, error) {
	maxAttempts := c.cfg.MaxRetries + 1
	var (
		resp *http.Response
		err  error
	)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = fn()
		if err != nil {
			return nil, err
		}
		if !shouldRetry(resp.StatusCode) {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if attempt < maxAttempts-1 {
			delay := c.backoff(attempt, resp)
			c.log.WarnContext(ctx, "retry",
				"status", resp.StatusCode,
				"attempt", attempt+1,
				"delay", delay.String(),
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	// All attempts exhausted; return the last retryable response as an error.
	return nil, fmt.Errorf("request failed after %d attempts: status %d", maxAttempts, resp.StatusCode)
}

// doJSON performs an HTTP request with retry and JSON decoding.
// target may be nil for requests that return no body (e.g. DELETE).
func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, target any) error {
	c.log.DebugContext(ctx, "http_request",
		"method", method,
		"path", path,
	)
	resp, err := c.retry(ctx, func() (*http.Response, error) {
		return c.doRequest(ctx, method, path, body)
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	c.log.DebugContext(ctx, "http_response",
		"method", method,
		"path", path,
		"status", resp.StatusCode,
	)

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		body := strings.TrimSpace(string(b))
		c.log.ErrorContext(ctx, "confluence_api_error",
			"method", method,
			"path", path,
			"status", resp.StatusCode,
			"body", body,
		)
		return &APIError{StatusCode: resp.StatusCode, Body: body}
	}

	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// buildQuery constructs a URL query string from ListOptions.
func buildQuery(opts *ListOptions) string {
	if opts == nil {
		return ""
	}
	var parts []string
	if opts.Limit > 0 {
		parts = append(parts, "limit="+strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		// Cursor arrives pre-encoded from _links.next (see extractCursor);
		// re-escaping it here would double-encode and break pagination.
		parts = append(parts, "cursor="+opts.Cursor)
	}
	if opts.BodyFormat != "" {
		parts = append(parts, "body-format="+url.QueryEscape(opts.BodyFormat))
	}
	for _, status := range opts.ResolutionStatus {
		parts = append(parts, "resolution-status="+url.QueryEscape(status))
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

// jsonBody marshals v to JSON and returns it as an io.Reader.
func jsonBody(v any) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding JSON body: %w", err)
	}
	return bytes.NewReader(b), nil
}

// extractCursor pulls the bare cursor token out of a relative "_links.next"
// URL, e.g. "/wiki/api/v2/spaces?cursor=abc&limit=10" -> "abc". Returns ""
// when next is empty or has no cursor param.
func extractCursor(next string) string {
	if next == "" {
		return ""
	}
	idx := strings.Index(next, "?")
	if idx < 0 {
		return ""
	}
	query := next[idx+1:]
	for _, pair := range strings.Split(query, "&") {
		key, value, found := strings.Cut(pair, "=")
		if found && key == "cursor" {
			return value
		}
	}
	return ""
}

// listPaginated performs a GET against path with opts applied as a query
// string, decodes a PaginatedResponse[T], and returns the results plus the
// bare cursor token extracted from "_links.next" (never the raw relative
// URL). Package-level because Go does not allow type parameters on methods.
func listPaginated[T any](ctx context.Context, c *Client, path string, opts *ListOptions) ([]T, string, error) {
	fullPath := path + buildQuery(opts)
	var result PaginatedResponse[T]
	if err := c.doJSON(ctx, http.MethodGet, fullPath, nil, &result); err != nil {
		return nil, "", err
	}
	return result.Results, extractCursor(result.Links.Next), nil
}

// withBodyFormatStorage returns a copy of opts with BodyFormat defaulted to
// "storage" when the caller left it empty. It never mutates the caller's
// ListOptions.
func withBodyFormatStorage(opts *ListOptions) *ListOptions {
	var o ListOptions
	if opts != nil {
		o = *opts
	}
	if o.BodyFormat == "" {
		o.BodyFormat = "storage"
	}
	return &o
}

// GetSpaces returns a page of spaces.
// Returns (spaces, nextCursor, error).
func (c *Client) GetSpaces(ctx context.Context, opts *ListOptions) ([]Space, string, error) {
	return listPaginated[Space](ctx, c, "/wiki/api/v2/spaces", opts)
}

// BaseURL returns the Confluence host URL (without trailing slash) that the
// client was configured with.
func (c *Client) BaseURL() string { return c.baseURL }

// GetCurrentUser returns the currently authenticated user.
// Uses the v1 API — there is no v2 equivalent for the current user endpoint.
func (c *Client) GetCurrentUser(ctx context.Context) (*User, error) {
	var user User
	err := c.doJSON(ctx, http.MethodGet, "/wiki/rest/api/user/current", nil, &user)
	return &user, err
}

// GetUser returns a user by account ID. Uses the v1 API — the v2 users
// endpoint does not expose display names.
func (c *Client) GetUser(ctx context.Context, accountID string) (*User, error) {
	var user User
	path := "/wiki/rest/api/user?accountId=" + url.QueryEscape(accountID)
	err := c.doJSON(ctx, http.MethodGet, path, nil, &user)
	return &user, err
}

// GetPage returns a single page by ID, including its storage-format body.
func (c *Client) GetPage(ctx context.Context, id string) (*Page, error) {
	var page Page
	err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/wiki/api/v2/pages/%s?body-format=storage", id), nil, &page)
	return &page, err
}

// GetPageChildren returns the child pages of a page.
// Returns (children, nextCursor, error).
//
// Known follow-up: unlike other List* methods, nextCursor here is the raw
// relative "_links.next" URL, not the extracted cursor token (see extractCursor).
func (c *Client) GetPageChildren(ctx context.Context, id string, opts *ListOptions) ([]Page, string, error) {
	path := fmt.Sprintf("/wiki/api/v2/pages/%s/children", id) + buildQuery(opts)
	var result PaginatedResponse[Page]
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, "", err
	}
	return result.Results, result.Links.Next, nil
}

// CreatePage creates a new page from the given payload.
func (c *Client) CreatePage(ctx context.Context, payload map[string]any) (*Page, error) {
	body, err := jsonBody(payload)
	if err != nil {
		return nil, err
	}
	var page Page
	if err := c.doJSON(ctx, http.MethodPost, "/wiki/api/v2/pages", body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// UpdatePage updates an existing page by ID.
func (c *Client) UpdatePage(ctx context.Context, id string, payload map[string]any) (*Page, error) {
	body, err := jsonBody(payload)
	if err != nil {
		return nil, err
	}
	var page Page
	if err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/wiki/api/v2/pages/%s", id), body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// DeletePage deletes a page by ID.
func (c *Client) DeletePage(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/wiki/api/v2/pages/%s", id), nil, nil)
}

// SearchContent searches Confluence using CQL (v1 API).
func (c *Client) SearchContent(ctx context.Context, cql string, opts *ListOptions) (*SearchResult, error) {
	path := "/wiki/rest/api/search?cql=" + url.QueryEscape(cql)
	if opts != nil {
		if opts.Limit > 0 {
			path += "&limit=" + strconv.Itoa(opts.Limit)
		}
		if opts.Cursor != "" {
			path += "&cursor=" + opts.Cursor
		}
	}
	var result SearchResult
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPageFooterComments returns the footer comments for a page.
// Returns (comments, nextCursor, error).
func (c *Client) GetPageFooterComments(ctx context.Context, pageID string, opts *ListOptions) ([]Comment, string, error) {
	path := fmt.Sprintf("/wiki/api/v2/pages/%s/footer-comments", pageID)
	return listPaginated[Comment](ctx, c, path, withBodyFormatStorage(opts))
}

// GetPageInlineComments returns the inline comments for a page.
// Returns (comments, nextCursor, error).
func (c *Client) GetPageInlineComments(ctx context.Context, pageID string, opts *ListOptions) ([]InlineComment, string, error) {
	path := fmt.Sprintf("/wiki/api/v2/pages/%s/inline-comments", pageID)
	return listPaginated[InlineComment](ctx, c, path, withBodyFormatStorage(opts))
}

// GetFooterCommentChildren returns the reply children of a footer comment.
// The children endpoints accept no resolution-status filter, so any
// ResolutionStatus set on opts is dropped before the request.
// Returns (children, nextCursor, error).
func (c *Client) GetFooterCommentChildren(ctx context.Context, commentID string, opts *ListOptions) ([]Comment, string, error) {
	o := withBodyFormatStorage(opts)
	o.ResolutionStatus = nil
	path := fmt.Sprintf("/wiki/api/v2/footer-comments/%s/children", commentID)
	return listPaginated[Comment](ctx, c, path, o)
}

// GetInlineCommentChildren returns the reply children of an inline comment.
// The children endpoints accept no resolution-status filter, so any
// ResolutionStatus set on opts is dropped before the request.
// Returns (children, nextCursor, error).
func (c *Client) GetInlineCommentChildren(ctx context.Context, commentID string, opts *ListOptions) ([]InlineComment, string, error) {
	o := withBodyFormatStorage(opts)
	o.ResolutionStatus = nil
	path := fmt.Sprintf("/wiki/api/v2/inline-comments/%s/children", commentID)
	return listPaginated[InlineComment](ctx, c, path, o)
}

// GetFooterComment returns a single footer comment by ID, including its storage-format body.
func (c *Client) GetFooterComment(ctx context.Context, commentID string) (*Comment, error) {
	var comment Comment
	err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/wiki/api/v2/footer-comments/%s?body-format=storage", commentID), nil, &comment)
	return &comment, err
}

// GetInlineComment returns a single inline comment by ID, including its storage-format body.
func (c *Client) GetInlineComment(ctx context.Context, commentID string) (*InlineComment, error) {
	var comment InlineComment
	err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/wiki/api/v2/inline-comments/%s?body-format=storage", commentID), nil, &comment)
	return &comment, err
}

// createComment POSTs a new footer or inline comment to endpoint. The
// payload carries exactly one identity key (identKey/identValue) — "pageId"
// for a top-level comment, "parentCommentId" for a reply — since Confluence
// rejects a payload carrying both.
func (c *Client) createComment(ctx context.Context, endpoint, identKey, identValue, storageBody string, out any) error {
	payload := map[string]any{
		identKey: identValue,
		"body": map[string]any{
			"representation": "storage",
			"value":          storageBody,
		},
	}
	body, err := jsonBody(payload)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, endpoint, body, out)
}

// AddComment adds a top-level footer comment to a page.
func (c *Client) AddComment(ctx context.Context, pageID string, storageBody string) (*Comment, error) {
	var comment Comment
	if err := c.createComment(ctx, "/wiki/api/v2/footer-comments", "pageId", pageID, storageBody, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// AddFooterCommentReply adds a reply to an existing footer comment; see createComment for why pageId is not sent.
func (c *Client) AddFooterCommentReply(ctx context.Context, parentCommentID string, storageBody string) (*Comment, error) {
	var comment Comment
	if err := c.createComment(ctx, "/wiki/api/v2/footer-comments", "parentCommentId", parentCommentID, storageBody, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// AddInlineCommentReply adds a reply to an existing inline comment; see createComment for why pageId is not sent.
func (c *Client) AddInlineCommentReply(ctx context.Context, parentCommentID string, storageBody string) (*InlineComment, error) {
	var comment InlineComment
	if err := c.createComment(ctx, "/wiki/api/v2/inline-comments", "parentCommentId", parentCommentID, storageBody, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// UpdateComment updates a footer comment by ID.
func (c *Client) UpdateComment(ctx context.Context, commentID string, storageBody string, versionNumber int) (*Comment, error) {
	payload := map[string]any{
		"version": map[string]any{"number": versionNumber},
		"body": map[string]any{
			"representation": "storage",
			"value":          storageBody,
		},
	}
	body, err := jsonBody(payload)
	if err != nil {
		return nil, err
	}
	var comment Comment
	if err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/wiki/api/v2/footer-comments/%s", commentID), body, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// GetPageLabels returns the labels for a page.
// Returns (labels, nextCursor, error).
//
// Known follow-up: unlike other List* methods, nextCursor here is the raw
// relative "_links.next" URL, not the extracted cursor token (see extractCursor).
func (c *Client) GetPageLabels(ctx context.Context, pageID string, opts *ListOptions) ([]Label, string, error) {
	path := fmt.Sprintf("/wiki/api/v2/pages/%s/labels", pageID) + buildQuery(opts)
	var result PaginatedResponse[Label]
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, "", err
	}
	return result.Results, result.Links.Next, nil
}

// AddPageLabel adds a label to a page.
// Uses the v1 API — v2 does not support POST on /pages/{id}/labels.
func (c *Client) AddPageLabel(ctx context.Context, pageID string, label string) (*Label, error) {
	// v1 API expects an array of label objects
	payload := []map[string]any{{
		"name":   label,
		"prefix": "global",
	}}
	body, err := jsonBody(payload)
	if err != nil {
		return nil, err
	}
	// v1 returns a paginated result with all labels on the page
	var result struct {
		Results []Label `json:"results"`
	}
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/wiki/rest/api/content/%s/label", pageID), body, &result); err != nil {
		return nil, err
	}
	for _, l := range result.Results {
		if l.Name == label {
			return &l, nil
		}
	}
	if len(result.Results) > 0 {
		return &result.Results[0], nil
	}
	return &Label{Name: label}, nil
}

// RemovePageLabel removes a label from a page by label name.
// Uses the v1 API — v2 DELETE on /pages/{id}/labels/{id} returns 404.
func (c *Client) RemovePageLabel(ctx context.Context, pageID string, labelName string) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/wiki/rest/api/content/%s/label/%s", pageID, labelName), nil, nil)
}
