#!/usr/bin/env zsh
set -euo pipefail

# Run the confluence-mcp Docker image as a stdio MCP server.
#
# Usage:
#   ./scripts/docker-run.sh [extra args passed to the binary]
#
# Required env vars:
#   CONFLUENCE_URL        e.g. https://your-instance.atlassian.net
#   CONFLUENCE_EMAIL      e.g. you@example.com
#   CONFLUENCE_API_TOKEN  your Confluence API token
#
# Optional env vars:
#   CONFLUENCE_MCP_IMAGE  override the image (default: sishbi/confluence-mcp:latest)

IMAGE="${CONFLUENCE_MCP_IMAGE:-sishbi/confluence-mcp:latest}"
LOG_FILE="${CONFLUENCE_MCP_LOG_FILE:-/tmp/confluence-mcp.log}"
LOG_LEVEL="${CONFLUENCE_MCP_LOG_LEVEL:-info}"

for var in CONFLUENCE_URL CONFLUENCE_EMAIL CONFLUENCE_API_TOKEN; do
    if [[ -z "${(P)var:-}" ]]; then
        echo "ERROR: $var is not set" >&2
        exit 1
    fi
done

# MCP uses stdin/stdout for protocol, so stderr is free for container logs.
# Redirect to the same log file used by the native wrapper so `tail -f` works
# identically across install modes.
exec docker run -i --rm \
    -e "CONFLUENCE_URL=$CONFLUENCE_URL" \
    -e "CONFLUENCE_EMAIL=$CONFLUENCE_EMAIL" \
    -e "CONFLUENCE_API_TOKEN=$CONFLUENCE_API_TOKEN" \
    "$IMAGE" -log-level "$LOG_LEVEL" "$@" 2>>"$LOG_FILE"
