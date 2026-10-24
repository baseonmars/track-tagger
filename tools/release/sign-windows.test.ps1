# Synthetic signature responses exercise failure handling without a certificate,
# Azure authentication, or an executable from the user's machine.
$ErrorActionPreference = 'Stop'
$signScript = Join-Path $PSScriptRoot 'sign-windows.ps1'
$fixture = [IO.Path]::GetTempFileName()
$evidenceDir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $evidenceDir | Out-Null
$env:SIGNTOOL_PATH = 'Test-SignTool'
$env:SIGNING_DLIB_PATH = 'synthetic-dlib'
$env:SIGNING_METADATA_PATH = 'synthetic-metadata'
$env:ARTIFACT_SIGNING_PUBLISHER = 'CN=Expected Publisher, C=US'
function Test-SignTool {
    $global:SigningTestCommands += ,@($args)
    $global:LASTEXITCODE = if ($args[0] -eq 'sign') { $global:SigningTestSignExit } else { $global:SigningTestVerifyExit }
}
function Get-AuthenticodeSignature {
    param([string]$LiteralPath)
    return $global:SigningTestSignature
}
function Reset-Fixture {
    $global:SigningTestCommands = @()
    $global:SigningTestSignExit = 0
    $global:SigningTestVerifyExit = 0
    $global:SigningTestSignature = [pscustomobject]@{
        Status = 'Valid'
        SignerCertificate = [pscustomobject]@{ Subject = $env:ARTIFACT_SIGNING_PUBLISHER }
        TimeStamperCertificate = [pscustomobject]@{ Subject = 'CN=Synthetic Timestamp' }
    }
}
function Expect-Failure {
    param([scriptblock]$Run, [string]$Message)
    try { & $Run } catch {
        if ($_.Exception.Message -notlike "*$Message*") { throw "Wrong failure: $_" }
        return
    }
    throw "Expected failure containing: $Message"
}
try {
    Reset-Fixture
    & $signScript -File $fixture
    if ($global:SigningTestCommands.Count -ne 2 -or $global:SigningTestCommands[0][0] -ne 'sign' -or $global:SigningTestCommands[1][0] -ne 'verify') { throw 'Expected sign followed by verify' }
    $arguments = $global:SigningTestCommands[0] -join ' '
    if ($arguments -notmatch '/fd SHA256' -or $arguments -notmatch '/td SHA256' -or $arguments -notmatch '/tr http://timestamp.acs.microsoft.com') { throw 'Expected SHA256 RFC3161 timestamp' }
    Reset-Fixture
    & $signScript -VerifyOnly -File $fixture
    if ($global:SigningTestCommands.Count -ne 1 -or $global:SigningTestCommands[0][0] -ne 'verify') { throw 'VerifyOnly must not sign' }
    Reset-Fixture
    $global:SigningTestSignExit = 1
    Expect-Failure { & $signScript -File $fixture } 'Signing failed'
    if ($global:SigningTestCommands.Count -ne 1) { throw 'Must stop after signing failure' }
    Reset-Fixture
    $global:SigningTestVerifyExit = 1
    Expect-Failure { & $signScript -File $fixture } 'Signature trust verification failed'
    Reset-Fixture
    $global:SigningTestSignature.Status = 'NotTrusted'
    Expect-Failure { & $signScript -File $fixture } 'trusted, timestamped signature'
    Reset-Fixture
    $global:SigningTestSignature.TimeStamperCertificate = $null
    Expect-Failure { & $signScript -File $fixture } 'trusted, timestamped signature'
    Reset-Fixture
    $global:SigningTestSignature.SignerCertificate.Subject = 'CN=Other Publisher, C=US'
    Expect-Failure { & $signScript -File $fixture } 'Unexpected publisher'
    Reset-Fixture
    $env:ARTIFACT_SIGNING_PUBLISHER = ''
    Expect-Failure { & $signScript -File $fixture } 'Missing signing configuration'
    if ($global:SigningTestCommands.Count -ne 0) { throw 'Missing configuration must stop before signing' }
    $env:ARTIFACT_SIGNING_PUBLISHER = 'CN=Expected Publisher, C=US'
    Reset-Fixture
    $env:SIGNING_UNINSTALLER_EVIDENCE = Join-Path $evidenceDir 'verified.exe'
    $uninstaller = Join-Path $evidenceDir 'uninst.e32.tmp'
    Copy-Item -LiteralPath $fixture -Destination $uninstaller
    & $signScript -File $uninstaller
    if (-not (Test-Path -LiteralPath $env:SIGNING_UNINSTALLER_EVIDENCE)) { throw 'Verified temporary uninstaller evidence was not retained' }
    Write-Output 'Windows signing safeguards passed.'
} finally {
    Remove-Item -LiteralPath $fixture
    Get-ChildItem -LiteralPath $evidenceDir | Remove-Item
    Remove-Item -LiteralPath $evidenceDir
}
