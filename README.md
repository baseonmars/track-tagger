# Track Tagger

A portable, offline-first Go application for reviewing and tagging live-concert FLAC recordings from local notes and artwork. No API key, Python installation, or external audio tool is required. The web interface is embedded in the executable.

## Start with a preview

```sh
go build -o track-tagger ./cmd/track-tagger
./track-tagger scan /music/concerts
./track-tagger scan /music/concerts --json --plan /tmp/concert-plan.json
./track-tagger serve /music/concerts
```

Open http://127.0.0.1:8787. Review source notes, metadata, file-to-title mappings, and artwork. Explicitly acknowledge replacements when saving corrections. The default server can save decisions in application storage but cannot change media.

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

`SOURCE` is a recording folder or a collection of folders. `--dry-run`, `--write`, and `--copy DEST` are mutually exclusive for every processing command. No configuration or saved plan enables writes. Flags work before or after the source argument. A scan reports blocked recordings and processes only ready recordings; inspect the preview/review queue for exceptions.

Use `--state DIR` to select application storage (default: your OS user configuration directory, `track-tagger` subdirectory). Storage must be outside both music trees. Keep this directory: it contains SQLite history, original metadata, copy ownership, and manual decisions. Plans must also be saved outside music trees. A new preview does not create application storage; an existing store may be opened to read saved decisions.

## Discovery and review

- Recursive discovery groups explicit `Disc 1`, `CD2`, and `Set 1` folders with their parent recording. Other nested recording folders remain independent.
- UTF-8 and Windows-1252 notes support labelled artist/venue/city, concert dates, numbered tracks, explicit sets/discs, and encore annotations. Filename IDs include `t01`, `s1t01`, `d1t01`, `1-01`, and leading numbers. Titles are matched by identifiers, never shifted by list position.
- Unlabelled or ambiguous headings require review. The parser intentionally avoids guessing performers. Numeric dates such as `03/04/1970` require review. Transfer dates and technical headings are excluded from concert metadata.
- Missing files, missing titles, duplicate numbering, conflicting dates/performers/tags, and ambiguous artwork enter the review queue. Checksum manifests are checked for referenced file presence; original documents are retained.
- Default album: `YYYY-MM-DD - Venue, City`. Concert date becomes `DATE`; no release date is invented. Explicit sets become discs. Existing populated tags/artwork are preserved unless a review explicitly approves replacement.
- Review decisions retain input hashes. Relevant input changes invalidate those decisions; stale browser forms and saved plans are rejected. Edited JSON plans must explicitly set `Reviewed` to `true` and still pass file/mapping/evidence validation.

The quiet interval defaults to two minutes. Watchers measure time since observed changes, add recursive directory watches, and reconcile every five minutes. Use `--poll` for network shares (15-second observations). Watcher failures fall back to polling and are logged. `--settle 0s` is available for deliberate testing on disposable copies; normal operation should retain the default.

Recognized partial-download extensions, zero-length files, macOS dataless files, and Windows offline/recall attributes wait for local availability. The application never asks a cloud provider to download a collection. Provider-specific Linux/network placeholders without recognizable attributes cannot be detected reliably; pin those recordings locally first.

## Write and recovery guarantees

Copy mode mirrors paths relative to `SOURCE`, including each recording's accompanying files. It rejects overlapping trees and symlinks, creates independent files, refuses unrelated destination collisions, and updates application-owned outputs only while their recorded hashes match. Sources stay byte-identical. Replacing an existing copy with different audio is refused; select a new destination for changed audio. Collection-level files outside discovered recordings are not copied.

FLAC processing streams audio. The adapter bounds metadata lengths, preserves unknown blocks and unrelated comments, and verifies SHA-256 identity of the exact audio payload. It stages beside the destination, syncs, rechecks source/evidence and destination hashes, and replaces through a platform adapter. A SQLite journal records intent before each replacement and recovers interrupted file commits on the next write command. A process lock serializes writers using the same state directory.

A job is recoverable per file, not an atomic whole-album transaction. An interrupted album can contain completed and pending files; rerun with the same state directory. The `.track-tagger-` file prefix is reserved for temporary stages; abandoned stages are ignored after a crash and can be removed while the application is stopped. Original metadata stays in history. Undo restores metadata in either mode, keeps copied companion files, and refuses subsequent external edits or later tagging jobs. Undo pauses automatic tagging of that recording until it is reviewed again.

Audio identity is verified without decoding FLAC frames: this does not validate frame CRCs or establish that a download with a nonempty payload is complete. Metadata changes alter whole-file MD5 checksums even when audio is identical; existing checksum documents are not rewritten. Back up important collections independently. Processes using different state directories are not mutually locked; do not run competing writers on the same music tree.

## Web access and services

The server binds to localhost by default. Remote access requires both `--remote` and a token of at least 16 characters:

```sh
export TRACK_TAGGER_TOKEN='replace-with-a-long-random-secret'
track-tagger serve SOURCE --copy DEST --remote --addr 0.0.0.0:8787
```

Use any Basic Auth username and the token as password. Put remote access behind HTTPS or an encrypted private tunnel. CSRF, origin and localhost Host checks protect mutations. Tokens never appear in command arguments.

[Deployment examples](deploy/README.md) cover LaunchAgent, systemd, Windows Task Scheduler, Docker Compose, Proxmox Linux VMs, and compatible Synology containers. Examples explicitly use `--write`; installing/enabling them is a separate deliberate step. No installer or background service is enabled by building or running a preview.

For Roon, inspect its metadata preferences for the album and prefer file metadata for fields you want Track Tagger to control, then rescan the watched storage location after a verified tagging run. Roon may retain its own identified metadata; tagging a file alone does not force every Roon display field to change.

## Development, tests, and releases

```sh
go test -race ./...            # unit, integration, and CLI/HTTP end-to-end tests
go vet ./...
go test ./internal/catalog -fuzz FuzzNotes -fuzztime 10s
go test ./internal/media -fuzz FuzzMetadata -fuzztime 10s
```

Fixtures cover Miles Davis's 10-title/9-file mismatch, Ringo Starr's 25-title/26-file mismatch, Ramblin' Jack's conflicting dates, Hendrix's unnumbered partial setlist, and failure/recovery cases. These are synthetic regressions reflecting the described cases; the actual collection has not yet been supplied or previewed.

GitHub Actions checks formatting, vet, and native race/unit/e2e tests on Linux, macOS, and Windows. Successful runs upload amd64/arm64 archives for all three platforms. Pushing a `v*` tag automatically creates a GitHub Release with archives and SHA-256 checksums, and publishes Linux amd64/arm64 images to GHCR. Manual container runs default to build-only. Binaries are unsigned; macOS notarization and Windows code signing are not configured.

V1 is FLAC-only. AI parsing, recognition, renaming, and duplicate removal are deferred.
