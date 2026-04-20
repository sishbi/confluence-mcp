#!/usr/bin/env zsh
# Wrapper that launches confluence-mcp and redirects logs to a file.
# Used by install-mcp.sh when --debug is passed.
#
# The MCP protocol uses stdin/stdout, so stderr is free for logging.
# Logs are appended (not truncated) so you can tail -f during a session.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="$SCRIPT_DIR/../bin/confluence-mcp"
LOG_FILE="${CONFLUENCE_MCP_LOG_FILE:-/tmp/confluence-mcp.log}"
LOG_LEVEL="${CONFLUENCE_MCP_LOG_LEVEL:-info}"

exec "$BINARY" -log-level "$LOG_LEVEL" 2>>"$LOG_FILE"
