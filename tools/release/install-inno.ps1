$ErrorActionPreference = 'Stop'
$installer = Join-Path $env:RUNNER_TEMP 'innosetup-6.4.3.exe'
$installDir = Join-Path $env:RUNNER_TEMP 'InnoSetup'
Invoke-WebRequest -Uri 'https://github.com/jrsoftware/issrc/releases/download/is-6_4_3/innosetup-6.4.3.exe' -OutFile $installer
$expectedHash = 'F3C42116542C4CC57263C5BA6C4FEABFC49FE771F2F98A79D2F7628B8762723B'
$actualHash = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash
if ($actualHash -ne $expectedHash) { throw "Inno Setup SHA-256 mismatch: $actualHash" }
New-Item -ItemType Directory -Force $installDir | Out-Null
$process = Start-Process -FilePath $installer -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/CURRENTUSER', "/DIR=$installDir") -Wait -PassThru -WindowStyle Hidden
if ($process.ExitCode -ne 0) { throw "Inno Setup install failed with exit code $($process.ExitCode)" }
