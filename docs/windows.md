# Windows guide

Track Tagger desktop builds support Windows 10/11 on x64 PCs and Windows 11 on ARM64 PCs. Choose the matching installer from the GitHub Release. The x64 installer is not offered on ARM64 Windows, where the native ARM64 build is available. A standalone ZIP for either architecture contains `track-tagger-gui.exe`, `track-tagger.exe`, the license, and this README.

Direct links to the latest stable installers:

- [Windows x64 installer](https://github.com/baseonmars/track-tagger/releases/latest/download/track-tagger-windows-x64-setup.exe)
- [Windows ARM64 installer](https://github.com/baseonmars/track-tagger/releases/latest/download/track-tagger-windows-arm64-setup.exe)

## Install and upgrade

The installer runs for the current Windows user and installs under `%LOCALAPPDATA%\Programs\Track Tagger`; it does not request administrator access. The Start menu shortcut opens the desktop app. Setup can also add a desktop shortcut and launch the app when it finishes. The CLI, `track-tagger.exe`, is installed in the same folder for use from PowerShell or Windows Terminal. If Track Tagger is running during an upgrade or uninstall, choose **Quit** from its notification-area menu, then retry. Setup and the uninstaller will not close the app or replace its files while it is running. Windows may show an unsigned-app warning because release executables are not code-signed; use installers downloaded from this project's GitHub Releases.

An upgrade replaces program files while retaining your settings and application state. Those files are under `%APPDATA%\track-tagger`. They include saved folder/mode preferences, SQLite job history, original metadata, and review decisions. Back up this directory if you need to preserve history independently.

## Choose folders and mode

Launch **Track Tagger** from the Start menu. On first launch, choose the music folder and select **Preview**, **Copy to another folder**, or **Update originals**; Preview is selected initially. Copy requires a destination. The app saves the current folders, mode, and polling setting when you press Start or Quit. On later launches, the last saved choices are restored, but the app always opens stopped. You must press Start after each launch or restart.

The app checks GitHub for the latest stable release at startup when **Check automatically at startup** is enabled (the default). Click **Check for updates** to check at another time. These checks retrieve public release information only; Track Tagger does not send music paths, audio, settings, or review data. After an update is found, click **Release notes** to review it. Click **Install update** and confirm to download the matching x64 or ARM64 installer and verify it against `SHA256SUMS`. Track Tagger safely stops active work, installs the update, then reopens with processing stopped; press **Start** when ready. If installation cannot start, the current app stays available so you can retry. Portable copies open the release page for a manual download instead of replacing files in place. Development builds also require a manual stable release install. Disable **Check automatically at startup** for fully offline use. You can still download a stable installer from the links above.

Copy keeps source files unchanged and writes owned outputs to the destination. Update originals changes metadata in the source music folder. Both modes automatically process eligible recordings after they settle; ambiguous recordings and other exceptions wait for review. The browser opens to the local review page when the service starts. Closing the browser leaves processing active.

For a network share or mapped drive, Track Tagger selects polling automatically for UNC paths. You can also check **Use polling** for a mapped drive or other filesystem that does not report changes reliably. Polling observes changes every 15 seconds. Filesystem watches otherwise observe changes and reconcile periodically.

## Stop, quit, and recover

Closing the desktop window hides Track Tagger in the notification area. The first close explains this behavior. Use the tray menu's **Stop** to stop the watcher and web service while leaving the desktop app open. Use **Quit** to stop and exit. Stop or quit before disconnecting a drive or moving a collection.

If Windows, Track Tagger, or the computer stops during a write, relaunch the app and press Start using the same folders and mode. The app uses `%APPDATA%\track-tagger` as its state directory, so the write journal is available for recovery. Recovery takes place when a write resumes and is per file, not per album. Inspect the review queue and rerun the operation if some tracks were still pending. Do not delete state files to clear an interrupted job; original metadata and the write journal are kept there for recovery and undo.

## Uninstall

Stop processing from the tray menu and choose **Quit** before uninstalling. Open Windows **Settings → Apps → Installed apps**, select **Track Tagger**, and uninstall it. The uninstaller removes program files and shortcuts, but retains `%APPDATA%\track-tagger` so you can keep your configuration, review decisions, job history, and undo information. Remove that directory yourself only if you intend to discard the history permanently.
