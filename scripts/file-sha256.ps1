param(
  [Parameter(Mandatory = $true)]
  [string]$Path
)

$ErrorActionPreference = "Stop"

if (Get-Command Get-FileHash -ErrorAction SilentlyContinue) {
  Write-Output (Get-FileHash $Path -Algorithm SHA256).Hash.ToLowerInvariant()
  exit 0
}

$stream = [System.IO.File]::OpenRead($Path)
try {
  $sha256 = [System.Security.Cryptography.SHA256]::Create()
  try {
    $hash = $sha256.ComputeHash($stream)
    Write-Output (($hash | ForEach-Object { $_.ToString("x2") }) -join "")
  } finally {
    $sha256.Dispose()
  }
} finally {
  $stream.Dispose()
}
