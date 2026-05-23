param(
  [string]$Version = "0.14.1",
  [string]$OutputDir = "dist"
)

$ErrorActionPreference = "Stop"

$expectedSHA256 = "07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"
$url = "https://www.wintun.net/builds/wintun-$Version.zip"
$cacheDir = Join-Path $env:TEMP "udp-tunnel-wintun"
$zipPath = Join-Path $cacheDir "wintun-$Version.zip"
$outputPath = Join-Path $OutputDir "wintun.dll"

if (!(Test-Path $cacheDir)) {
  New-Item -ItemType Directory -Path $cacheDir | Out-Null
}
if (!(Test-Path $OutputDir)) {
  New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

if (!(Test-Path $zipPath)) {
  Invoke-WebRequest -Uri $url -OutFile $zipPath
}

function Get-SHA256Hex([string]$Path) {
  if (Get-Command Get-FileHash -ErrorAction SilentlyContinue) {
    return (Get-FileHash $Path -Algorithm SHA256).Hash.ToLowerInvariant()
  }
  $stream = [System.IO.File]::OpenRead($Path)
  try {
    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    try {
      $hash = $sha256.ComputeHash($stream)
      return (($hash | ForEach-Object { $_.ToString("x2") }) -join "")
    } finally {
      $sha256.Dispose()
    }
  } finally {
    $stream.Dispose()
  }
}

$actualSHA256 = Get-SHA256Hex $zipPath
if ($actualSHA256 -ne $expectedSHA256) {
  Remove-Item -LiteralPath $zipPath -Force -ErrorAction SilentlyContinue
  throw "wintun-$Version.zip sha256 mismatch: expected $expectedSHA256, got $actualSHA256"
}

Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [System.IO.Compression.ZipFile]::OpenRead($zipPath)
try {
  $entry = $archive.GetEntry("wintun/bin/amd64/wintun.dll")
  if ($null -eq $entry) {
    throw "wintun/bin/amd64/wintun.dll not found in $zipPath"
  }
  [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $outputPath, $true)
} finally {
  $archive.Dispose()
}

Write-Host "Wintun runtime ready: $outputPath"
