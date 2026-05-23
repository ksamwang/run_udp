@echo off
setlocal EnableExtensions EnableDelayedExpansion

if "%~1"=="" (
  echo Usage: release.bat ^<version^> [release-notes-file]
  echo PowerShell example: .\release.bat 1.0.0
  echo PowerShell example: .\release.bat v1.0.0 CHANGELOG.md
  exit /b 1
)

set ROOT=%~dp0
set VERSION=%~1
if /I "%VERSION:~0,1%"=="v" (
  set TAG=%VERSION%
) else (
  set TAG=v%VERSION%
)
if /I "%VERSION:~0,1%"=="v" set VERSION=%VERSION:~1%
set NOTES_FILE=%~2
set DIST=%ROOT%dist
set RELEASE_DIR=%DIST%\release-%TAG%
set FRONTEND_ZIP=%RELEASE_DIR%\frontend-admin-%TAG%.zip
set SERVER_ZIP=%RELEASE_DIR%\server-linux-amd64-%TAG%.zip
set CLIENT_ZIP=%RELEASE_DIR%\client-windows-amd64-%TAG%.zip
set LAN_ZIP=%RELEASE_DIR%\UDPTunnelLAN-windows-amd64-%TAG%.zip
set COMMIT=

where git >nul 2>nul || (echo ERROR: git not found & exit /b 1)
where gh >nul 2>nul || (echo ERROR: GitHub CLI gh not found & exit /b 1)
where go >nul 2>nul || (echo ERROR: go not found & exit /b 1)

gh auth status >nul 2>nul
if errorlevel 1 (
  echo ERROR: GitHub CLI is not logged in. Run: gh auth login
  exit /b 1
)

for /f "usebackq delims=" %%i in (`git status --short`) do (
  echo ERROR: working tree is not clean. Commit or stash changes first.
  git status --short
  exit /b 1
)

for /f "usebackq delims=" %%i in (`git rev-parse --short HEAD`) do set COMMIT=%%i

git fetch --tags
if errorlevel 1 exit /b 1
git rev-parse -q --verify "refs/tags/%TAG%" >nul
if not errorlevel 1 (
  echo ERROR: tag %TAG% already exists.
  exit /b 1
)

echo === Building release %TAG% commit %COMMIT% ===
set VERSION=%VERSION%
set COMMIT=%COMMIT%
call "%ROOT%build-all.bat"
if errorlevel 1 exit /b 1

if not exist "%RELEASE_DIR%" mkdir "%RELEASE_DIR%"

if exist "%FRONTEND_ZIP%" del /Q "%FRONTEND_ZIP%"
if exist "%SERVER_ZIP%" del /Q "%SERVER_ZIP%"
if exist "%CLIENT_ZIP%" del /Q "%CLIENT_ZIP%"
if exist "%LAN_ZIP%" del /Q "%LAN_ZIP%"

echo === Packaging assets ===
powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%DIST%\frontend-admin\*' -DestinationPath '%FRONTEND_ZIP%' -Force"
if errorlevel 1 exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%DIST%\server','%DIST%\.env.example' -DestinationPath '%SERVER_ZIP%' -Force"
if errorlevel 1 exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%DIST%\client.exe','%DIST%\client.json.example' -DestinationPath '%CLIENT_ZIP%' -Force"
if errorlevel 1 exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%DIST%\UDPTunnelLAN.exe','%DIST%\wintun.dll','%DIST%\lan.json.example' -DestinationPath '%LAN_ZIP%' -Force"
if errorlevel 1 exit /b 1

set ASSETS="%FRONTEND_ZIP%" "%SERVER_ZIP%" "%CLIENT_ZIP%" "%LAN_ZIP%"
if exist "%DIST%\udp-tunnel-client-%VERSION%-setup.exe" set ASSETS=%ASSETS% "%DIST%\udp-tunnel-client-%VERSION%-setup.exe"
if exist "%DIST%\udp-tunnel-lan-%VERSION%-setup.exe" set ASSETS=%ASSETS% "%DIST%\udp-tunnel-lan-%VERSION%-setup.exe"
if exist "%DIST%\latest.json" set ASSETS=%ASSETS% "%DIST%\latest.json"

echo === Creating git tag %TAG% ===
git tag -a "%TAG%" -m "Release %TAG%"
if errorlevel 1 exit /b 1
git push origin "%TAG%"
if errorlevel 1 exit /b 1

echo === Creating GitHub release %TAG% ===
if "%NOTES_FILE%"=="" (
  gh release create "%TAG%" %ASSETS% --title "%TAG%" --generate-notes
) else (
  gh release create "%TAG%" %ASSETS% --title "%TAG%" --notes-file "%NOTES_FILE%"
)
if errorlevel 1 exit /b 1

echo.
echo === Release published: %TAG% ===
gh release view "%TAG%" --web
exit /b 0
