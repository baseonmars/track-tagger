# Deployment examples

These examples are templates, not installers run by the application. They explicitly enable in-place writes. Start with a disposable recording copy, inspect a dry-run of the collection, resolve review items, and keep backups before installing a watcher. No service has been enabled by adding these files.

```sh
track-tagger scan /path/to/recordings
track-tagger scan /path/to/disposable-copy --write
```

Keep application state on a local persistent disk **outside the music tree**. State contains metadata history and review decisions; retain and back it up for undo. Give the service account permission to read the entire source and create, replace, and remove files in recording directories. The application stages replacements beside their targets, so allow free space for the largest track. Mounts must support reliable rename semantics. Never run two installations against the same source with separate state directories.

Defaults are a two-minute quiet period and reconciliation every five minutes. Add `--poll` for SMB/NFS/NAS paths. Use locally available files; cloud placeholders must be downloaded explicitly by their owner before processing. Whole-file checksum documents remain unchanged and will no longer match after metadata edits; audio payload fingerprints remain unchanged.

## macOS LaunchAgent

Install the appropriate `darwin-arm64` or `darwin-amd64` binary in `/usr/local/bin/track-tagger`. Edit every `YOUR_USER` and the source path in `com.track-tagger.plist`; plist paths must be absolute (no `~`). Create the state and log directories, then manually copy the plist to `~/Library/LaunchAgents/com.track-tagger.plist`.

```sh
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.track-tagger.plist"
# Stop and unload:
launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.track-tagger.plist"
```

The UI is at `http://127.0.0.1:8787`. macOS may require Files and Folders permissions for the executable's source location. The LaunchAgent runs only while the user is logged in.

## Linux systemd

Install the Linux binary at `/usr/local/bin/track-tagger`. Create a dedicated `track-tagger` account and grant access to `/srv/music` through an appropriate group or ACL. Edit the source and `ReadWritePaths` in `track-tagger.service` together. Add `--poll` to `ExecStart` for a network filesystem and ensure it is mounted before service start (for example with `RequiresMountsFor=/srv/music`).

```sh
sudo install -m 644 deploy/track-tagger.service /etc/systemd/system/track-tagger.service
sudo systemctl daemon-reload
sudo systemctl enable --now track-tagger
journalctl -u track-tagger -f
# Stop and disable:
sudo systemctl disable --now track-tagger
```

`StateDirectory` creates persistent `/var/lib/track-tagger`. `ProtectHome=true` means the supplied service cannot access collections in home directories; adjust paths or the hardening directive deliberately. Access the localhost UI over an SSH tunnel rather than exposing it:

```sh
ssh -L 8787:127.0.0.1:8787 your-server
```

## Windows Task Scheduler

Extract the Windows executable. Review `install-task.ps1`, then run it manually in PowerShell with the intended executable, collection, and local state paths:

```powershell
.\deploy\install-task.ps1 -Executable 'C:\Tools\track-tagger.exe' -Source 'D:\Music\Recordings' -State 'C:\Users\YOUR_USER\AppData\Local\TrackTagger'
Start-ScheduledTask TrackTagger
# Stop and remove:
Stop-ScheduledTask TrackTagger
Unregister-ScheduledTask TrackTagger
```

The task runs under the current interactive user at logon, with polling and without elevation. For shares, use UNC paths available to that user. Mapped drive letters may be unavailable to scheduled tasks. The UI binds to localhost. The example does not store a password or create an unattended system account task.

## Docker Compose

The image is a single static executable and defaults to preview (`scan /music`). The supplied Compose file explicitly runs an in-place polling watcher and publishes no ports.

Create writable music and state directories before starting; set your own UID/GID:

```sh
export TRACK_TAGGER_MUSIC=/srv/music
export TRACK_TAGGER_STATE=/srv/track-tagger-state
export TRACK_TAGGER_UID=1000 TRACK_TAGGER_GID=1000
mkdir -p "$TRACK_TAGGER_STATE"
docker compose -f deploy/compose.yml build
# Preview before the first start:
docker compose -f deploy/compose.yml run --rm track-tagger scan /music
docker compose -f deploy/compose.yml up -d
# Stop:
docker compose -f deploy/compose.yml down
```

Container metadata history persists in the state bind mount. Staged media uses the music mount, so the read-only container root does not prevent writes. A remote UI needs `serve /music --write --state /state --poll --remote --addr 0.0.0.0:8787` as its command, an explicitly set `TRACK_TAGGER_TOKEN` environment secret of at least 16 characters, and a host port binding such as `127.0.0.1:8787:8787`. Do not commit the token; connect with an authenticated client. If exposing the service beyond the host, use TLS termination and authentication; prefer SSH tunneling to a host-local published port.

## Proxmox and Synology

For Proxmox, use a Linux VM with the matching amd64/arm64 executable or Docker. Keep SQLite state on the VM's local disk; mount the recording share into the VM, grant the service account write access, and use `--poll`. Start with the systemd or Compose example. This repository does not provision the VM or mount shares.

On Synology models supporting Container Manager/Docker and Linux amd64 or arm64, use the Compose layout with host paths such as `/volume1/music` and `/volume1/docker/track-tagger`. Match the NAS user's numeric UID/GID and ACLs. Keep state on a local volume, use polling, and verify replacement behavior on disposable files first. Other CPU architectures and NAS platforms without compatible containers are not covered; run Track Tagger in a Linux VM with the share mounted instead.

## Automated artifacts and releases

GitHub CI checks formatting, runs `go vet` and race-enabled tests natively on Linux, Windows, and macOS, then builds CGo-free binaries for all six OS/architecture combinations. Each build publishes downloadable workflow archives. Pushing a `v*` tag creates a GitHub Release with those archives and a combined SHA-256 checksum file; the container workflow publishes a Linux amd64/arm64 manifest to GHCR for version tags. Manual container runs default to build-only unless publication is explicitly selected. No release tag is created by these examples.

The Docker base Go version must stay aligned with `go.mod`; Dependabot opens dependency and action update PRs. CI is the source of evidence for native platform checks: cross-compilation alone does not establish runtime compatibility. Release archives are not code-signed or notarized.
