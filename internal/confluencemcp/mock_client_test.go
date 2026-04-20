package confluencemcp

import (
	"context"

	"github.com/sishbi/confluence-mcp/internal/confluence"
)

type mockClient struct {
	BaseURLValue      string
	GetCurrentUserFn  func(ctx context.Context) (*confluence.User, error)
	GetUserFn         func(ctx context.Context, accountID string) (*confluence.User, error)
	GetSpacesFn       func(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error)
	GetPageFn         func(ctx context.Context, id string) (*confluence.Page, error)
	GetPageChildrenFn func(ctx context.Context, id string, opts *confluence.ListOptions) ([]confluence.Page, string, error)
	CreatePageFn      func(ctx context.Context, payload map[string]any) (*confluence.Page, error)
	UpdatePageFn      func(ctx context.Context, id string, payload map[string]any) (*confluence.Page, error)
	DeletePageFn      func(ctx context.Context, id string) error
	SearchContentFn   func(ctx context.Context, cql string, opts *confluence.ListOptions) (*confluence.SearchResult, error)
	GetPageCommentsFn func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error)
	GetCommentFn      func(ctx context.Context, commentID string) (*confluence.Comment, error)
	AddCommentFn      func(ctx context.Context, pageID string, body string) (*confluence.Comment, error)
	UpdateCommentFn   func(ctx context.Context, commentID string, body string, versionNumber int) (*confluence.Comment, error)
	GetPageLabelsFn   func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Label, string, error)
	AddPageLabelFn    func(ctx context.Context, pageID string, label string) (*confluence.Label, error)
	RemovePageLabelFn func(ctx context.Context, pageID string, label string) error
}

func (m *mockClient) BaseURL() string { return m.BaseURLValue }

func (m *mockClient) GetCurrentUser(ctx context.Context) (*confluence.User, error) {
	if m.GetCurrentUserFn == nil {
		panic("GetCurrentUserFn not set")
	}
	return m.GetCurrentUserFn(ctx)
}

func (m *mockClient) GetUser(ctx context.Context, accountID string) (*confluence.User, error) {
	if m.GetUserFn == nil {
		panic("GetUserFn not set")
	}
	return m.GetUserFn(ctx, accountID)
}

func (m *mockClient) GetSpaces(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error) {
	if m.GetSpacesFn == nil {
		panic("GetSpacesFn not set")
	}
	return m.GetSpacesFn(ctx, opts)
}

func (m *mockClient) GetPage(ctx context.Context, id string) (*confluence.Page, error) {
	if m.GetPageFn == nil {
		panic("GetPageFn not set")
	}
	return m.GetPageFn(ctx, id)
}

func (m *mockClient) GetPageChildren(ctx context.Context, id string, opts *confluence.ListOptions) ([]confluence.Page, string, error) {
	if m.GetPageChildrenFn == nil {
		panic("GetPageChildrenFn not set")
	}
	return m.GetPageChildrenFn(ctx, id, opts)
}

func (m *mockClient) CreatePage(ctx context.Context, payload map[string]any) (*confluence.Page, error) {
	if m.CreatePageFn == nil {
		panic("CreatePageFn not set")
	}
	return m.CreatePageFn(ctx, payload)
}

func (m *mockClient) UpdatePage(ctx context.Context, id string, payload map[string]any) (*confluence.Page, error) {
	if m.UpdatePageFn == nil {
		panic("UpdatePageFn not set")
	}
	return m.UpdatePageFn(ctx, id, payload)
}

func (m *mockClient) DeletePage(ctx context.Context, id string) error {
	if m.DeletePageFn == nil {
		panic("DeletePageFn not set")
	}
	return m.DeletePageFn(ctx, id)
}

func (m *mockClient) SearchContent(ctx context.Context, cql string, opts *confluence.ListOptions) (*confluence.SearchResult, error) {
	if m.SearchContentFn == nil {
		panic("SearchContentFn not set")
	}
	return m.SearchContentFn(ctx, cql, opts)
}

func (m *mockClient) GetPageComments(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
	if m.GetPageCommentsFn == nil {
		panic("GetPageCommentsFn not set")
	}
	return m.GetPageCommentsFn(ctx, pageID, opts)
}

func (m *mockClient) GetComment(ctx context.Context, commentID string) (*confluence.Comment, error) {
	if m.GetCommentFn == nil {
		panic("GetCommentFn not set")
	}
	return m.GetCommentFn(ctx, commentID)
}

func (m *mockClient) AddComment(ctx context.Context, pageID string, body string) (*confluence.Comment, error) {
	if m.AddCommentFn == nil {
		panic("AddCommentFn not set")
	}
	return m.AddCommentFn(ctx, pageID, body)
}

func (m *mockClient) UpdateComment(ctx context.Context, commentID string, body string, versionNumber int) (*confluence.Comment, error) {
	if m.UpdateCommentFn == nil {
		panic("UpdateCommentFn not set")
	}
	return m.UpdateCommentFn(ctx, commentID, body, versionNumber)
}

func (m *mockClient) GetPageLabels(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Label, string, error) {
	if m.GetPageLabelsFn == nil {
		panic("GetPageLabelsFn not set")
	}
	return m.GetPageLabelsFn(ctx, pageID, opts)
}

func (m *mockClient) AddPageLabel(ctx context.Context, pageID string, label string) (*confluence.Label, error) {
	if m.AddPageLabelFn == nil {
		panic("AddPageLabelFn not set")
	}
	return m.AddPageLabelFn(ctx, pageID, label)
}

func (m *mockClient) RemovePageLabel(ctx context.Context, pageID string, label string) error {
	if m.RemovePageLabelFn == nil {
		panic("RemovePageLabelFn not set")
	}
	return m.RemovePageLabelFn(ctx, pageID, label)
}
