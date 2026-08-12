package confluencemcp

import (
	"context"

	"github.com/sishbi/confluence-mcp/internal/confluence"
)

type mockClient struct {
	BaseURLValue               string
	GetCurrentUserFn           func(ctx context.Context) (*confluence.User, error)
	GetUserFn                  func(ctx context.Context, accountID string) (*confluence.User, error)
	GetSpacesFn                func(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error)
	GetPageFn                  func(ctx context.Context, id string) (*confluence.Page, error)
	GetPageChildrenFn          func(ctx context.Context, id string, opts *confluence.ListOptions) ([]confluence.Page, string, error)
	CreatePageFn               func(ctx context.Context, payload map[string]any) (*confluence.Page, error)
	UpdatePageFn               func(ctx context.Context, id string, payload map[string]any) (*confluence.Page, error)
	DeletePageFn               func(ctx context.Context, id string) error
	SearchContentFn            func(ctx context.Context, cql string, opts *confluence.ListOptions) (*confluence.SearchResult, error)
	GetPageFooterCommentsFn    func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error)
	GetPageInlineCommentsFn    func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error)
	GetFooterCommentChildrenFn func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error)
	GetInlineCommentChildrenFn func(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error)
	GetFooterCommentFn         func(ctx context.Context, commentID string) (*confluence.Comment, error)
	GetInlineCommentFn         func(ctx context.Context, commentID string) (*confluence.InlineComment, error)
	AddCommentFn               func(ctx context.Context, pageID string, body string) (*confluence.Comment, error)
	AddFooterCommentReplyFn    func(ctx context.Context, parentCommentID string, storageBody string) (*confluence.Comment, error)
	AddInlineCommentReplyFn    func(ctx context.Context, parentCommentID string, storageBody string) (*confluence.InlineComment, error)
	UpdateCommentFn            func(ctx context.Context, commentID string, body string, versionNumber int) (*confluence.Comment, error)
	GetPageLabelsFn            func(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Label, string, error)
	AddPageLabelFn             func(ctx context.Context, pageID string, label string) (*confluence.Label, error)
	RemovePageLabelFn          func(ctx context.Context, pageID string, label string) error
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

func (m *mockClient) GetPageFooterComments(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
	if m.GetPageFooterCommentsFn == nil {
		panic("GetPageFooterCommentsFn not set")
	}
	return m.GetPageFooterCommentsFn(ctx, pageID, opts)
}

func (m *mockClient) GetPageInlineComments(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
	if m.GetPageInlineCommentsFn == nil {
		panic("GetPageInlineCommentsFn not set")
	}
	return m.GetPageInlineCommentsFn(ctx, pageID, opts)
}

func (m *mockClient) GetFooterCommentChildren(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error) {
	if m.GetFooterCommentChildrenFn == nil {
		panic("GetFooterCommentChildrenFn not set")
	}
	return m.GetFooterCommentChildrenFn(ctx, commentID, opts)
}

func (m *mockClient) GetInlineCommentChildren(ctx context.Context, commentID string, opts *confluence.ListOptions) ([]confluence.InlineComment, string, error) {
	if m.GetInlineCommentChildrenFn == nil {
		panic("GetInlineCommentChildrenFn not set")
	}
	return m.GetInlineCommentChildrenFn(ctx, commentID, opts)
}

func (m *mockClient) GetFooterComment(ctx context.Context, commentID string) (*confluence.Comment, error) {
	if m.GetFooterCommentFn == nil {
		panic("GetFooterCommentFn not set")
	}
	return m.GetFooterCommentFn(ctx, commentID)
}

func (m *mockClient) GetInlineComment(ctx context.Context, commentID string) (*confluence.InlineComment, error) {
	if m.GetInlineCommentFn == nil {
		panic("GetInlineCommentFn not set")
	}
	return m.GetInlineCommentFn(ctx, commentID)
}

func (m *mockClient) AddComment(ctx context.Context, pageID string, body string) (*confluence.Comment, error) {
	if m.AddCommentFn == nil {
		panic("AddCommentFn not set")
	}
	return m.AddCommentFn(ctx, pageID, body)
}

func (m *mockClient) AddFooterCommentReply(ctx context.Context, parentCommentID string, storageBody string) (*confluence.Comment, error) {
	if m.AddFooterCommentReplyFn == nil {
		panic("AddFooterCommentReplyFn not set")
	}
	return m.AddFooterCommentReplyFn(ctx, parentCommentID, storageBody)
}

func (m *mockClient) AddInlineCommentReply(ctx context.Context, parentCommentID string, storageBody string) (*confluence.InlineComment, error) {
	if m.AddInlineCommentReplyFn == nil {
		panic("AddInlineCommentReplyFn not set")
	}
	return m.AddInlineCommentReplyFn(ctx, parentCommentID, storageBody)
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
