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

func TestGetPageComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wiki/api/v2/pages/123/footer-comments", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PaginatedResponse[Comment]{Results: []Comment{{ID: "c1"}}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	comments, _, err := c.GetPageComments(context.Background(), "123", nil)
	require.NoError(t, err)
	assert.Len(t, comments, 1)
}

func TestGetComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wiki/api/v2/footer-comments/456", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Comment{ID: "456", Body: PageBody{Storage: StorageBody{Value: "<p>A comment</p>"}}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	comment, err := c.GetComment(context.Background(), "456")
	require.NoError(t, err)
	assert.Equal(t, "456", comment.ID)
}

func TestAddComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "footer-comments")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Comment{ID: "c2"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	comment, err := c.AddComment(context.Background(), "123", "<p>New comment</p>")
	require.NoError(t, err)
	assert.Equal(t, "c2", comment.ID)
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
