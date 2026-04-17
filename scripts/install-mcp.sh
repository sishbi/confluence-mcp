#!/usr/bin/env bash
set -euo pipefail

# Install the confluence-mcp server into Claude Code.
#
# Usage:
#   ./scripts/install-mcp.sh              # install (builds first, logs at info level)
#   ./scripts/install-mcp.sh --debug      # install with debug-level logging
#   ./scripts/install-mcp.sh --remove     # uninstall
#
# Required env vars (set in your shell profile or .envrc):
#   CONFLUENCE_URL        e.g. https://your-instance.atlassian.net
#   CONFLUENCE_EMAIL      e.g. you@example.com
#   CONFLUENCE_API_TOKEN  your Confluence API token
#
# Logs are always written to /tmp/confluence-mcp.log (override with
# CONFLUENCE_MCP_LOG_FILE). Tail them with: tail -f /tmp/confluence-mcp.log

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$PROJECT_DIR/bin/confluence-mcp"
WRAPPER="$SCRIPT_DIR/confluence-mcp-wrapper.sh"
SERVER_NAME="confluence-mcp"
LOG_LEVEL="info"

if [[ "${1:-}" == "--remove" ]]; then
    echo "Removing $SERVER_NAME from Claude Code..."
    claude mcp remove "$SERVER_NAME"
    echo "Done."
    exit 0
fi

if [[ "${1:-}" == "--debug" ]]; then
    LOG_LEVEL="debug"
fi

# Validate env vars.
for var in CONFLUENCE_URL CONFLUENCE_EMAIL CONFLUENCE_API_TOKEN; do
    if [[ -z "${!var:-}" ]]; then
        echo "ERROR: $var is not set" >&2
        exit 1
    fi
done

# Build.
echo "Building $BINARY..."
(cd "$PROJECT_DIR" && task build)

# Remove existing registration (ignore error if not present).
claude mcp remove "$SERVER_NAME" 2>/dev/null || true

# Register with wrapper so logs always go to a file.
LOG_FILE="${CONFLUENCE_MCP_LOG_FILE:-/tmp/confluence-mcp.log}"
touch "$LOG_FILE"
echo "Registering $SERVER_NAME in Claude Code (log level: $LOG_LEVEL, logs: $LOG_FILE)..."
claude mcp add \
    -e "CONFLUENCE_URL=$CONFLUENCE_URL" \
    -e "CONFLUENCE_EMAIL=$CONFLUENCE_EMAIL" \
    -e "CONFLUENCE_API_TOKEN=$CONFLUENCE_API_TOKEN" \
    -e "CONFLUENCE_MCP_LOG_LEVEL=$LOG_LEVEL" \
    -e "CONFLUENCE_MCP_LOG_FILE=$LOG_FILE" \
    -- "$SERVER_NAME" "$WRAPPER"

echo "Installed. Tail logs with: tail -f $LOG_FILE"
