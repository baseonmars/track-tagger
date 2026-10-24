# Track Tagger

Review and tag live-concert FLAC recordings using local notes and artwork. Track Tagger runs locally as a single executable with an embedded web interface.

## Start with a preview

```sh
go build -o track-tagger ./cmd/track-tagger
./track-tagger scan /music/concerts
./track-tagger scan /music/concerts --json --plan /tmp/concert-plan.json
./track-tagger serve /music/concerts
```

Open http://127.0.0.1:8787. Review source notes, metadata, file-to-title mappings, and artwork. Check the approval box after reviewing your changes, then choose **Save review** or, in write/copy mode, **Save and apply**. An unchanged saved review can be applied with **Apply saved review** without checking approval again. Editing any field requires approval again; restoring all fields to their saved values restores the saved-review action. Saving in write/copy mode also allows the watcher to apply approved corrections after the quiet interval. Waiting recordings must settle before applying; completed recordings need an approved change to apply again. If applying fails after saving, the review remains saved: reopen the recording, address the reported problem, and retry **Apply saved review**. The default server runs in preview mode and saves review decisions in application storage. Select `--write` or `--copy DEST` to apply metadata changes.

The running browser interface has an illustrated **Help** manual covering the full review, copy, undo, and recovery workflow. It is embedded with its screenshots for offline reading; the Windows desktop app also opens a local copy when processing is stopped. The [manual source](internal/manual/manual.html) is maintained in this repository.

## Windows desktop app

Download the Windows x64 installer for Windows 10/11 x64, or the ARM64 installer for Windows 11 ARM64. Each installer includes the desktop app and `track-tagger.exe` command line tool. The standalone Windows ZIPs include both executables. Installers are per-user and place the application in `%LOCALAPPDATA%\Programs\Track Tagger`; they do not require administrator access. Windows may show a warning when installing an unsigned release. Continue only if you obtained the installer from this project's GitHub Release; check that release's notes and signature status before installing.

- [Windows x64 installer](https://github.com/baseonmars/track-tagger/releases/latest/download/track-tagger-windows-x64-setup.exe)
- [Windows ARM64 installer](https://github.com/baseonmars/track-tagger/releases/latest/download/track-tagger-windows-arm64-setup.exe)

On first launch, choose a music folder, optional copy destination, and mode. Preview is selected initially; Write and Copy require an explicit choice. The app saves folder, mode, and polling choices when you press Start or Quit, but always starts stopped after launch or restart. Press **Start** to begin. Track Tagger opens the local review page in your browser for the selected mode. Use the tray menu's **Stop** to stop processing while leaving the app open, or **Quit** to close it; closing the browser does not stop the watcher.

The app checks GitHub for the latest stable release at startup when **Check automatically at startup** is enabled (default). You can also use **Check for updates** at any time. It retrieves release information only; it does not send your music paths, audio, or review data. **Install update** asks for confirmation, downloads and verifies the matching installer, safely stops active work, then reopens with processing stopped. Portable copies open the release page for a manual download; development builds also require manual installation. Turn off the startup check for fully offline use. Windows may show a warning when installing an unsigned release.

Network shares can use polling instead of filesystem notifications; choose the polling option in the desktop app and allow the watcher to observe changes at its configured interval. After an interrupted write, start the app with the same state directory: recovery runs when a write operation resumes. Recovery is per file, so review the queue and rerun if an album was interrupted partway through.

The application stores configuration and history under `%APPDATA%\track-tagger`. Keep that data to retain review decisions, job history, and original metadata for undo. Upgrading replaces application files and retains this data. To uninstall, stop and quit Track Tagger first, then use Windows **Installed apps**. Uninstall removes the application but keeps configuration and history.

See the [Windows guide](docs/windows.md) for the full install, operating, recovery, and uninstall steps.

```sh
track-tagger scan SOURCE --dry-run
track-tagger scan SOURCE --write
track-tagger scan SOURCE --copy DEST
track-tagger apply /tmp/concert-plan.json        # preview only
track-tagger apply /tmp/concert-plan.json --write
track-tagger watch SOURCE --write
track-tagger serve SOURCE --copy DEST
track-tagger undo JOB_ID                        # preview restoration
track-tagger undo JOB_ID --write
```

`SOURCE` is a recording folder or a collection of folders. `--dry-run`, `--write`, and `--copy DEST` are mutually exclusive for every processing command. Each command requires an explicit `--write` or `--copy DEST` flag to apply changes. Flags work before or after the source argument. A scan reports blocked recordings and processes only ready recordings; inspect the preview/review queue for exceptions.

Use `--state DIR` to select application storage (default: your OS user configuration directory, `track-tagger` subdirectory). Storage must be outside both music trees. Keep this directory: it contains SQLite history, original metadata, copy ownership, and manual decisions. Plans must also be saved outside music trees. A fresh dry-run scan leaves application storage uncreated; an existing store may be opened to read saved decisions.

## Listen while reviewing

Open a recording and press **Play** beside a track. One shared player provides play/pause, seeking, volume, and previous/next controls. Audio loads when you press Play. Use the previous/next controls to select another track. Files stream locally with HTTP range support and remain unchanged by playback.

FLAC, ordinary PCM WAV, MP3, and Ogg audio (`.ogg`, `.oga`, `.opus`) are available for browser playback, subject to the browser's codec support. A recording containing any non-FLAC audio is **playback-only**, including mixed-format folders; metadata editing and writing remain restricted to FLAC-only recordings. Playback is restricted to locally available files in the current recording collection. Refresh the page if a track changed after loading it.

## Discovery and review

- Recursive discovery groups explicit `Disc 1`, `CD2`, and `Set 1` folders with their parent recording. Other nested recording folders remain independent.
- UTF-8 and Windows-1252 notes support labelled artist/venue/city, concert dates, numbered tracks, written `Set One` through `Set Ten` headings, explicit sets/discs (including `Set I` and `Set II`), encore annotations, and indented segue continuations. Ambiguous continuations require review. Technical sections resume track parsing only after an explicit track-list, set, or `dNtN` marker. Filename IDs include `t01`, `s1t01`, `d1t01`, `1-01`, date-prefixed `s1t01`/`t01`, and leading numbers. Titles are matched by identifiers, never shifted by list position.
- See the [notes parser corpus and evaluation](docs/research/notes-parser-evaluation.md) for the frozen synthetic corpus, source methodology, baseline comparison and reproduction steps.
- Structured artist/date/city `@` venue, artist/venue/city/date, and artist/event/venue/city/date headers are recognized. In five-line headers, the event and venue form the album venue (for example, `Isle of Wight Festival, East Afton Farm`). Track lists may use punctuation or whitespace after their numbers; technical duration reports are excluded. Other unlabelled or ambiguous headings require review.
- An ambiguous numeric date such as `03/04/1970` uses a valid `YYYY-MM-DD` date in the recording folder name, notes filenames, or audio filenames when it selects exactly one interpretation. Otherwise the app proposes month/day order in the concert and album dates and requires review. Conflicting filename dates also require review; check the proposed date against the notes before approving it. Ancestor folders do not count, and filenames never supply a date absent from concert notes. Invalid dates and multiple distinct concert dates leave the date empty. Date-only lines immediately before later track sections also count as concert dates; `01/xx/1968` retains January 1968 without inventing a day. Transfer dates, technical sections, and trailing production dates are excluded from concert metadata.
- Missing files, missing titles, duplicate numbering, conflicting dates/performers/tags, and ambiguous artwork enter the review queue. Checksum manifests are checked for referenced file presence; original documents are retained.
- Default album: `YYYY-MM-DD - Venue, City`. Concert date becomes `DATE`. Explicit sets become discs. For bonus tracks by another performer, use the optional per-track artist field; the album artist stays unchanged. Existing populated tags/artwork are preserved unless a review explicitly approves replacement.
- Concert dates accept `YYYY`, `YYYY-MM`, or `YYYY-MM-DD`. Mark **Date unknown** when the date cannot be established; existing date tags are preserved. The Google lookup opens only when clicked and searches artist and album, falling back to the recording folder name when both are missing.
- Review decisions retain input hashes. Relevant input changes invalidate those decisions; stale browser forms and saved plans are rejected. Edited JSON plans must explicitly set `Reviewed` to `true` and still pass file/mapping/evidence validation.

The quiet interval defaults to two minutes. Watchers measure time since observed changes, add recursive directory watches, and reconcile every five minutes. Use `--poll` for network shares (15-second observations). Watcher failures fall back to polling and are logged. `--settle 0s` is available for deliberate testing on disposable copies; normal operation should retain the default.

After folder discovery, the web library shows recordings while validating them one at a time in the background. Open a recording to prioritize it after the current recording finishes. Progress and library cards update automatically; an open review keeps its edits and playback and asks you to refresh when newer details are available. Saving and applying become available once that recording is validated, even while others are still loading. A failed recording does not prevent the remaining recordings from loading. Refresh the page to retry a discovery error.

The Recordings page can show all recordings or only those marked Needs review, Ready, or Completed. Sort by latest source-file modification time or title. In All, you can keep Needs review at the top or group cards by status. These display choices are saved in this browser and apply as the library updates. Recordings without a known modification time sort last. After saving a review or applying tags, the page returns to Recordings so you can choose the next item.

The watcher and browser share this background work. The first observation reads local files to establish content hashes. Later observations reuse hashes when file identity, size, and modification time match, and read only FLAC headers for tag conflicts. Background validation pauses for review saves and media operations; library pages and progress remain available during that work. Saving or applying refreshes the affected recording. An external edit that preserves those attributes can leave the preview stale; saving reviews and applying changes always validate fresh content in the selected recording, including newly added or removed inputs. CLI scans remain synchronous and strict. The preview cache is in memory and resets when the app restarts.

Recognized partial-download extensions, zero-length files, macOS dataless files, and Windows offline/recall attributes wait for local availability. Pin cloud recordings locally before processing, especially on Linux and network shares where placeholder attributes vary by provider.

## Write and recovery guarantees

Copy mode mirrors paths relative to `SOURCE`, including each recording's accompanying files. It rejects overlapping trees and symlinks, creates independent files, refuses unrelated destination collisions, and updates application-owned outputs only while their recorded hashes match. Sources stay byte-identical. Replacing an existing copy with different audio is refused; select a new destination for changed audio. Collection-level files outside discovered recordings are not copied.

FLAC processing streams audio. The adapter bounds metadata lengths, preserves unknown blocks and unrelated comments, and verifies SHA-256 identity of the exact audio payload. It stages beside the destination, syncs, rechecks source/evidence and destination hashes, and replaces through a platform adapter. A SQLite journal records intent before each replacement and recovers interrupted file commits on the next write command. A process lock serializes writers using the same state directory.

Saving and applying a selected recording do not read unrelated recordings' payloads. Applying still freshly checks the entire selected recording before each file replacement, as well as the source and destination files. Large recordings with many tracks therefore retain repeated full-recording reads during a write; background preview evidence never replaces these integrity checks.

A job is recoverable per file, not an atomic whole-album transaction. An interrupted album can contain completed and pending files; rerun with the same state directory. The `.track-tagger-` file prefix is reserved for temporary stages; abandoned stages are ignored after a crash and can be removed while the application is stopped. Original metadata stays in history. Undo restores metadata in either mode, keeps copied companion files, and refuses subsequent external edits or later tagging jobs. Undo pauses automatic tagging of that recording until it is reviewed again.

Audio identity is verified without decoding FLAC frames: this does not validate frame CRCs or establish that a download with a nonempty payload is complete. Metadata changes alter whole-file MD5 checksums even when audio is identical; existing checksum documents are not rewritten. Back up important collections independently. Processes using different state directories are not mutually locked; do not run competing writers on the same music tree.

## Web access and services

The server binds to localhost by default. Remote access requires both `--remote` and a token of at least 16 characters:

```sh
export TRACK_TAGGER_TOKEN='replace-with-a-long-random-secret'
track-tagger serve SOURCE --copy DEST --remote --addr 0.0.0.0:8787
```

Use any Basic Auth username and the token as password. Put remote access behind HTTPS or an encrypted private tunnel. CSRF, origin and localhost Host checks protect mutations. Tokens never appear in command arguments.

[Deployment examples](deploy/README.md) cover LaunchAgent, systemd, Windows Task Scheduler, Docker Compose, Proxmox Linux VMs, and compatible Synology containers. Examples use `--write`; follow the platform instructions to install and enable a watcher.

For Roon, inspect its metadata preferences for the album and prefer file metadata for fields you want Track Tagger to control, then rescan the watched storage location after a verified tagging run. Roon may retain its own identified metadata; tagging a file alone does not force every Roon display field to change.

## Development, tests, and releases

```sh
go test -race ./...            # unit, integration, and CLI/HTTP end-to-end tests
go vet ./...
node --test internal/webui/testdata/player.test.cjs internal/webui/testdata/review.test.cjs  # development only; Node 24
go test ./internal/catalog -fuzz FuzzNotes -fuzztime 10s
go test ./internal/media -fuzz FuzzMetadata -fuzztime 10s
```

Fixtures cover Miles Davis's 10-title/9-file mismatch, Ringo Starr's 25-title/26-file mismatch, Ramblin' Jack's conflicting dates, Hendrix's unnumbered partial setlist, and failure/recovery cases. Fixtures include synthetic cases and text regressions shaped from local Snarky Puppy and Miles Davis notes.

GitHub Actions checks formatting, vet, and native race/unit/e2e tests on Linux, macOS, and Windows. Successful runs upload amd64/arm64 archives for all three platforms. On `main`, Conventional Commits determine release versions: `feat` creates a minor release, `fix` and `perf` create a patch release, and breaking changes create a major release. Documentation-only changes do not create a version. After tests, archives, and installers build, CI waits for approval in the protected GitHub `github-release` environment. It then pushes the matching container image, creates a draft, attaches all archives, Windows installers, and SHA-256 checksums, verifies the attachments, and publishes the release. The same approval gate applies to manual `v*` releases and manual GHCR publication. A merge does not by itself publish a release. Generated release tags do not trigger a second workflow; the main workflow builds and publishes the matching container image directly. Manual container workflow runs default to build-only. Windows release signing is configured through [Azure Artifact Signing and GitHub OIDC](docs/windows-signing.md); the first signed release still requires explicit release approval and CI acceptance. Existing published releases remain unsigned.

## License

Track Tagger is licensed under the [BSD 3-Clause License](LICENSE).
