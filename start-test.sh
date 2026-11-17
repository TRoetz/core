#!/bin/bash
set -euo pipefail

echo "=== Windshift Test Instance ==="
echo ""

# Configuration
PORT=8085
DB_PATH="windshift-test.db"
ATTACHMENT_PATH="data-test/"
SSO_SECRET="test-secret-for-local-testing"
BASE_URL="http://localhost:${PORT}"

ADMIN_USERNAME="admin"
ADMIN_EMAIL="admin@example.com"
ADMIN_PASSWORD="admin123!"

echo "Starting Windshift test server on port ${PORT}..."
echo ""

# Start the server in background with test config
SSO_SECRET="$SSO_SECRET" BASE_URL="$BASE_URL" ./windshift.exe \
    -port "$PORT" \
    -db "$DB_PATH" \
    -attachment-path "$ATTACHMENT_PATH/" \
    -no-csrf \
    -allow-insecure-http > test-server.log 2>&1 &

SERVER_PID=$!
echo "Server PID: $SERVER_PID"
echo ""

# Wait for server to be ready (up to 30 seconds)
echo "Waiting for server to start..."
for i in {1..30}; do
    if curl -s http://localhost:$PORT/api/setup/status >/dev/null 2>&1; then
        echo "Server is ready!"
        break
    fi
    sleep 1
    
    # Check if server process is still alive
    if ! kill -0 $SERVER_PID 2>/dev/null; then
        echo ""
        echo "ERROR: Server failed to start. Checking logs..."
        tail -n 20 test-server.log
        exit 1
    fi
    
    printf "."
done

echo ""
echo ""

# Check setup status and create admin user if needed
STATUS=$(curl -s http://localhost:$PORT/api/setup/status)

if echo "$STATUS" | grep -q '"setup_completed":false'; then
    echo "Creating admin user (username: $ADMIN_USERNAME, password: $ADMIN_PASSWORD)..."
    
    SETUP_RESPONSE=$(curl -s -X POST http://localhost:$PORT/api/setup/complete \
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
        echo "Admin user created successfully!"
    else
        echo ""
        echo "ERROR: Failed to create admin user:"
        echo "$SETUP_RESPONSE"
        exit 1
    fi
else
    echo "Setup already completed. Admin user exists."
fi

echo ""
echo "=== Windshift is running! ==="
echo "URL:      http://localhost:${PORT}"
echo "Username: $ADMIN_USERNAME"
echo "Password: $ADMIN_PASSWORD"
echo ""
echo "To stop: kill process with PID $SERVER_PID (or Ctrl+C if foreground)"
