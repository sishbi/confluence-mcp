# Confluence MCP

**Note:** This tool was heavily inspired by [mmatczuk/jira-mcp](https://github.com/mmatczuk/jira-mcp), following the same design principles but for Confluence.

Give your AI agent full Confluence access with just 2 tools.

| Tool | What it does |
|---|---|
| `confluence_read` | Get pages by ID/URL, search via CQL, list spaces/children/comments/labels |
| `confluence_write` | Create, update, delete pages; add/edit comments; manage labels. Batch + dry_run |

## Philosophy

- **2 tools, not 72** — the server knows the API so the LLM doesn't have to
- **Credentials stay local** — Basic auth via env vars, no OAuth dance
- **Smart by default** — long pages are automatically chunked with a table of contents; follow-up section requests are served from cache

## Features

### Reading
- **URL parsing** — paste any Confluence page URL, including `?focusedCommentId=` deep-links
- **CQL search** — arbitrary CQL queries via the v1 search endpoint
- **Resource listings** — spaces, children, comments, labels
- **Adaptive chunking** — long pages return a TOC + first chunk; request sections individually by heading
- **`next_page_token` cursor** — base64url JSON cursor for section-index or byte-offset paging; re-fetches silently if the cache has evicted
- **60-second page cache** — eliminates redundant API calls for section follow-ups; evicted on write
- **Raw storage access** — `format="storage"` returns Confluence XHTML when you need to inspect or hand-edit macros

### Writing
- **Markdown in, storage format out** — body fields auto-convert to Confluence XHTML
- **Raw storage passthrough** — set `format="storage"` on an item to push XHTML directly (for macro authoring)
- **Partial-page `append` action** — insert at end, insert after a named heading, or replace a heading's section. The agent sends only the fragment; the server fetches the current body, splices, and writes the merged result. Typical edits ship a payload ~100× smaller than `update` (observed ~147 B vs ~34 KB on a representative fixture), cutting both wall-clock and token cost. Success responses report fragment size and base→merged body bytes so the saving is visible in telemetry. Retries once on 409 from read-replica lag when no version is pinned.
- **Batch-first** — every action takes an array; per-item errors are reported with `[N]` prefixes
- **Dry run** — preview any write as JSON without calling the API
- **Cache eviction** — updates, appends, and deletes automatically invalidate the page cache

### Markdown converter

A bidirectional converter lives in `internal/mdconv` — it is used for every read and write and exercised by a golden-file round-trip suite.

- **Tables** — GFM tables with `colspan` repeat-and-annotate and `rowspan` ⬆ fill; lists inside cells are flattened with `<br>` separators
- **Task lists** — `[x]` / `[ ]` checkbox state preserved
- **Panels → GFM alerts** — info/note/warning/tip/error (including ADF panels) render as `> [!NOTE]` syntax and strip cleanly on write-back
- **Status lozenges** — rendered as colour-keyed emoji badges (🟢/🔵/🔴/🟡/🟣/⚪)
- **Mentions** — resolved to `@DisplayName [accountId]` via a live user lookup (per-conversion cache); write-back restores the original `ac:link`
- **Children macro** — rendered as a real nested Markdown list (depth cap 3) using a live child-page resolver; falls back to a `[Child pages]` placeholder on resolver error
- **Expand / layout** — expand bodies get ┈┈┈ borders; multi-column layouts get `┈┈ Column N ┈┈` delimiters; borders strip on write-back
- **Emoji** — Atlassian shortnames (`:cross_mark:`, `:warning:`, etc.) map to Unicode glyphs, with passthrough for unknown codes
- **Attachments** — `view-file` macros render as `[filename](attachment:…)` links; image captions preserved
- **Anchors, sub/sup, strikethrough, `<u>`** — all round-trip
- **Macro preservation** — unknown or opaque macros (`jira`, `details`, `toc`, `code`, etc.) are replaced with self-describing `<!-- macro:mN -->` sentinels in the Markdown; the original XML is restored verbatim on write

### Operational

- **Structured logging** — slog JSON to stderr with request/retry/error detail in the HTTP client, tool-call middleware, and handler-level page/cache/URL events
- **`-log-level` flag** — `debug`, `info`, `warn`, `error`
- **Install script** — `scripts/install-mcp.sh` (re)registers the server with Claude Code via a wrapper that tees logs to `/tmp/confluence-mcp.log` (override with `CONFLUENCE_MCP_LOG_FILE`); `--debug` switches level, `--remove` uninstalls
- **Anonymise CLI** — `cmd/anonymise` turns a Chrome-saved Confluence page into a deterministic test fixture by replacing text and attribute values while preserving structure
- **Smoke-test harness** — `scripts/smoke_test.go` (build tag `smoke`) runs end-to-end checks against live Confluence; excluded from normal `task test`

## Quick start

### 1. Get an API token

Create an Atlassian API token at https://id.atlassian.com/manage-profile/security/api-tokens

### 2. Install

**Homebrew:**
```bash
brew tap sishbi/confluence-mcp https://github.com/sishbi/confluence-mcp
brew install confluence-mcp
```

**Docker:**
```bash
docker run -e CONFLUENCE_URL=... -e CONFLUENCE_EMAIL=... -e CONFLUENCE_API_TOKEN=... \
  sishbi/confluence-mcp
```

**From source:**
```bash
task build              # produces bin/confluence-mcp
./scripts/install-mcp.sh            # register with Claude Code (info logging)
./scripts/install-mcp.sh --debug    # or with debug logging
./scripts/install-mcp.sh --remove   # to uninstall
```

**Binary:** Download from [Releases](https://github.com/sishbi/confluence-mcp/releases).

### 3. Configure

Set the required environment variables:

```bash
export CONFLUENCE_URL="https://your-domain.atlassian.net"
export CONFLUENCE_EMAIL="you@example.com"
export CONFLUENCE_API_TOKEN="your-api-token"
```

### 4. Add to Claude Code

```bash
claude mcp add confluence-mcp -- confluence-mcp
```

Or use the install script above, which wraps the binary so stderr logs land in a file you can `tail -f`.

### 5. Verify

Ask Claude: "List my Confluence spaces"

## Running smoke tests

```bash
go test -tags smoke -v -timeout 120s ./scripts/
```

Requires the three `CONFLUENCE_*` env vars. The suite reads a known page, round-trips an edit, verifies comments/labels, and restores the original page body via `t.Cleanup`.

## Releasing

Releases are automated via [release-please](https://github.com/googleapis/release-please). The version, changelog, and tag are all driven from PR titles — contributors do not tag manually.

### PR title convention

Every PR title must follow [Conventional Commits](https://www.conventionalcommits.org/) and is enforced by the `Validate PR title` check:

```
<type>(optional-scope): <subject>
```

Allowed types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `style`, `revert`. Append `!` (e.g. `feat!:`) or add a `BREAKING CHANGE:` footer for a breaking change.

### How a release is cut

1. Merge PRs to `main` as usual. Each squash-merge commit keeps its Conventional Commits title.
2. On every push to `main`, the `Release Please` workflow updates (or opens) a standing Release PR titled `chore(main): release X.Y.Z`. It accumulates a changelog entry per merged PR and bumps the version.
3. When the release is ready, merge the Release PR. release-please creates the `vX.Y.Z` tag.
4. The tag push triggers the `Release` workflow (GoReleaser), which publishes the GitHub release binaries, pushes the Docker image, and updates the Homebrew cask.

### Version bump rules (pre-1.0)

While under `1.0.0`, the bump is one level smaller than standard semver:

| Commit type              | Bump  |
|--------------------------|-------|
| `fix:` / `feat:`         | patch |
| `feat!:` / breaking      | minor |

No accidental 1.0 cut. Remove `bump-minor-pre-major` / `bump-patch-for-minor-pre-major` from `release-please-config.json` when you are ready for 1.0.

### Forcing a specific version

If a PR contains only non-bumping types (e.g. `docs:`, `chore:`) but you still want to cut a release when it merges, append a `Release-As: X.Y.Z` trailer to the squash-merge commit message. release-please will cut that version regardless of commit types.

### First-run requirements

- A `RELEASE_PAT` repository secret is required — a fine-grained PAT with `Contents: Read and write` on this repo, owned by a repo admin. Tag pushes from `GITHUB_TOKEN` do not trigger downstream workflows; the PAT is what makes the tag push fire the `Release` workflow.

## License

MIT
