@echo off
REM Windshift test startup script for Windows
REM Creates a fresh instance with SQLite backend on port 8080

set PORT=%1
if "%PORT%"=="" set PORT=8080

REM Start the server
windshift.exe -port %PORT% -db windshift-test.db -attachment-path data-test\ -no-csrf -allow-insecure-http
