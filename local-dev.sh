#!/usr/bin/env bash
set -euo pipefail

# local-dev.sh — start Windshift for localhost + local browser testing.
#
#   Backend API : http://localhost:7777  (SQLite, curl-able)
#   Browser UI  : http://localhost:5555  (Vite dev server, HMR; proxies /api -> 7777)
#
# Usage: ./local-dev.sh [--db <path>] [--fresh] [--open]
#   --db <path>  use a different SQLite DB file (default windshift.db)
#   --fresh      wipe the DB and data/ before starting (clean slate + setup wizard)
#   --open       open the browser UI automatically once ready
#
# API-only testing without the Vite server: use dev.sh instead.

PORT=7777
UI_PORT=5555
DB_PATH="windshift.db"
ATTACHMENT_PATH="data/"
SSO_SECRET="dev-secret-for-testing"
LLM_ENDPOINT="http://localhost:1234"

ADMIN_USERNAME="admin"
ADMIN_EMAIL="admin@example.com"
ADMIN_PASSWORD="admin123!"

FRESH=0
OPEN_BROWSER=0
while [ $# -gt 0 ]; do
  case "$1" in
    --fresh) FRESH=1 ;;
    --open) OPEN_BROWSER=1 ;;
    --db)
      shift
      if [ $# -eq 0 ]; then echo "ERROR: --db requires a path" >&2; exit 1; fi
      DB_PATH="$1"
      ;;
    -h|--help) sed -n '4,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; echo "Usage: $0 [--db <path>] [--fresh] [--open]" >&2; exit 1 ;;
  esac
  shift
done

PIDS=()
cleanup() {
  echo ""
  echo "Shutting down..."
  for pid in "${PIDS[@]}"; do
    # PIDs were started with setsid, so negative PID kills the whole group (npm + vite children).
    kill -- "-$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  rm -f .dev-windshift
  echo "Stopped."
}
trap cleanup EXIT
trap 'echo ""; echo "Interrupted."; exit 130' INT TERM

port_in_use() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

# --- Kill previous dev instance if running (backend + orphaned Vite) ---
# Must run before the port checks below: a leftover instance holds both ports,
# and an orphaned Vite (e.g. after a hard terminal exit) does not match the
# .dev-windshift pattern.
if pgrep -f '\.dev-windshift' >/dev/null 2>&1 || pgrep -f 'frontend/node_modules/\.bin/vite' >/dev/null 2>&1; then
  echo "Stopping previous dev instance..."
  pkill -f '\.dev-windshift' 2>/dev/null || true
  pkill -f 'frontend/node_modules/\.bin/vite' 2>/dev/null || true
  for _ in $(seq 1 10); do
    if ! port_in_use "$PORT" && ! port_in_use "$UI_PORT"; then break; fi
    sleep 1
  done
fi

if port_in_use "$PORT"; then
  echo "ERROR: port $PORT is already in use." >&2
  exit 1
fi
if port_in_use "$UI_PORT"; then
  echo "ERROR: port $UI_PORT is already in use (Vite dev server)." >&2
  exit 1
fi

# --- Optionally wipe the SQLite DB and attachments ---
if [ "$FRESH" = "1" ]; then
  echo "Wiping fresh: removing $DB_PATH and $ATTACHMENT_PATH..."
  rm -f "$DB_PATH" "${DB_PATH}-shm" "${DB_PATH}-wal"
  rm -rf "$ATTACHMENT_PATH"
fi

mkdir -p "$ATTACHMENT_PATH"

# --- Build the Go server (SQLite) ---
echo "Building windshift server..."
go build -o .dev-windshift .

# --- Start backend ---
echo "Starting windshift backend on :$PORT (SQLite)..."
SSO_SECRET="$SSO_SECRET" LLM_ENDPOINT="$LLM_ENDPOINT" \
  setsid ./.dev-windshift \
    -port "$PORT" \
    -db "$DB_PATH" \
    -attachment-path "$ATTACHMENT_PATH" \
    -no-csrf \
    -log-level debug > local-dev-server.log 2>&1 &
BACKEND_PID=$!
PIDS+=("$BACKEND_PID")

# Wait for backend readiness (up to 60 s)
echo "Waiting for backend..."
ready=0
for i in $(seq 1 60); do
  if curl -s "http://localhost:$PORT/api/setup/status" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
    echo "" >&2
    echo "ERROR: backend failed to start. Last log lines:" >&2
    tail -n 20 local-dev-server.log >&2 || true
    exit 1
  fi
  printf "."
  sleep 1
done
if [ "$ready" != "1" ]; then
  echo "" >&2
  echo "ERROR: backend did not become ready within 60 s. Last log lines:" >&2
  tail -n 20 local-dev-server.log >&2 || true
  exit 1
fi
echo " ready."

# --- Create admin user if setup has not been completed yet ---
STATUS=$(curl -s "http://localhost:$PORT/api/setup/status")
if echo "$STATUS" | grep -q '"setup_completed":false'; then
  echo "Setup pending — creating admin user ($ADMIN_USERNAME / $ADMIN_PASSWORD)..."
  SETUP_RESPONSE=$(curl -s -X POST "http://localhost:$PORT/api/setup/complete" \
    -H "Content-Type: application/json" \
    -d "{
        \"admin_user\": {
            \"email\": \"$ADMIN_EMAIL\",
            \"username\": \"$ADMIN_USERNAME\",
            \"first_name\": \"Admin\",
            \"last_name\": \"User\",
            \"language\": \"en\",
            \"password\": \"$ADMIN_PASSWORD\"
        },
        \"module_settings\": {}
    }")
  if echo "$SETUP_RESPONSE" | grep -q '"success":true'; then
    echo "Admin user created."
  else
    echo "" >&2
    echo "ERROR: failed to create admin user:" >&2
    echo "$SETUP_RESPONSE" >&2
    exit 1
  fi
else
  echo "Setup already completed — using existing users."
fi

# --- Start Vite dev server for browser testing ---
if [ ! -d frontend/node_modules ]; then
  echo "frontend/node_modules missing — running npm ci..."
  (cd frontend && npm ci)
fi

echo "Starting Vite dev server on :$UI_PORT..."
(cd frontend && exec setsid npm run dev > ../local-dev-vite.log 2>&1) &
VITE_PID=$!
PIDS+=("$VITE_PID")

# Wait for Vite readiness (first start can be slow due to dep pre-bundling; up to 90 s)
echo "Waiting for Vite..."
ready=0
for i in $(seq 1 90); do
  if curl -s -o /dev/null "http://localhost:$UI_PORT/"; then
    ready=1
    break
  fi
  if ! kill -0 "$VITE_PID" 2>/dev/null; then
    echo "" >&2
    echo "ERROR: Vite failed to start. Last log lines:" >&2
    tail -n 20 local-dev-vite.log >&2 || true
    exit 1
  fi
  printf "."
  sleep 1
done
if [ "$ready" != "1" ]; then
  echo "" >&2
  echo "ERROR: Vite did not become ready within 90 s. Last log lines:" >&2
  tail -n 20 local-dev-vite.log >&2 || true
  exit 1
fi
echo " ready."

if [ "$OPEN_BROWSER" = "1" ]; then
  if command -v xdg-open >/dev/null 2>&1; then
    xdg-open "http://localhost:$UI_PORT" >/dev/null 2>&1 || true
  elif command -v open >/dev/null 2>&1; then
    open "http://localhost:$UI_PORT" >/dev/null 2>&1 || true
  fi
fi

echo ""
echo "=== Windshift local dev is running ==="
echo "Browser UI : http://localhost:${UI_PORT}   (Vite, HMR)"
echo "Backend API: http://localhost:${PORT}/api  (e.g. /api/setup/status)"
if echo "$STATUS" | grep -q '"setup_completed":false'; then
  echo "Login      : $ADMIN_USERNAME / $ADMIN_PASSWORD"
fi
echo "Logs       : local-dev-server.log, local-dev-vite.log"
echo "Press Ctrl+C to stop."

wait "${PIDS[@]}" || true
