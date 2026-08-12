package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Config{
		URL:        baseURL,
		Email:      "test@example.com",
		APIToken:   "test-token",
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
	})
	require.NoError(t, err)
	return c
}

func TestRetry_SucceedsOn429ThenOK(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Space]{
			Results: []Space{{ID: "1", Key: "DEV", Name: "Dev", Type: "global"}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	spaces, _, err := c.GetSpaces(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, spaces, 1)
	assert.Equal(t, 3, calls)
}

func TestRetry_ExhaustedReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, err := c.GetSpaces(context.Background(), nil)
	assert.Error(t, err)
}

func TestRetry_502Retries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Space]{
			Results: []Space{{ID: "1", Key: "DEV", Name: "Dev", Type: "global"}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	spaces, _, err := c.GetSpaces(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, spaces, 1)
	assert.Equal(t, 2, calls)
}

func TestBasicAuth_HeaderSent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Space]{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, _ = c.GetSpaces(context.Background(), nil)
	assert.Contains(t, gotAuth, "Basic ")
}

func TestGetCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wiki/rest/api/user/current", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(User{AccountID: "abc123", DisplayName: "Test User"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	user, err := c.GetCurrentUser(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "abc123", user.AccountID)
	assert.Equal(t, "Test User", user.DisplayName)
}

func TestGetPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wiki/api/v2/pages/123", r.URL.Path)
		assert.Equal(t, "body-format=storage", r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Page{ID: "123", Title: "Test Page", Body: PageBody{Storage: StorageBody{Value: "<p>Hello</p>"}}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	page, err := c.GetPage(context.Background(), "123")
	require.NoError(t, err)
	assert.Equal(t, "123", page.ID)
	assert.Equal(t, "Test Page", page.Title)
	assert.Equal(t, "<p>Hello</p>", page.Body.Storage.Value)
}

func TestGetPageChildren(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wiki/api/v2/pages/123/children", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Page]{Results: []Page{{ID: "456", Title: "Child"}}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	children, _, err := c.GetPageChildren(context.Background(), "123", nil)
	require.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "Child", children[0].Title)
}

func TestCreatePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/wiki/api/v2/pages", r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "New Page", body["title"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Page{ID: "456", Title: "New Page"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	page, err := c.CreatePage(context.Background(), map[string]any{"title": "New Page", "spaceId": "1"})
	require.NoError(t, err)
	assert.Equal(t, "456", page.ID)
}

func TestUpdatePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/wiki/api/v2/pages/123", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Page{ID: "123", Title: "Updated"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	page, err := c.UpdatePage(context.Background(), "123", map[string]any{"title": "Updated"})
	require.NoError(t, err)
	assert.Equal(t, "Updated", page.Title)
}

func TestDeletePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/wiki/api/v2/pages/123", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.DeletePage(context.Background(), "123")
	assert.NoError(t, err)
}

func TestSearchContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/wiki/rest/api/search")
		assert.Contains(t, r.URL.RawQuery, "cql=")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SearchResult{Results: []SearchResultItem{{Title: "Found"}}, TotalSize: 1})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	result, err := c.SearchContent(context.Background(), "type=page", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalSize)
	assert.Equal(t, "Found", result.Results[0].Title)
}

func TestGetPageFooterComments(t *testing.T) {
	tests := []struct {
		name       string
		nextLink   string
		wantCursor string
	}{
		{
			name: "no pagination",
		},
		{
			name:       "cursor extracted from next link, not the raw URL",
			nextLink:   "/wiki/api/v2/pages/123/footer-comments?limit=25&cursor=nextPageCursor",
			wantCursor: "nextPageCursor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/wiki/api/v2/pages/123/footer-comments", r.URL.Path)
				assert.Contains(t, r.URL.RawQuery, "body-format=storage")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(PaginatedResponse[Comment]{
					Results: []Comment{{ID: "c1"}},
					Links:   Links{Next: tt.nextLink},
				})
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL)
			comments, next, err := c.GetPageFooterComments(context.Background(), "123", nil)
			require.NoError(t, err)
			assert.Len(t, comments, 1)
			assert.Equal(t, tt.wantCursor, next)
		})
	}
}

func TestGetPageInlineComments(t *testing.T) {
	tests := []struct {
		name           string
		opts           *ListOptions
		nextLink       string
		wantResolution string // expected resolution-status query value; "" means absent
		wantCursor     string
	}{
		{
			name:       "defaults body-format, no resolution status sent when unset",
			opts:       nil,
			nextLink:   "/wiki/api/v2/pages/123/inline-comments?limit=25&cursor=abc123XYZ",
			wantCursor: "abc123XYZ",
		},
		{
			name:           "resolution status is passed through when set",
			opts:           &ListOptions{ResolutionStatus: []string{ResolutionOpen}},
			nextLink:       "/wiki/api/v2/pages/123/inline-comments?cursor=abc123XYZ",
			wantResolution: "open",
			wantCursor:     "abc123XYZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/wiki/api/v2/pages/123/inline-comments", r.URL.Path)
				assert.Contains(t, r.URL.RawQuery, "body-format=storage")
				if tt.wantResolution != "" {
					assert.Contains(t, r.URL.RawQuery, "resolution-status="+tt.wantResolution)
				} else {
					assert.NotContains(t, r.URL.RawQuery, "resolution-status")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(PaginatedResponse[InlineComment]{
					Results: []InlineComment{{ID: "ic1"}},
					Links:   Links{Next: tt.nextLink},
				})
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL)
			comments, next, err := c.GetPageInlineComments(context.Background(), "123", tt.opts)
			require.NoError(t, err)
			require.Len(t, comments, 1)
			assert.Equal(t, "ic1", comments[0].ID)
			assert.Equal(t, tt.wantCursor, next)
		})
	}
}

func TestGetFooterCommentChildren(t *testing.T) {
	tests := []struct {
		name       string
		opts       *ListOptions
		nextLink   string
		wantCursor string
	}{
		{
			name:       "defaults body-format",
			opts:       nil,
			nextLink:   "/wiki/api/v2/footer-comments/456/children?limit=25&cursor=childCursor1",
			wantCursor: "childCursor1",
		},
		{
			name:       "resolution status in opts is dropped, children endpoint has no such filter",
			opts:       &ListOptions{ResolutionStatus: []string{ResolutionOpen}},
			nextLink:   "/wiki/api/v2/footer-comments/456/children?cursor=childCursor2",
			wantCursor: "childCursor2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/wiki/api/v2/footer-comments/456/children", r.URL.Path)
				assert.Contains(t, r.URL.RawQuery, "body-format=storage")
				assert.NotContains(t, r.URL.RawQuery, "resolution-status")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(PaginatedResponse[Comment]{
					Results: []Comment{{ID: "fc1"}},
					Links:   Links{Next: tt.nextLink},
				})
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL)
			children, next, err := c.GetFooterCommentChildren(context.Background(), "456", tt.opts)
			require.NoError(t, err)
			require.Len(t, children, 1)
			assert.Equal(t, "fc1", children[0].ID)
			assert.Equal(t, tt.wantCursor, next)
		})
	}
}

func TestGetInlineCommentChildren(t *testing.T) {
	tests := []struct {
		name       string
		opts       *ListOptions
		nextLink   string
		wantCursor string
	}{
		{
			name:       "defaults body-format",
			opts:       nil,
			nextLink:   "/wiki/api/v2/inline-comments/456/children?limit=25&cursor=inlineChildCursor1",
			wantCursor: "inlineChildCursor1",
		},
		{
			name:       "resolution status in opts is dropped, children endpoint has no such filter",
			opts:       &ListOptions{ResolutionStatus: []string{ResolutionResolved}},
			nextLink:   "/wiki/api/v2/inline-comments/456/children?cursor=inlineChildCursor2",
			wantCursor: "inlineChildCursor2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/wiki/api/v2/inline-comments/456/children", r.URL.Path)
				assert.Contains(t, r.URL.RawQuery, "body-format=storage")
				assert.NotContains(t, r.URL.RawQuery, "resolution-status")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(PaginatedResponse[InlineComment]{
					Results: []InlineComment{{ID: "ic2"}},
					Links:   Links{Next: tt.nextLink},
				})
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL)
			children, next, err := c.GetInlineCommentChildren(context.Background(), "456", tt.opts)
			require.NoError(t, err)
			require.Len(t, children, 1)
			assert.Equal(t, "ic2", children[0].ID)
			assert.Equal(t, tt.wantCursor, next)
		})
	}
}

func TestExtractCursor(t *testing.T) {
	tests := []struct {
		name string
		next string
		want string
	}{
		{
			name: "cursor is the last param, no trailing ampersand",
			next: "/wiki/api/v2/pages/123/inline-comments?limit=25&cursor=lastParamCursor",
			want: "lastParamCursor",
		},
		{
			name: "cursor is followed by another param",
			next: "/wiki/api/v2/pages/123/inline-comments?cursor=midParamCursor&limit=25",
			want: "midParamCursor",
		},
		{
			name: "percent-encoded value is returned unchanged, not decoded",
			next: "/wiki/api/v2/pages/123/inline-comments?cursor=eyJpZCI6MX0%3D&limit=25",
			want: "eyJpZCI6MX0%3D",
		},
		{
			name: "no cursor param present",
			next: "/wiki/api/v2/pages/123/inline-comments?limit=25",
			want: "",
		},
		{
			name: "empty input",
			next: "",
			want: "",
		},
		{
			name: "prefix-boundary decoy: precursor",
			next: "/wiki/api/v2/pages/123/inline-comments?precursor=x&cursor=real",
			want: "real",
		},
		{
			name: "prefix-boundary decoy: mycursor",
			next: "/wiki/api/v2/pages/123/inline-comments?mycursor=x&cursor=real",
			want: "real",
		},
		{
			name: "cursor present with empty value",
			next: "/wiki/api/v2/pages/123/inline-comments?cursor=&limit=25",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractCursor(tt.next))
		})
	}
}

func TestGetFooterComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wiki/api/v2/footer-comments/456", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Comment{ID: "456", Body: PageBody{Storage: StorageBody{Value: "<p>A comment</p>"}}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	comment, err := c.GetFooterComment(context.Background(), "456")
	require.NoError(t, err)
	assert.Equal(t, "456", comment.ID)
}

func TestGetInlineComment(t *testing.T) {
	tests := []struct {
		name           string
		commentID      string
		statusCode     int
		respBody       string
		wantErr        bool
		wantStatus     int
		wantID         string
		wantResolution string
		wantSelection  string
	}{
		{
			name:       "decodes resolution status and inline original selection",
			commentID:  "789",
			statusCode: http.StatusOK,
			respBody: `{
				"id": "789",
				"resolutionStatus": "resolved",
				"properties": {"inlineOriginalSelection": "the selected text"}
			}`,
			wantID:         "789",
			wantResolution: "resolved",
			wantSelection:  "the selected text",
		},
		{
			name:       "404 surfaces as APIError with StatusCode 404",
			commentID:  "missing",
			statusCode: http.StatusNotFound,
			respBody:   `{"message":"not found"}`,
			wantErr:    true,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/wiki/api/v2/inline-comments/"+tt.commentID, r.URL.Path)
				assert.Contains(t, r.URL.RawQuery, "body-format=storage")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL)
			comment, err := c.GetInlineComment(context.Background(), tt.commentID)

			if tt.wantErr {
				require.Error(t, err)
				var apiErr *APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tt.wantStatus, apiErr.StatusCode)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, comment.ID)
			assert.Equal(t, tt.wantResolution, comment.ResolutionStatus)
			assert.Equal(t, tt.wantSelection, comment.Properties.InlineOriginalSelection)
		})
	}
}

func TestAddComment(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "footer-comments")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Comment{ID: "c2"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	comment, err := c.AddComment(context.Background(), "123", "<p>New comment</p>")
	require.NoError(t, err)
	assert.Equal(t, "c2", comment.ID)

	assert.Equal(t, "123", gotBody["pageId"])
	_, hasParentID := gotBody["parentCommentId"]
	assert.False(t, hasParentID, "parentCommentId must be absent from a top-level comment payload")
}

// capturedRequest records what a captureCommentPost server received.
type capturedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// captureCommentPost stands up an httptest server that captures the
// method, path, and decoded JSON body of the request it receives, then
// responds with respBody.
func captureCommentPost(t *testing.T, respBody string) (*Client, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Method = r.Method
		captured.Path = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured.Body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return newTestClient(t, srv.URL), captured
}

// assertReplyPayload asserts the shape common to every reply payload:
// parentCommentId present and equal to wantParentID, pageId and
// inlineCommentProperties absent, and the storage body matching wantBody.
func assertReplyPayload(t *testing.T, got *capturedRequest, wantParentID, wantBody string) {
	t.Helper()
	assert.Equal(t, http.MethodPost, got.Method)
	assert.Equal(t, wantParentID, got.Body["parentCommentId"])

	_, hasPageID := got.Body["pageId"]
	assert.False(t, hasPageID, "pageId must be absent from a reply payload, not just empty")

	_, hasInlineProps := got.Body["inlineCommentProperties"]
	assert.False(t, hasInlineProps, "inlineCommentProperties must be absent on a reply")

	body, ok := got.Body["body"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "storage", body["representation"])
	assert.Equal(t, wantBody, body["value"])
}

func TestAddCommentReply(t *testing.T) {
	t.Run("footer comment reply", func(t *testing.T) {
		c, got := captureCommentPost(t, `{"id":"c-reply-1"}`)

		comment, err := c.AddFooterCommentReply(context.Background(), "parent-1", "<p>A reply</p>")
		require.NoError(t, err)

		assert.Equal(t, "/wiki/api/v2/footer-comments", got.Path)
		assertReplyPayload(t, got, "parent-1", "<p>A reply</p>")
		assert.Equal(t, "c-reply-1", comment.ID)
	})

	t.Run("inline comment reply", func(t *testing.T) {
		c, got := captureCommentPost(t, `{
			"id": "ic-reply-1",
			"resolutionStatus": "open",
			"properties": {"inlineOriginalSelection": "selected text"}
		}`)

		comment, err := c.AddInlineCommentReply(context.Background(), "parent-2", "<p>An inline reply</p>")
		require.NoError(t, err)

		assert.Equal(t, "/wiki/api/v2/inline-comments", got.Path)
		assertReplyPayload(t, got, "parent-2", "<p>An inline reply</p>")
		assert.Equal(t, "ic-reply-1", comment.ID)
		assert.Equal(t, "open", comment.ResolutionStatus)
		assert.Equal(t, "selected text", comment.Properties.InlineOriginalSelection)
	})

	t.Run("footer reply error propagates as APIError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"parentCommentId not found"}`))
		}))
		defer srv.Close()

		c := newTestClient(t, srv.URL)
		_, err := c.AddFooterCommentReply(context.Background(), "missing-parent", "<p>x</p>")
		require.Error(t, err)
		var apiErr *APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	})

	t.Run("inline reply error propagates as APIError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"forbidden"}`))
		}))
		defer srv.Close()

		c := newTestClient(t, srv.URL)
		_, err := c.AddInlineCommentReply(context.Background(), "missing-parent", "<p>x</p>")
		require.Error(t, err)
		var apiErr *APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	})
}

func TestUpdateComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/wiki/api/v2/footer-comments/c1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Comment{ID: "c1"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	comment, err := c.UpdateComment(context.Background(), "c1", "<p>Updated</p>", 2)
	require.NoError(t, err)
	assert.Equal(t, "c1", comment.ID)
}

func TestGetPageLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wiki/api/v2/pages/123/labels", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Label]{Results: []Label{{ID: "l1", Name: "test"}}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	labels, _, err := c.GetPageLabels(context.Background(), "123", nil)
	require.NoError(t, err)
	assert.Len(t, labels, 1)
	assert.Equal(t, "test", labels[0].Name)
}

func TestAddPageLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/label")
		w.Header().Set("Content-Type", "application/json")
		// v1 API returns a results array with all labels on the page
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []Label{{ID: "l2", Name: "new-label"}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	label, err := c.AddPageLabel(context.Background(), "123", "new-label")
	require.NoError(t, err)
	assert.Equal(t, "new-label", label.Name)
}

func TestAddPageLabel_FallbackToFirstResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// API returned labels but none match the requested name (unlikely but handled).
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []Label{{ID: "l1", Name: "some-other-label"}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	label, err := c.AddPageLabel(context.Background(), "123", "requested")
	require.NoError(t, err)
	assert.Equal(t, "some-other-label", label.Name, "falls back to first result when exact match missing")
}

func TestAddPageLabel_FallbackWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []Label{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	label, err := c.AddPageLabel(context.Background(), "123", "requested")
	require.NoError(t, err)
	assert.Equal(t, "requested", label.Name, "falls back to requested name when results are empty")
	assert.Empty(t, label.ID)
}

func TestBaseURL(t *testing.T) {
	c := newTestClient(t, "https://example.com")
	assert.Equal(t, "https://example.com", c.BaseURL())
}

func TestGetUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/wiki/rest/api/user", r.URL.Path)
		assert.Equal(t, "acc-42", r.URL.Query().Get("accountId"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(User{AccountID: "acc-42", DisplayName: "Jane"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	user, err := c.GetUser(context.Background(), "acc-42")
	require.NoError(t, err)
	assert.Equal(t, "Jane", user.DisplayName)
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{StatusCode: 409, Body: "StaleStateException"}
	assert.Contains(t, err.Error(), "409")
	assert.Contains(t, err.Error(), "StaleStateException")
}

func TestRemovePageLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/wiki/rest/api/content/123/label/old-label", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.RemovePageLabel(context.Background(), "123", "old-label")
	assert.NoError(t, err)
}

func TestNew_MissingURL(t *testing.T) {
	_, err := New(Config{Email: "a@b.com", APIToken: "tok"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL")
}

func TestNew_MissingCredentials(t *testing.T) {
	_, err := New(Config{URL: "https://example.com"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "email")
}

func TestNew_Defaults(t *testing.T) {
	c, err := New(Config{URL: "https://example.com", Email: "a@b.com", APIToken: "tok"})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", c.baseURL)
	assert.Equal(t, 0, c.cfg.MaxRetries) // 0 is valid, not overridden
}

func TestGetSpaces_WithPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "limit=10")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Space]{
			Results: []Space{{ID: "1", Key: "DEV"}},
			Links:   Links{Next: "/wiki/api/v2/spaces?cursor=abc123&limit=10"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	spaces, next, err := c.GetSpaces(context.Background(), &ListOptions{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, spaces, 1)
	assert.Equal(t, "abc123", next)
}

func TestGetSpaces_CursorWithAmpersand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Space]{
			Results: []Space{{ID: "1"}},
			Links:   Links{Next: "/wiki/api/v2/spaces?cursor=xyz789&limit=25"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, next, err := c.GetSpaces(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "xyz789", next)
}

func TestSearchContent_WithOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "cql=")
		assert.Contains(t, r.URL.RawQuery, "limit=5")
		assert.Contains(t, r.URL.RawQuery, "cursor=page2")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SearchResult{
			Results:   []SearchResultItem{{Title: "Result"}},
			TotalSize: 1,
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	result, err := c.SearchContent(context.Background(), "type=page", &ListOptions{Limit: 5, Cursor: "page2"})
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalSize)
}

func TestRetry_LogsAttempts(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Space]{
			Results: []Space{{ID: "1", Key: "DEV", Name: "Dev", Type: "global"}},
		})
	}))
	defer srv.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c, err := New(Config{
		URL:        srv.URL,
		Email:      "test@example.com",
		APIToken:   "test-token",
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
		Logger:     logger,
	})
	require.NoError(t, err)

	_, _, err = c.GetSpaces(context.Background(), nil)
	require.NoError(t, err)

	logs := buf.String()
	assert.Contains(t, logs, "http_request")
	assert.Contains(t, logs, "retry")
}

func TestDoJSON_4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.GetPage(context.Background(), "999")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestBuildQuery(t *testing.T) {
	tests := []struct {
		name string
		opts *ListOptions
		want string
	}{
		{
			name: "nil opts",
			opts: nil,
			want: "",
		},
		{
			name: "empty opts",
			opts: &ListOptions{},
			want: "",
		},
		{
			name: "limit only",
			opts: &ListOptions{Limit: 25},
			want: "?limit=25",
		},
		{
			// Cursor arrives pre-encoded from _links.next (see extractCursor) and
			// must pass through verbatim -- re-escaping it here would double
			// encode it and break pagination. Do not "fix" this back to
			// url.QueryEscape.
			name: "pre-encoded cursor passes through verbatim",
			opts: &ListOptions{Cursor: "eyJpZCI6MX0%3D"},
			want: "?cursor=eyJpZCI6MX0%3D",
		},
		{
			name: "body format only",
			opts: &ListOptions{BodyFormat: "storage"},
			want: "?body-format=storage",
		},
		{
			name: "body format with characters requiring escaping",
			opts: &ListOptions{BodyFormat: "storage view"},
			want: "?body-format=storage+view",
		},
		{
			name: "single resolution status",
			opts: &ListOptions{ResolutionStatus: []string{ResolutionOpen}},
			want: "?resolution-status=open",
		},
		{
			name: "multiple resolution statuses repeat the param",
			opts: &ListOptions{ResolutionStatus: []string{ResolutionOpen, ResolutionResolved}},
			want: "?resolution-status=open&resolution-status=resolved",
		},
		{
			name: "resolution status with characters requiring escaping",
			opts: &ListOptions{ResolutionStatus: []string{"open&extra=1"}},
			want: "?resolution-status=open%26extra%3D1",
		},
		{
			name: "combination of limit, cursor, body format and statuses",
			opts: &ListOptions{
				Limit:            10,
				Cursor:           "next-cursor",
				BodyFormat:       "storage",
				ResolutionStatus: []string{ResolutionOpen, ResolutionResolved},
			},
			want: "?limit=10&cursor=next-cursor&body-format=storage&resolution-status=open&resolution-status=resolved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildQuery(tt.opts)
			assert.Equal(t, tt.want, got)
		})
	}
}
