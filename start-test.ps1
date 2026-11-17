# Windshift Test Instance Startup Script (PowerShell)
# Creates a test instance on port 8085 with admin credentials.

$Port = 8085
$DbPath = "windshift-test.db"
$AttachmentPath = "data-test\"
$SsoSecret = "test-secret-for-local-testing"
$BaseUrl = "http://localhost:$Port"

$AdminUsername = "admin"
$AdminEmail = "admin@example.com"
$AdminPassword = "admin123!"

Write-Host "=== Windshift Test Instance ==="
Write-Host ""

# Set environment variables for the process
$env:SSO_SECRET = $SsoSecret
$env:BASE_URL = $BaseUrl

Write-Host "Starting Windshift test server on port $Port..."
Write-Host ""

# Start the server in background with test config
$process = Start-Process -FilePath "./windshift.exe" `
    -ArgumentList "-port $Port", "-db $DbPath", "-attachment-path $AttachmentPath", "-no-csrf", "-allow-insecure-http" `
    -RedirectStandardOutput "test-server.log" `
    -RedirectStandardError "test-server-error.log" `
    -PassThru

Write-Host "Server PID: $($process.Id)"
Write-Host ""

# Wait for server to be ready (up to 30 seconds)
Write-Host "Waiting for server to start..."
$ready = $false
for ($i = 1; $i -le 30; $i++) {
    try {
        Invoke-WebRequest -Uri "$BaseUrl/api/setup/status" -UseBasicParsing | Out-Null
        Write-Host "Server is ready!"
        $ready = $true
        break
    } catch {
        # Check if process is still alive
        if ($process.HasExited) {
            Write-Host ""
            Write-Host "ERROR: Server failed to start. Checking logs..."
            Get-Content "test-server.log" -Tail 20
            exit 1
        }
        Start-Sleep -Seconds 1
        Write-Host "." -NoNewline
    }
}

if (-not $ready) {
    Write-Host ""
    Write-Host "ERROR: Server did not start within timeout."
    exit 1
}

Write-Host ""
Write-Host ""

# Check setup status and create admin user if needed
$Status = Invoke-RestMethod -Uri "$BaseUrl/api/setup/status"

if ($Status.setup_completed -eq $false) {
    Write-Host "Creating admin user (username: $AdminUsername, password: $AdminPassword)..."
    
    $SetupBody = @{
        admin_user = @{
            email = $AdminEmail
            username = $AdminUsername
            first_name = "Admin"
            last_name = "User"
            language = "en"
            password = $AdminPassword
        }
        module_settings = @{}
    } | ConvertTo-Json -Depth 10
    
    try {
        $SetupResponse = Invoke-RestMethod -Uri "$BaseUrl/api/setup/complete" `
            -Method POST `
            -ContentType "application/json" `
            -Headers @{ Origin = $BaseUrl; Referer = "$BaseUrl/" } `
            -Body $SetupBody
        
        Write-Host "Admin user created successfully!"
    } catch {
        Write-Host ""
        Write-Host "ERROR: Failed to create admin user:"
        Write-Host $_.Exception.Message
        exit 1
    }
} else {
    Write-Host "Setup already completed. Admin user exists."
}

Write-Host ""
Write-Host "=== Windshift is running! ==="
Write-Host "URL:      $BaseUrl"
Write-Host "Username: $AdminUsername"
Write-Host "Password: $AdminPassword"
Write-Host ""
Write-Host "To stop: Stop process with PID $($process.Id)"
