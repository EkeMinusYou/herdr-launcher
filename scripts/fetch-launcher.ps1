# Put the prebuilt launcher for this plugin version into bin\. herdr runs this as the plugin's build step on Windows.
#
# Modes, in order:
#   1. LAUNCHER_FROM_SOURCE=1 is set               -> go build
#   2. matching binary already installed           -> exit 0
#   3. download the release asset and verify it against SHA256SUMS
$ErrorActionPreference = "Stop"

# Absolute paths from the script's own location, so the current directory does not matter.
$root = [System.IO.Path]::GetDirectoryName($PSScriptRoot)
$manifest = Get-Content -LiteralPath ([System.IO.Path]::Combine($root, "herdr-plugin.toml"))
$version = ($manifest | Select-String -Pattern '^version = "(.*)"$' | Select-Object -First 1).Matches.Groups[1].Value
if (-not $version) { throw "no version in herdr-plugin.toml" }
$bin = [System.IO.Path]::Combine($root, "bin")
[System.IO.Directory]::CreateDirectory($bin) | Out-Null
$destination = [System.IO.Path]::Combine($bin, "launcher.exe")
$stamp = [System.IO.Path]::Combine($bin, "launcher.version")

if ($env:LAUNCHER_FROM_SOURCE) {
  Push-Location -LiteralPath $root
  try { go build -o $destination . } finally { Pop-Location }
  if ($LASTEXITCODE) { exit $LASTEXITCODE }
  Remove-Item -LiteralPath $stamp -ErrorAction SilentlyContinue
  Write-Output "built launcher from source"
  exit 0
}

$installed = if (Test-Path -LiteralPath $stamp -PathType Leaf) { ([string](Get-Content -LiteralPath $stamp -Raw)).Trim() } else { "" }
if ((Test-Path -LiteralPath $destination) -and $installed -eq $version) {
  Write-Output "launcher $version already installed"
  exit 0
}

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
$target = switch ($architecture) {
  "X64" { "windows_amd64" }
  "Arm64" { "windows_arm64" }
  default { throw "no prebuilt launcher for Windows/$architecture; set LAUNCHER_FROM_SOURCE=1 to build it with Go" }
}
$asset = "launcher_$target.exe"
$base = "https://github.com/EkeMinusYou/herdr-launcher/releases/download/v$version"
$temporary = Join-Path ([System.IO.Path]::GetTempPath()) ("herdr-launcher-" + [guid]::NewGuid())
[System.IO.Directory]::CreateDirectory($temporary) | Out-Null
try {
  Write-Output "downloading $base/$asset"
  Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile (Join-Path $temporary $asset)
  Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS" -OutFile (Join-Path $temporary "SHA256SUMS")
  $line = Get-Content -LiteralPath (Join-Path $temporary "SHA256SUMS") | Where-Object { $_ -match "\s$([regex]::Escape($asset))$" } | Select-Object -First 1
  if (-not $line) { throw "$asset is not listed in $base/SHA256SUMS" }
  $expected = ($line -split "\s+")[0].ToLowerInvariant()
  $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $temporary $asset)).Hash.ToLowerInvariant()
  if ($actual -ne $expected) { throw "sha256 mismatch for ${asset}: expected $expected, got $actual" }
  Copy-Item -Force -LiteralPath (Join-Path $temporary $asset) -Destination "$destination.tmp"
  Move-Item -Force -LiteralPath "$destination.tmp" -Destination $destination
  Set-Content -LiteralPath $stamp -NoNewline -Value $version
  Write-Output "installed launcher $version ($target)"
}
finally {
  Remove-Item -Recurse -Force -LiteralPath $temporary -ErrorAction SilentlyContinue
}
