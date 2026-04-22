@echo off
setlocal

set DIST=%~dp0dist
if not exist "%DIST%" mkdir "%DIST%"

echo === Building server (linux/amd64) ===
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w" -o "%DIST%\server" .\server
if errorlevel 1 goto :fail

echo === Building client (windows/amd64) ===
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w" -o "%DIST%\client.exe" .\client
if errorlevel 1 goto :fail

echo.
echo === Done ===
dir /b "%DIST%"
exit /b 0

:fail
echo.
echo BUILD FAILED
exit /b 1
