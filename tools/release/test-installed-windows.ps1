param([Parameter(Mandatory)][string]$Installer)
$ErrorActionPreference = 'Stop'
if ($env:RUNNER_ENVIRONMENT -ne 'github-hosted') { throw 'Installer acceptance runs only on a disposable GitHub-hosted runner' }
$installDir = Join-Path $env:RUNNER_TEMP 'track-tagger-install-test'
$process = Start-Process -FilePath (Resolve-Path -LiteralPath $Installer).Path -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=`"$installDir`"") -Wait -PassThru -WindowStyle Hidden
if ($process.ExitCode -ne 0) { throw "Installer acceptance failed: $($process.ExitCode)" }
try {
    foreach ($name in @('track-tagger.exe', 'track-tagger-gui.exe', 'unins000.exe')) {
        & "$PSScriptRoot/sign-windows.ps1" -VerifyOnly -File (Join-Path $installDir $name)
    }
    & (Join-Path $installDir 'track-tagger.exe') --version
    if ($LASTEXITCODE -ne 0) { throw 'Installed CLI version check failed' }
} finally {
    $uninstaller = Join-Path $installDir 'unins000.exe'
    if (Test-Path -LiteralPath $uninstaller) {
        $process = Start-Process -FilePath $uninstaller -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -Wait -PassThru -WindowStyle Hidden
        if ($process.ExitCode -ne 0) { throw "Uninstall acceptance failed: $($process.ExitCode)" }
    }
}
for ($attempt = 0; $attempt -lt 20 -and (Test-Path -LiteralPath $installDir); $attempt++) { Start-Sleep -Milliseconds 500 }
if (Test-Path -LiteralPath $installDir) { throw 'Uninstall left the disposable application directory behind' }
