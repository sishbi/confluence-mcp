package confluencemcp

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connect creates an in-process client session connected to the given server.
func connect(ctx context.Context, t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	t1, t2 := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, t1, nil)
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestNewServer_ToolsRegistered(t *testing.T) {
	s := NewServer(&mockClient{}, &confluence.User{
		AccountID:   "abc123",
		DisplayName: "Test User",
	}, nil)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	res, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)

	toolNames := make(map[string]bool)
	for _, tool := range res.Tools {
		toolNames[tool.Name] = true
	}

	assert.True(t, toolNames["confluence_read"], "confluence_read should be registered")
	assert.True(t, toolNames["confluence_write"], "confluence_write should be registered")
	assert.Len(t, res.Tools, 2, "exactly 2 tools should be registered")
}

func TestNewServer_InstructionsIncludeUser(t *testing.T) {
	s := NewServer(&mockClient{}, &confluence.User{
		AccountID:   "abc123",
		DisplayName: "Test User",
	}, nil)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	init := cs.InitializeResult()
	require.NotNil(t, init)
	assert.Contains(t, init.Instructions, "Test User")
	assert.Contains(t, init.Instructions, "abc123")
}

func TestNewServer_InstructionsWithoutUser(t *testing.T) {
	s := NewServer(&mockClient{}, nil, nil)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	init := cs.InitializeResult()
	require.NotNil(t, init)
	assert.Contains(t, init.Instructions, "confluence_read")
	assert.NotContains(t, init.Instructions, "accountId")
}

func TestNewServer_CallReadTool(t *testing.T) {
	s := NewServer(&mockClient{
		GetSpacesFn: func(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error) {
			return []confluence.Space{{ID: "1", Key: "DEV", Name: "Development"}}, "", nil
		},
	}, nil, nil)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_read",
		Arguments: map[string]any{
			"resource": "spaces",
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	require.NotEmpty(t, result.Content)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "DEV")
	assert.Contains(t, text, "Development")
}

func TestNewServer_LoggerPassedToSDK(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s := NewServer(&mockClient{
		GetSpacesFn: func(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error) {
			return []confluence.Space{{ID: "1", Key: "DEV", Name: "Development"}}, "", nil
		},
	}, nil, logger)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	_, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"resource": "spaces"},
	})
	require.NoError(t, err)

	// The SDK or our middleware should have logged something
	assert.NotEmpty(t, buf.String())
}

func TestNewServer_ToolCallMiddlewareLogsCall(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s := NewServer(&mockClient{
		GetSpacesFn: func(ctx context.Context, opts *confluence.ListOptions) ([]confluence.Space, string, error) {
			return []confluence.Space{{ID: "1", Key: "DEV", Name: "Dev"}}, "", nil
		},
	}, nil, logger)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	_, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{"resource": "spaces"},
	})
	require.NoError(t, err)

	logs := buf.String()
	assert.Contains(t, logs, "tool_call")
	assert.Contains(t, logs, "confluence_read")
	assert.Contains(t, logs, "duration")
}

func TestNewServer_ToolCallMiddlewareLogsError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s := NewServer(&mockClient{}, nil, logger)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	// Call with no mode set — returns isError=true
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "confluence_read",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)

	logs := buf.String()
	assert.Contains(t, logs, "tool_result")
	assert.Contains(t, logs, `"is_error":true`)
}

func TestNewServer_CallWriteTool_DryRun(t *testing.T) {
	s := NewServer(&mockClient{}, nil, nil)

	ctx := context.Background()
	cs := connect(ctx, t, s)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "confluence_write",
		Arguments: map[string]any{
			"action":  "create",
			"dry_run": true,
			"items": []any{
				map[string]any{
					"space_id": "123",
					"title":    "Test Page",
					"body":     "Hello **world**",
				},
			},
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	require.NotEmpty(t, result.Content)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Would create")
}
