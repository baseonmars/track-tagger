The illustrated manual embeds these actual application screenshots:
launcher.png
library.png
review.png
mapping.png
playback.png
history.png
updates.png

Keep captures free of private music paths and personal data. Render rejects missing or invalid PNG files.

Capture recipe:
- Run the synthetic read-only browser fixture with:
  go run ./internal/webui/testdata/capture -addr 127.0.0.1:8788
  It uses the actual web UI templates. Capture library, review, mapping,
  playback, and History there. Its History page supplies a synthetic example
  of a prior job; Preview itself creates no tagging job.
- Capture the native Windows launcher with isolated APPDATA and LOCALAPPDATA
  temporary folders and a synthetic APPDATA/track-tagger/launcher.json. Run
  the following PowerShell from the repository root to create an ignored Go
  build overlay. It changes only the developer binary's single-instance mutex
  suffix; the production source and a running installed launcher are untouched:

  $source = (Resolve-Path 'internal/launcher/gui_windows.go').Path
  $overlayDir = Join-Path (Get-Location) 'dist/_manual-capture'
  New-Item -ItemType Directory -Force -Path $overlayDir | Out-Null
  $copy = Join-Path $overlayDir 'gui_windows.txt'
  $body = [IO.File]::ReadAllText($source)
  if (-not $body.Contains('Local\TrackTaggerLauncher-')) { throw 'singleton literal missing' }
  [IO.File]::WriteAllText($copy, $body.Replace('Local\TrackTaggerLauncher-', 'Local\TrackTaggerManualCapture-'))
  $overlay = Join-Path $overlayDir 'overlay.json'
  [IO.File]::WriteAllText($overlay, (@{Replace=@{$source=$copy}} | ConvertTo-Json -Depth 4))
  go build -overlay $overlay -o dist/manual-launcher-capture.exe ./cmd/track-tagger-gui

  Keep the overlay source extension .txt. The overlay files are generated for
  capture and are not shipped. Use disposable temporary environment folders
  when running the capture executable; do not open real music in write mode.
- Save actual application captures as PNG with the exact names above. Crop only
  to remove unrelated desktop chrome; retain the real application controls.
