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
- `confluence_write` accepts Markdown in body fields and auto-converts to storage format via `mdconv.ToStorageFormat()`. Setting `format="storage"` on a `create`, `update`, `append`, or `reply_comment` item pushes raw XHTML through (for macro authoring); `comment`, `edit_comment`, `delete`, `add_label`, and `remove_label` reject `format`. `create`'s optional `page_id` names the source page whose macro registry to reuse when the body carries `<!-- macro:mN -->` sentinels (e.g. copying a page while preserving its macros) — it is a hard error otherwise.
- The `append` action performs a server-side splice: the handler fetches the current storage body, splices the fragment via `internal/confluencemcp/splice.go`, and PUTs the merged result — but the agent only sends the fragment (not the full body). Typical edits are ~100× smaller than `update` payloads, cutting both wall-clock and token cost. The success message reports fragment size and base→merged body bytes so the saving is visible to the caller. Always prefer `append` over `update` for additive edits or single-section replacements; use `update` only when rewriting the whole page. Positions are `end`, `after_heading`, `end_of_section`, `replace_section`, `start`, `replace_preamble`: `after_heading` inserts at the top of a section, `end_of_section` at the bottom (after its existing content, before the next heading) — a new sibling section wants `end_of_section`, since `after_heading` displaces the target section's own body into it. `replace_section` stops at the next heading of ANY level, so subsections survive a replace and the fragment need not repeat them; `include_subsections: true` widens the range to the whole section. Both the preview and the success message name the nested subsections replaced or preserved — the earlier default silently deleted them and only the byte delta showed it. `replace_section` fails with `section_boundary_unbalanced` if a plain wrapper element (e.g. a `<div>`) inside the replaced range opens before the chosen stop but does not close before it — replacing would delete its opening tag and orphan its closing tag; the caller should use `update` for a full-body replace instead. `end_of_section` is unaffected by the same shape: it only inserts at the stop offset without deleting anything before it, and that offset is always a genuine token boundary, so the insert is still well-formed there, only nested one level deeper than expected. `replace_section` also accepts `new_heading`, which renames the target heading in place (plain text, escaped, level and attributes preserved) — previously the only route was `update` with the whole body, so agents left heading renames to the user. It is rejected when the new text is the current text, when it already names another locatable heading on the page (every later `locateHeading` would return `ambiguous_heading`), or when the heading contains a Confluence-namespaced child (`ac:`/`ri:` — mention, macro, status lozenge, emoticon) that a plain-text rewrite would destroy silently. Plain XHTML inline formatting (`em`, `strong`, `code`, `span`) is allowed through and replaced along with the words: the caller saw it as Markdown, so losing it is what they asked for. The rule is namespace-based, not an allow-list, so a new Confluence construct is refused by default. All three fire before the PUT. A rename breaks anchors: Confluence derives them from heading text, so `<ac:link ac:anchor="...">` and `anchor` macros naming the old text stop resolving. `splice_anchor.go` finds the on-page ones and both the preview and the success message name them; they are reported, never rewritten, because rewriting would edit the page outside the section the caller named. Off-page links are invisible from here. Retries once on 409 when no `version_number` is pinned (Confluence read replicas are eventually consistent); surfaces `version_conflict` without retry when the caller pins a version. Two more positions, `start` and `replace_preamble`, edit the page preamble — the content above the first heading — which was previously unreachable except by rewriting the whole page with `update`; a preamble routinely holds a scope statement and a TOC macro. `start` inserts at byte 0 of the body, or just past the opening tag of the FIRST `<ac:layout-cell>` on a layout-wrapped page — deliberately the mirror of `end`, which targets the LAST cell, so on a two-column page `start` writes into the left cell and `end` into the right one. `replace_preamble` replaces everything from that same start point up to (not including) the first locatable heading, leaving the heading and everything after it untouched; unlike `replace_section` it does not strip a leading heading from the fragment, since there is no target heading being preserved for a leading heading to duplicate — a fragment opening with a heading is ordinary preamble content and is inserted whole. Both take no `heading`, `include_subsections`, or `new_heading` — they operate on structure, not a named section — and `end` was tightened to match: it now rejects a supplied `heading` too, where it previously accepted and silently dropped it. A heading buried in a macro body or an unsafe container (`td`, `th`, `blockquote`, `li`, `adf-extension`, `task-body`) never ends the preamble, matching `locateHeading`'s own rule that such a heading can never be located by name either; and the preamble never crosses a layout cell, so a first cell with no heading of its own is `no_heading_on_page` rather than a range spilling into the second cell. `replace_preamble` returns `no_heading_on_page` whenever the container has no locatable heading at all, since the replaced range would then be undefined — the caller should use `update` for a full-body replace on a headless page. It also returns `preamble_boundary_unbalanced` if the first heading sits inside a plain wrapper element that itself opens in the preamble but closes after it — replacing `[containerStart, firstHeadingStart)` would then delete the wrapper's opening tag while its closing tag survives further down the page; `update` is the caller's way out here too. The dry-run preview and the replaced-element summary also now name the macros inside a replaced range (e.g. `macro "toc" x 1`) rather than omitting them, which makes `replace_section` safer too.
- `confluence_read` converts storage-format responses to Markdown via `mdconv.ToMarkdownWithMacrosResolved()` using a per-conversion `pageResolver` (user cache, depth cap 3). Setting `format="storage"` returns raw XHTML instead.
- Long pages are adaptively chunked: if content exceeds the threshold, the first chunk + a TOC is returned. Follow-ups use either `section` (by heading) or `next_page_token` (base64url JSON cursor with section-index or byte-offset mode). Cache-served with a silent refetch fallback if the cache has evicted.
- A 60-second in-memory page cache keyed by page ID avoids re-fetching for section follow-ups. Successful `update` and `delete` evict the cache.
- Uses Confluence REST API v2 for all endpoints except v1 for CQL search, current user, and label add/remove (v2 has no equivalents).
- `confluence_read` gained an `inline_comments` resource (needs `page_id`); both comment resources render threaded, and every top-level entry is labelled with its ID and `(type: footer|inline)` — that label is the `comment_type` an agent feeds back to `reply_comment`.
- `confluence_write` gained `reply_comment`. Inline writes are replies only — no new anchored inline comments; that needs text-selection anchoring and is a follow-up.
- Replies send `parentCommentId` and omit `pageId` entirely — the Confluence spec makes the two mutually exclusive on both create models.
- Per-action field validation (`validateWriteItemFields` in `tool_write.go`): a field supplied to an action that does not use it is a hard error in both directions. One `WriteItem` struct backs every action and the tool schema is reflected from it, so validation is the only guard against a silent field-drop — which is what caused a real mis-post: `action: "comment"` with `comment_type: "inline"` silently posted a footer comment. `format` is rejected on `comment`, `edit_comment`, `delete`, `add_label`, and `remove_label`, which ignore it today.
- Child replies are fetched per thread, capped at 25 threads per read, with a truncation notice beyond that; a per-thread child-fetch failure degrades to a notice rather than failing the whole read.
- An inline-comment permalink (`focusedCommentId`) falls back to the inline endpoint when the footer fetch 404s.

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

Design docs and implementation plans live in `.ai-local-plans/`. That directory is untracked (it
carries a `.gitignore` of `*`), so they are local working notes rather than repo content — a fresh
clone has none. Where present, start with
`.ai-local-plans/2026-04-14-confluence-mcp-master-plan.md`.

## Quality Gates

Before considering any change complete, run all three:

```bash
task lint           # Must be 0 issues (includes go vet -tags smoke ./...)
task test           # All packages must pass
task build          # Must compile cleanly
```

Do not skip any of these. If lint or tests fail, fix before moving on. Smoke tests are opt-in and not part of the gate, but run them locally when changes touch the HTTP client, handlers, or converter.

**Formatting is `golangci-lint fmt`, not stock `gofmt`.** `task lint` reporting `0 issues` is the
authoritative gate; do not use `gofmt -l`, which disagrees with the project's formatter.

**Watch out: `task fmt` reformats nine files unrelated to whatever you are changing.** These files are
committed in a state the project's own formatter disagrees with (comment and struct-field alignment
only), and `task lint` does not flag them because `golangci-lint run` does not enforce the formatter:

```
internal/confluencemcp/append_preview.go     internal/mdconv/fixture_test.go
internal/confluencemcp/integration_test.go   internal/mdconv/macro_test.go
internal/confluencemcp/tool_read_test.go     internal/mdconv/preprocess.go
internal/confluencemcp/tool_write_append.go  internal/mdconv/testgen/fingerprint.go
internal/confluencemcp/tool_write_test.go
```

After running `task fmt`, check `git status` and revert any of the above you did not mean to touch, or
they add around 50 lines of whitespace noise to your diff. Fixing the drift is worth doing — as its
own formatting-only PR, never bundled into a feature change.

## CI

GitHub Actions runs lint, test (with `-race` and coverage), and a tag-gated smoke-test compile on push/PR to main. See `.github/workflows/ci.yml`. A separate `PR Title` workflow validates the PR title against Conventional Commits (`.github/workflows/pr-title.yml`). Release builds via GoReleaser on `v*` tags: see `.github/workflows/release.yml`.

## Releases

Releases are driven by [release-please](https://github.com/googleapis/release-please) (`.github/workflows/release-please.yml`). Every merge to `main` updates (or opens) a standing Release PR titled `chore(main): release X.Y.Z`, containing the changelog diff and version bump computed from Conventional Commits since the last tag. Merging the Release PR cuts the tag, which triggers GoReleaser.

**Conventional Commits are mandatory for PR titles** — they're the source of the changelog and version bump. Format:

```
<type>(optional-scope): <subject>
```

Types in use: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`, `build`, `perf`, `style`, `revert`. Use `feat!:` (or a `BREAKING CHANGE:` footer) for breaking changes. In pre-1.0 (`bump-minor-pre-major: true`, `bump-patch-for-minor-pre-major: true`), `feat:` bumps the patch and a breaking change bumps the minor — no accidental 1.0 cut.

A fine-grained PAT stored as `RELEASE_PAT` (Contents: R/W, owned by a repo admin) is required for release-please. `GITHUB_TOKEN` pushes don't trigger downstream workflows — the PAT is what makes the tag push fire `release.yml`.

## Distribution

Homebrew tap casks in `Casks/`, Docker image via distroless, and GitHub releases.
