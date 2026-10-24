#ifndef AppVersion
  #error AppVersion must be supplied by the build
#endif
#ifndef AppArch
  #error AppArch must be x64 or arm64
#endif
#ifndef BuildDir
  #error BuildDir must point to the executable directory
#endif

[Setup]
AppId={{A4D0D90B-4DB2-469D-A85B-2E2FE5311E67}
AppName=Track Tagger
AppVersion={#AppVersion}
AppPublisher=Track Tagger contributors
DefaultDirName={localappdata}\Programs\Track Tagger
DefaultGroupName=Track Tagger
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
CloseApplications=no
RestartApplications=no
#if AppArch == "x64"
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible and not arm64
#else
ArchitecturesInstallIn64BitMode=arm64
ArchitecturesAllowed=arm64
#endif
MinVersion=10.0
OutputBaseFilename=track-tagger-windows-{#AppArch}-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayName=Track Tagger
UninstallDisplayIcon={app}\track-tagger-gui.exe
LicenseFile=..\..\LICENSE
SetupLogging=yes
#ifdef ProductionSigning
SignTool=azure
SignedUninstaller=yes
#endif

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "{#BuildDir}\track-tagger.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\track-tagger-gui.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\docs\windows.md"; DestDir: "{app}\docs"; DestName: "windows.md"; Flags: ignoreversion
Source: "..\..\docs\windows-signing.md"; DestDir: "{app}\docs"; Flags: ignoreversion

[Icons]
Name: "{group}\Track Tagger"; Filename: "{app}\track-tagger-gui.exe"
Name: "{autodesktop}\Track Tagger"; Filename: "{app}\track-tagger-gui.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\track-tagger-gui.exe"; Description: "Launch Track Tagger"; Flags: postinstall nowait skipifsilent

[Code]
function AppIsRunning: Boolean;
begin
  Result := FindWindowByClassName('TrackTaggerLauncher') <> 0;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  NeedsRestart := False;
  Result := '';
  if AppIsRunning then
    Result := 'Track Tagger is running. Choose Quit from its notification-area menu, then retry the installer.';
end;

function InitializeUninstall: Boolean;
begin
  Result := not AppIsRunning;
  if not Result then
    MsgBox('Track Tagger is running. Choose Quit from its notification-area menu, then retry uninstall.', mbError, MB_OK);
end;
