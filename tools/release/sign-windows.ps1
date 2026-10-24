param([Parameter(Mandatory)][string]$File, [switch]$VerifyOnly)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
foreach ($name in @('SIGNTOOL_PATH', 'SIGNING_DLIB_PATH', 'SIGNING_METADATA_PATH', 'ARTIFACT_SIGNING_PUBLISHER')) {
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) { throw "Missing signing configuration: $name" }
}
$target = (Resolve-Path -LiteralPath $File).Path
if (-not $VerifyOnly) {
    & $env:SIGNTOOL_PATH sign /v /fd SHA256 /tr http://timestamp.acs.microsoft.com /td SHA256 /dlib $env:SIGNING_DLIB_PATH /dmdf $env:SIGNING_METADATA_PATH $target
    if ($LASTEXITCODE -ne 0) { throw "Signing failed: $target ($LASTEXITCODE)" }
}
& $env:SIGNTOOL_PATH verify /pa /all /v /tw $target
if ($LASTEXITCODE -ne 0) { throw "Signature trust verification failed: $target ($LASTEXITCODE)" }
$signature = Get-AuthenticodeSignature -LiteralPath $target
if ($signature.Status -ne 'Valid' -or $null -eq $signature.SignerCertificate -or $null -eq $signature.TimeStamperCertificate) {
    throw "A trusted, timestamped signature is required: $target"
}
if ($signature.SignerCertificate.Subject -cne $env:ARTIFACT_SIGNING_PUBLISHER) {
    throw "Unexpected publisher for ${target}: $($signature.SignerCertificate.Subject)"
}
# Inno removes its signed temporary uninstaller after embedding it in Setup.
# Keep a verified copy outside dist for an independent postcompile check.
if (-not $VerifyOnly -and $env:SIGNING_UNINSTALLER_EVIDENCE -and [IO.Path]::GetFileName($target) -match '^uninst\.e(32|64)\.tmp$') {
    Copy-Item -LiteralPath $target -Destination $env:SIGNING_UNINSTALLER_EVIDENCE
}
