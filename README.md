# Confluence MCP

**Note:** This tool was heavily inspired by [mmatczuk/jira-mcp](https://github.com/mmatczuk/jira-mcp), following the same design principles but for Confluence.

Give your AI agent full Confluence access with just 2 tools.

| Tool | What it does |
|---|---|
| `confluence_read` | Get pages by ID/URL, search via CQL, list spaces/children/comments/inline comments/labels |
| `confluence_write` | Create, update, delete pages; add/edit/reply to comments; manage labels. Batch + dry_run |

## Philosophy

- **2 tools, not 72** — the server knows the API so the LLM doesn't have to
- **Credentials stay local** — Basic auth via env vars, no OAuth dance
- **Smart by default** — long pages are automatically chunked with a table of contents; section requests fetch the page on a cold cache and are served from cache thereafter

## Features

### Reading
- **URL parsing** — paste any Confluence page URL, including `?focusedCommentId=` deep-links
- **CQL search** — arbitrary CQL queries via the v1 search endpoint
- **Resource listings** — spaces, children, comments, inline comments, labels
- **Adaptive chunking** — long pages return a TOC + first chunk; request sections individually by heading. `section` accepts either `page_id` or a single-element `page_ids`, needs exactly one page id resolved, and fetches the page itself on a cold cache
- **`next_page_token` cursor** — base64url JSON cursor for section-index or byte-offset paging; accepts `page_id` or `page_ids` alongside it, but the id(s) must agree with the page the token already carries — a mismatch or more than one id is rejected. A `url` naming the same page continues the read too (a `focusedCommentId` permalink does not); `cql` and `resource` listings use the field as their own pagination cursor. Re-fetches silently if the cache has evicted
- **60-second page cache** — eliminates redundant API calls for section follow-ups; evicted on write
- **Raw storage access** — `format="storage"` returns Confluence XHTML when you need to inspect or hand-edit macros

### Writing
- **Markdown in, storage format out** — body fields auto-convert to Confluence XHTML
- **Raw storage passthrough** — set `format="storage"` on a create, update, append, or reply_comment item to push XHTML directly (for macro authoring); comment, edit_comment, delete, add_label, and remove_label reject it
- **Partial-page `append` action** — insert at the end of the page, at the top of a named heading's section (`after_heading`), at the bottom of it (`end_of_section`), replace the section (`replace_section`, which keeps the section's subsections unless `include_subsections: true`, and can rename the heading via `new_heading`), or edit the preamble above the first heading (`start` to insert at the very top of the page, `replace_preamble` to replace everything before the first heading). A new sibling section wants `end_of_section`: `after_heading` would displace the target section's own body into it. `start` and `replace_preamble` take no `heading` — on a multi-column page `start` writes into the first layout cell and `end` into the last; `replace_preamble` fails with `no_heading_on_page` if the CONTAINER — the first layout cell on a layout-wrapped page, the whole page otherwise — has no locatable heading, which is not the same as the whole page having none (use `update` for a full-body replace there). `replace_preamble` and `replace_section` also refuse with `preamble_boundary_unbalanced` / `section_boundary_unbalanced` when a plain wrapper element in the replaced range opens before it ends but does not close before it (use `update` instead — replacing would orphan its closing tag). The agent sends only the fragment; the server fetches the current body, splices, and writes the merged result. Typical edits ship a payload ~100× smaller than `update` (observed ~147 B vs ~34 KB on a representative fixture), cutting both wall-clock and token cost. Success responses report fragment size and base→merged body bytes so the saving is visible in telemetry. Retries once on 409 from read-replica lag when no version is pinned.
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
claude mcp add confluence -- confluence-mcp
```

Or use the install script above, which wraps the binary so stderr logs land in a file you can `tail -f`.

### 5. Verify

Ask Claude: "List my Confluence spaces"

## Releasing

See [RELEASING.md](RELEASING.md).

## License

MIT
