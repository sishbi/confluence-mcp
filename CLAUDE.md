# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

confluence-mcp is a Model Context Protocol (MCP) server for Confluence Cloud, written in Go. It exposes two tools (`confluence_read`, `confluence_write`) over stdio transport. Forked from github.com/mmatczuk/jira-mcp.

## Build & Development

Uses [Task](https://taskfile.dev/) as the task runner (requires `task` CLI). Go version is specified in `go.mod`.

```bash
task build          # Build binary to bin/confluence-mcp
task test           # Run all tests (go test ./...)
task lint           # Run golangci-lint + go vet -tags smoke ./...
task fmt            # Format code + go mod tidy
```

Single test: `go test -run TestName ./internal/confluencemcp/`

Smoke tests (hit live Confluence, build-tag gated so they are excluded from `task test`):

```bash
go test -tags smoke -v -timeout 120s ./scripts/
```

The binary requires three env vars at runtime: `CONFLUENCE_URL`, `CONFLUENCE_EMAIL`, `CONFLUENCE_API_TOKEN`.

Flags:
- `--version` prints `version / commit / date / go` (injected via `-ldflags`).
- `-log-level` selects `debug`, `info`, `warn`, or `error` (default `info`). Logs are slog JSON on stderr.

`scripts/install-mcp.sh` (re)registers the server with Claude Code via `scripts/confluence-mcp-wrapper.sh`, which redirects stderr to `/tmp/confluence-mcp.log` (override with `CONFLUENCE_MCP_LOG_FILE`). `--debug` selects debug level, `--remove` uninstalls.

## Architecture

```
cmd/confluence-mcp/main.go     Entry point — flags, slog setup, Confluence client, current user lookup, stdio MCP server
cmd/anonymise/main.go          CLI to anonymise Chrome-saved Confluence pages into deterministic test fixtures
internal/confluence/            REST API v2 client with retry (429, 502, 503), exponential backoff, slog hooks
internal/confluencemcp/         MCP server, tool handlers, resolver for mentions/children, page cache, chunking
internal/mdconv/                Bidirectional Markdown <-> Confluence storage (XHTML) converter
internal/mdconv/testgen/        Deterministic doc generator, content fingerprinting, fixture helpers
internal/mdconv/testdata/       Anonymised real-world fixtures + generated goldens (fixture-all-elements)
scripts/                        install-mcp.sh, confluence-mcp-wrapper.sh, smoke_test.go (build tag smoke)
```

### Key design decisions

- `internal/confluence/client.go` wraps `net/http` with a `retry()` helper honouring `Retry-After` and handling transient 5xx. No third-party Confluence client library.
- The client exposes `BaseURL()` and `GetUser()` so the handler layer can resolve mention users and build absolute page URLs.
- `internal/confluencemcp/client.go` defines a `ConfluenceClient` interface matching the client methods — handlers depend on this interface; tests use a mock.
- Tool handlers live in separate files per tool (`tool_read.go`, `tool_write.go`) with corresponding `_test.go` files plus `integration_test.go` (in-process MCP client/server via `NewInMemoryTransports`).
- A receiving middleware in `server.go` logs every `tools/call` request with tool name, duration, and result size.
- `confluence_write` accepts Markdown in body fields and auto-converts to storage format via `mdconv.ToStorageFormat()`. Setting `format="storage"` on an item pushes raw XHTML through (for macro authoring).
- `confluence_read` converts storage-format responses to Markdown via `mdconv.ToMarkdownWithMacrosResolved()` using a per-conversion `pageResolver` (user cache, depth cap 3). Setting `format="storage"` returns raw XHTML instead.
- Long pages are adaptively chunked: if content exceeds the threshold, the first chunk + a TOC is returned. Follow-ups use either `section` (by heading) or `next_page_token` (base64url JSON cursor with section-index or byte-offset mode). Cache-served with a silent refetch fallback if the cache has evicted.
- A 60-second in-memory page cache keyed by page ID avoids re-fetching for section follow-ups. Successful `update` and `delete` evict the cache.
- Uses Confluence REST API v2 for all endpoints except v1 for CQL search, current user, and label add/remove (v2 has no equivalents).

### Markdown converter

`internal/mdconv` is a pre/post-process pipeline over goldmark (MD→XHTML) and html-to-markdown/v2 (XHTML→MD).

- Preprocessors pull Confluence-only constructs (mentions, emoticons, attachment images, anchor links, sub/sup, task lists, status lozenges, panels, view-file, ADF extensions, details, layouts, expand) out into HTML the standard tools can consume or into hidden `<!-- macro:mN -->` sentinels that round-trip verbatim.
- Panels (including ADF panels) map to GFM alert syntax (`> [!NOTE]`, `[!WARNING]`, etc.); the alert marker is stripped on write-back. The mapping is **read-only**: a bare `> [!NOTE]` in a markdown fragment does NOT synthesise a new panel macro on write — it becomes a plain `<blockquote>`. Alert syntax only round-trips through an existing macro (paired with its `<!-- macro:mN -->` sentinel and a `MacroRegistry` entry). To add a new panel macro via `confluence_write`, use `format="storage"` with raw `<ac:structured-macro>`.
- Status lozenges render as colour-keyed emoji (🟢/🔵/🔴/🟡/🟣/⚪).
- Tables render as GFM with `colspan` repeat-and-annotate and `rowspan` ⬆ fill. Lists inside cells are flattened with `<br>` separators.
- Macros are renumbered `m1..mN` in document order after extraction so rendered Markdown reads top-to-bottom.
- `Resolver` interface (`ResolveUser`, `ListChildren`) lets the handler layer supply live lookups; `pageResolver` implements it using the Confluence client. A nil resolver keeps the original fallback rendering.
- `TestFixtureAllElementsRoundtrip` is a golden-file suite covering every supported element; regenerate with `UPDATE_GOLDEN=1 go test ./internal/mdconv/...`.

## Design & Implementation Plans

Full design doc and 6 implementation plans are in `.claude/local-plans/`. Start with the master plan:
`.claude/local-plans/2026-04-14-confluence-mcp-master-plan.md`

## Quality Gates

Before considering any change complete, run all three:

```bash
task lint           # Must be 0 issues (includes go vet -tags smoke ./...)
task test           # All packages must pass
task build          # Must compile cleanly
```

Do not skip any of these. If lint or tests fail, fix before moving on. Smoke tests are opt-in and not part of the gate, but run them locally when changes touch the HTTP client, handlers, or converter.

## CI

GitHub Actions runs lint, test (with `-race` and coverage), and a tag-gated smoke-test compile on push/PR to main. See `.github/workflows/ci.yml`. Release builds via GoReleaser on `v*` tags: see `.github/workflows/release.yml`.

## Distribution

Homebrew tap casks in `Casks/`, Docker image via distroless, and GitHub releases.
