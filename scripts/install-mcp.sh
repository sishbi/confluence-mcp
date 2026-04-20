#!/usr/bin/env zsh
set -euo pipefail

# Install the confluence-mcp server into Claude Code.
#
# Usage:
#   ./scripts/install-mcp.sh                     # install native (builds first, info logs)
#   ./scripts/install-mcp.sh --debug             # install native with debug logging
#   ./scripts/install-mcp.sh --brew              # install Homebrew binary (no build)
#   ./scripts/install-mcp.sh --brew --debug      # Homebrew binary with debug logging
#   ./scripts/install-mcp.sh --docker            # install Docker variant (no build)
#   ./scripts/install-mcp.sh --docker --debug    # Docker variant with debug logging
#   ./scripts/install-mcp.sh --remove            # uninstall native/brew (same server name)
#   ./scripts/install-mcp.sh --docker --remove   # uninstall Docker variant
#
# Scope flags (combine with any install mode):
#   --user        register user-wide (all projects; default is local/project-scoped)
#
# Required env vars (set in your shell profile or .envrc):
#   CONFLUENCE_URL        e.g. https://your-instance.atlassian.net
#   CONFLUENCE_EMAIL      e.g. you@example.com
#   CONFLUENCE_API_TOKEN  your Confluence API token
#
# Optional env vars (Docker mode):
#   CONFLUENCE_MCP_IMAGE  override image (default: sishbi/confluence-mcp:latest)
#
# Logs are always written to /tmp/confluence-mcp.log (override with
# CONFLUENCE_MCP_LOG_FILE). Tail them with: tail -f /tmp/confluence-mcp.log

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$PROJECT_DIR/bin/confluence-mcp"
WRAPPER="$SCRIPT_DIR/confluence-mcp-wrapper.sh"
DOCKER_RUN="$SCRIPT_DIR/docker-run.sh"

MODE="native"
ACTION="install"
LOG_LEVEL="info"
SCOPE="local"

for arg in "$@"; do
    case "$arg" in
        --docker) MODE="docker" ;;
        --brew)   MODE="brew" ;;
        --debug)  LOG_LEVEL="debug" ;;
        --remove) ACTION="remove" ;;
        --user)   SCOPE="user" ;;
        *) echo "ERROR: unknown argument: $arg" >&2; exit 1 ;;
    esac
done

# Native and brew share the server name (confluence-mcp) — switching modes
# re-registers cleanly because the install step removes any prior entry first.
if [[ "$MODE" == "docker" ]]; then
    SERVER_NAME="confluence-mcp-docker"
    LAUNCHER="$DOCKER_RUN"
else
    SERVER_NAME="confluence-mcp"
    LAUNCHER="$WRAPPER"
fi

if [[ "$ACTION" == "remove" ]]; then
    echo "Removing $SERVER_NAME from Claude Code (scope: $SCOPE)..."
    claude mcp remove --scope "$SCOPE" "$SERVER_NAME"
    echo "Done."
    exit 0
fi

# Validate env vars.
for var in CONFLUENCE_URL CONFLUENCE_EMAIL CONFLUENCE_API_TOKEN; do
    # shellcheck disable=SC2296
    if [[ -z "${(P)var:-}" ]]; then
        echo "ERROR: $var is not set" >&2
        exit 1
    fi
done

# Native mode builds the binary; Docker mode relies on a pulled/built image;
# brew mode resolves the installed binary on PATH.
BREW_BINARY=""
if [[ "$MODE" == "native" ]]; then
    echo "Building $BINARY..."
    (cd "$PROJECT_DIR" && task build)
elif [[ "$MODE" == "brew" ]]; then
    if ! BREW_BINARY="$(command -v confluence-mcp 2>/dev/null)"; then
        echo "ERROR: confluence-mcp not found on PATH — run 'brew install --cask confluence-mcp' first" >&2
        exit 1
    fi
    echo "Using Homebrew binary: $BREW_BINARY"
fi

# Remove existing registration at the target scope (ignore error if not present).
claude mcp remove --scope "$SCOPE" "$SERVER_NAME" 2>/dev/null || true

LOG_FILE="${CONFLUENCE_MCP_LOG_FILE:-/tmp/confluence-mcp.log}"
touch "$LOG_FILE"
echo "Registering $SERVER_NAME in Claude Code (mode: $MODE, scope: $SCOPE, log level: $LOG_LEVEL, logs: $LOG_FILE)..."

typeset -a env_args
env_args=(
    -e "CONFLUENCE_URL=$CONFLUENCE_URL"
    -e "CONFLUENCE_EMAIL=$CONFLUENCE_EMAIL"
    -e "CONFLUENCE_API_TOKEN=$CONFLUENCE_API_TOKEN"
    -e "CONFLUENCE_MCP_LOG_LEVEL=$LOG_LEVEL"
    -e "CONFLUENCE_MCP_LOG_FILE=$LOG_FILE"
)
if [[ -n "${CONFLUENCE_MCP_IMAGE:-}" ]]; then
    env_args+=(-e "CONFLUENCE_MCP_IMAGE=$CONFLUENCE_MCP_IMAGE")
fi
if [[ -n "$BREW_BINARY" ]]; then
    env_args+=(-e "CONFLUENCE_MCP_BINARY=$BREW_BINARY")
fi

claude mcp add --scope "$SCOPE" "${env_args[@]}" -- "$SERVER_NAME" "$LAUNCHER"

echo "Installed. Tail logs with: tail -f $LOG_FILE"
