#!/usr/bin/env bash
set -euo pipefail

# start-with-mcp.sh — start Windshift local dev with the built-in MCP server.
#
# Thin wrapper around local-dev.sh --mcp: backend on :7777 (SQLite) with the
# MCP server mounted at /mcp, Vite UI on :5555. All other flags pass through:
#   ./start-with-mcp.sh [--db <path>] [--fresh] [--open]

cd "$(dirname "$0")"
exec ./local-dev.sh --mcp "$@"
