package confluencemcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sishbi/confluence-mcp/internal/confluence"
)

func TestResolver_ResolveUser_CachesHit(t *testing.T) {
	calls := 0
	client := &mockClient{
		GetUserFn: func(_ context.Context, accountID string) (*confluence.User, error) {
			calls++
			return &confluence.User{AccountID: accountID, DisplayName: "Jane Doe"}, nil
		},
	}
	r := newPageResolver(context.Background(), client, "https://x", "p1")

	name, ok := r.ResolveUser("acc-1")
	assert.True(t, ok)
	assert.Equal(t, "Jane Doe", name)

	name2, ok2 := r.ResolveUser("acc-1")
	assert.True(t, ok2)
	assert.Equal(t, "Jane Doe", name2)
	assert.Equal(t, 1, calls, "second lookup should hit the cache")
}

func TestResolver_ResolveUser_CachesMiss(t *testing.T) {
	calls := 0
	client := &mockClient{
		GetUserFn: func(_ context.Context, _ string) (*confluence.User, error) {
			calls++
			return nil, errors.New("not found")
		},
	}
	r := newPageResolver(context.Background(), client, "https://x", "p1")

	name, ok := r.ResolveUser("acc-miss")
	assert.False(t, ok)
	assert.Empty(t, name)

	_, ok2 := r.ResolveUser("acc-miss")
	assert.False(t, ok2)
	assert.Equal(t, 1, calls, "failed lookups should be cached as empty string")
}

func TestResolver_ResolveUser_EmptyAccountID(t *testing.T) {
	r := newPageResolver(context.Background(), &mockClient{}, "https://x", "p1")
	name, ok := r.ResolveUser("")
	assert.False(t, ok)
	assert.Empty(t, name)
}

func TestResolver_ResolveUser_NilReceiver(t *testing.T) {
	var r *pageResolver
	name, ok := r.ResolveUser("acc-1")
	assert.False(t, ok)
	assert.Empty(t, name)
}

func TestResolver_ListChildren_Success(t *testing.T) {
	client := &mockClient{
		GetPageChildrenFn: func(_ context.Context, id string, _ *confluence.ListOptions) ([]confluence.Page, string, error) {
			assert.Equal(t, "parent-1", id)
			return []confluence.Page{
				{ID: "c1", Title: "Child 1"},
				{ID: "c2", Title: "Child 2"},
			}, "", nil
		},
	}
	r := newPageResolver(context.Background(), client, "https://example.atlassian.net", "p1")

	kids, err := r.ListChildren("parent-1", 1)
	require.NoError(t, err)
	require.Len(t, kids, 2)
	assert.Equal(t, "c1", kids[0].ID)
	assert.Equal(t, "Child 1", kids[0].Title)
	assert.Equal(t, "https://example.atlassian.net/wiki/pages/viewpage.action?pageId=c1", kids[0].URL)
	assert.Nil(t, kids[0].Children, "depth=1 should not recurse")
}

func TestResolver_ListChildren_Recursive(t *testing.T) {
	calls := []string{}
	client := &mockClient{
		GetPageChildrenFn: func(_ context.Context, id string, _ *confluence.ListOptions) ([]confluence.Page, string, error) {
			calls = append(calls, id)
			switch id {
			case "root":
				return []confluence.Page{{ID: "c1", Title: "C1"}}, "", nil
			case "c1":
				return []confluence.Page{{ID: "g1", Title: "G1"}}, "", nil
			default:
				return nil, "", nil
			}
		},
	}
	r := newPageResolver(context.Background(), client, "https://x", "p1")

	kids, err := r.ListChildren("root", 2)
	require.NoError(t, err)
	require.Len(t, kids, 1)
	require.Len(t, kids[0].Children, 1)
	assert.Equal(t, "g1", kids[0].Children[0].ID)
	assert.Equal(t, []string{"root", "c1"}, calls)
}

func TestResolver_ListChildren_DepthClamped(t *testing.T) {
	calls := 0
	client := &mockClient{
		GetPageChildrenFn: func(_ context.Context, _ string, _ *confluence.ListOptions) ([]confluence.Page, string, error) {
			calls++
			return []confluence.Page{{ID: "next", Title: "T"}}, "", nil
		},
	}
	r := newPageResolver(context.Background(), client, "", "p1")

	// Request depth 10, expect recursion to stop at 3.
	_, err := r.ListChildren("root", 10)
	require.NoError(t, err)
	assert.Equal(t, 3, calls, "depth should be clamped to 3")
}

func TestResolver_ListChildren_DefaultsEmptyParentToResolverPage(t *testing.T) {
	var queried string
	client := &mockClient{
		GetPageChildrenFn: func(_ context.Context, id string, _ *confluence.ListOptions) ([]confluence.Page, string, error) {
			queried = id
			return nil, "", nil
		},
	}
	r := newPageResolver(context.Background(), client, "", "resolver-page")

	_, err := r.ListChildren("", 1)
	require.NoError(t, err)
	assert.Equal(t, "resolver-page", queried)
}

func TestResolver_ListChildren_ZeroDepthTreatedAsOne(t *testing.T) {
	calls := 0
	client := &mockClient{
		GetPageChildrenFn: func(_ context.Context, _ string, _ *confluence.ListOptions) ([]confluence.Page, string, error) {
			calls++
			return []confluence.Page{{ID: "c1"}}, "", nil
		},
	}
	r := newPageResolver(context.Background(), client, "", "p1")

	kids, err := r.ListChildren("root", 0)
	require.NoError(t, err)
	assert.Len(t, kids, 1)
	assert.Equal(t, 1, calls, "depth=0 should behave like depth=1")
}

func TestResolver_ListChildren_Error(t *testing.T) {
	client := &mockClient{
		GetPageChildrenFn: func(_ context.Context, _ string, _ *confluence.ListOptions) ([]confluence.Page, string, error) {
			return nil, "", errors.New("boom")
		},
	}
	r := newPageResolver(context.Background(), client, "", "p1")

	_, err := r.ListChildren("root", 1)
	assert.Error(t, err)
}

func TestResolver_ListChildren_RecursiveErrorSkipped(t *testing.T) {
	client := &mockClient{
		GetPageChildrenFn: func(_ context.Context, id string, _ *confluence.ListOptions) ([]confluence.Page, string, error) {
			if id == "root" {
				return []confluence.Page{{ID: "c1", Title: "C1"}}, "", nil
			}
			return nil, "", errors.New("deep fail")
		},
	}
	r := newPageResolver(context.Background(), client, "", "p1")

	kids, err := r.ListChildren("root", 2)
	require.NoError(t, err, "top-level success should not propagate recursive failures")
	require.Len(t, kids, 1)
	assert.Nil(t, kids[0].Children, "failed child recursion leaves children unset")
}

func TestResolver_ListChildren_NilReceiver(t *testing.T) {
	var r *pageResolver
	_, err := r.ListChildren("root", 1)
	assert.Error(t, err)
}

func TestResolver_PageURL(t *testing.T) {
	t.Run("with baseURL", func(t *testing.T) {
		r := newPageResolver(context.Background(), &mockClient{}, "https://x.atlassian.net/", "p1")
		assert.Equal(t, "https://x.atlassian.net/wiki/pages/viewpage.action?pageId=abc", r.pageURL("abc"))
	})
	t.Run("empty baseURL", func(t *testing.T) {
		r := newPageResolver(context.Background(), &mockClient{}, "", "p1")
		assert.Empty(t, r.pageURL("abc"))
	})
	t.Run("empty pageID", func(t *testing.T) {
		r := newPageResolver(context.Background(), &mockClient{}, "https://x", "p1")
		assert.Empty(t, r.pageURL(""))
	})
}
