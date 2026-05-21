@echo off
setlocal

set DIST=%~dp0dist
if not exist "%DIST%" mkdir "%DIST%"
set VERSION=%VERSION%
if "%VERSION%"=="" set VERSION=dev
set COMMIT=%COMMIT%
if "%COMMIT%"=="" set COMMIT=local
for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')"`) do set BUILD_TIME=%%i
set LDFLAGS=-X main.Version=%VERSION% -X main.Commit=%COMMIT% -X main.BuildTime=%BUILD_TIME%

echo === Building server (linux/amd64) ===
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags "%LDFLAGS% -s -w" -o "%DIST%\server" .\cmd\server
if errorlevel 1 goto :fail

echo === Building client (windows/amd64) ===
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags "%LDFLAGS% -H=windowsgui -s -w" -o "%DIST%\client.exe" .\cmd\client
if errorlevel 1 goto :fail
copy /Y client.json.example "%DIST%\client.json.example" >nul
if exist "%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe" (
  echo === Building installer (Inno Setup) ===
  "%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe" /DMyAppVersion=%VERSION% /DMySourceDir="%DIST%" installer\client.iss
) else (
  echo WARN: Inno Setup compiler not found, skipping installer build
)
if exist "installer\Output\udp-tunnel-client-%VERSION%-setup.exe" (
  copy /Y "installer\Output\udp-tunnel-client-%VERSION%-setup.exe" "%DIST%\udp-tunnel-client-%VERSION%-setup.exe" >nul
  for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Get-FileHash '%DIST%\udp-tunnel-client-%VERSION%-setup.exe' -Algorithm SHA256).Hash.ToLower()"`) do set SETUP_SHA=%%i
  > "%DIST%\latest.json" echo {^
  "version":"%VERSION%",^
  "url":"",^
  "sha256":"%SETUP_SHA%",^
  "published_at":"%BUILD_TIME%",^
  "notes":"",^
  "minimum_supported_version":""^
}
)

echo.
echo === Done ===
dir /b "%DIST%"
exit /b 0

:fail
echo.
echo BUILD FAILED
exit /b 1
