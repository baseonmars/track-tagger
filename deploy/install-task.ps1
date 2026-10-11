# Run manually after reviewing paths and a collection preview.
param(
  [string]$Executable = 'C:\Program Files\TrackTagger\track-tagger.exe',
  [string]$Source = 'D:\Music\Recordings',
  [string]$State = "$env:LOCALAPPDATA\TrackTagger"
)
$ErrorActionPreference = 'Stop'
foreach ($Value in @($Executable, $Source, $State)) {
  if ($Value.Contains('"')) { throw 'Paths must not contain double quotes' }
}
if (-not (Test-Path -LiteralPath $Executable -PathType Leaf)) { throw 'Executable does not exist' }
if (-not (Test-Path -LiteralPath $Source -PathType Container)) { throw 'Source directory does not exist' }
New-Item -ItemType Directory -Force -Path $State | Out-Null
$Arguments = 'serve "{0}" --write --state "{1}" --addr 127.0.0.1:8787 --poll' -f ($Source.TrimEnd('\') + '\.'), ($State.TrimEnd('\') + '\.')
$Action = New-ScheduledTaskAction -Execute $Executable -Argument $Arguments
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User ([System.Security.Principal.WindowsIdentity]::GetCurrent().Name)
$Principal = New-ScheduledTaskPrincipal -UserId ([System.Security.Principal.WindowsIdentity]::GetCurrent().Name) -LogonType Interactive -RunLevel Limited
$Settings = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName TrackTagger -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings
Write-Output 'Task registered for next logon. Use Start-ScheduledTask TrackTagger to start it explicitly.'
