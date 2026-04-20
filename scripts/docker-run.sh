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

for var in CONFLUENCE_URL CONFLUENCE_EMAIL CONFLUENCE_API_TOKEN; do
    if [[ -z "${(P)var:-}" ]]; then
        echo "ERROR: $var is not set" >&2
        exit 1
    fi
done

exec docker run -i --rm \
    -e "CONFLUENCE_URL=$CONFLUENCE_URL" \
    -e "CONFLUENCE_EMAIL=$CONFLUENCE_EMAIL" \
    -e "CONFLUENCE_API_TOKEN=$CONFLUENCE_API_TOKEN" \
    "$IMAGE" "$@"
