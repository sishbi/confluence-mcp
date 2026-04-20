// Package confluencemcp implements the MCP server with Confluence tools.
package confluencemcp

import (
	"context"

	"github.com/sishbi/confluence-mcp/internal/confluence"
)

// ConfluenceClient defines the Confluence operations used by the MCP handlers.
type ConfluenceClient interface {
	BaseURL() string
	GetCurrentUser(ctx context.Context) (*confluence.User, error)
	GetUser(ctx context.Context, accountID string) (*confluence.User, error)
	GetSpaces(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error)
	GetPage(ctx context.Context, id string) (*confluence.Page, error)
	GetPageChildren(ctx context.Context, id string, opts *confluence.ListOptions) ([]confluence.Page, string, error)
	CreatePage(ctx context.Context, payload map[string]any) (*confluence.Page, error)
	UpdatePage(ctx context.Context, id string, payload map[string]any) (*confluence.Page, error)
	DeletePage(ctx context.Context, id string) error
	SearchContent(ctx context.Context, cql string, opts *confluence.ListOptions) (*confluence.SearchResult, error)
	GetPageComments(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Comment, string, error)
	GetComment(ctx context.Context, commentID string) (*confluence.Comment, error)
	AddComment(ctx context.Context, pageID string, body string) (*confluence.Comment, error)
	UpdateComment(ctx context.Context, commentID string, body string, versionNumber int) (*confluence.Comment, error)
	GetPageLabels(ctx context.Context, pageID string, opts *confluence.ListOptions) ([]confluence.Label, string, error)
	AddPageLabel(ctx context.Context, pageID string, label string) (*confluence.Label, error)
	RemovePageLabel(ctx context.Context, pageID string, label string) error
}
