# Track Tagger agent guidance

Track Tagger is an offline Go executable with an embedded browser interface. Music scanning and tagging run locally; the Windows launcher can check GitHub release metadata when its startup check is enabled. That check sends no music paths or file contents, and can be disabled for fully offline use. Read `README.md` for the user contract and `.github/workflows/` for build and release behavior.

## Architecture

- `cmd/track-tagger`: CLI commands, flags, and service startup.
- `cmd/track-tagger-gui` and `internal/launcher`: native Windows launcher, update check, tray behavior, and preferences.
- `internal/catalog`: discovery, notes parsing, identifier matching, review reasons, and preview evidence caching.
- `internal/app`: orchestration, plans and reviews, SQLite history and writer locking, staged writes/recovery/undo, watchers, and playback registration.
- `internal/media`: FLAC metadata inspection and streaming transformations, audio hashes, and platform replacement adapters.
- `internal/webui`: embedded HTML/CSS/JavaScript, HTTP review interface, authenticated remote access, and local audio streaming.

## Media and review safeguards

- Preview is the default. Keep `--dry-run`, `--write`, and `--copy DEST` mutually exclusive; require explicit write/copy intent for media changes and `--write` for undo. Run services in preview mode unless the user authorizes media mutation.
- Always preserve the exact audio payload, unknown FLAC blocks, and unrelated comments. Review approval never authorizes audio alteration. Preserve existing populated tags/artwork unless a reviewed decision explicitly approves replacement. Non-FLAC or mixed-format recordings are playback-only.
- Match tracks by conservative file identifiers, never by list position. Ambiguity, conflicting evidence, missing files/titles, and duplicate numbering require review; do not silently guess mappings or dates.
- Preview caches may reuse hashes from file identity, size, and modification time. Review saves, apply, writes, and undo must retain fresh-content validation; never substitute preview evidence for strict integrity checks. Reject stale forms, plans, reviews, and external edits.
- Retain staged replacement, audio identity checks, journal-before-replacement, crash recovery, and writer locking. Recovery is per file, not an atomic album transaction; different state directories do not serialize writers.
- Keep state and saved plans outside music trees. Copy must preserve source bytes, reject overlaps/symlinks and unrelated collisions, and update owned outputs only when recorded hashes match. Undo must refuse external edits or later jobs and retain original metadata history.
- Tests must use synthetic fixtures and disposable temporary trees/state. Never mutate the user's real library or enable a write-mode service as part of testing. Keep remote authentication, CSRF/origin/Host checks, and playback path restrictions intact.

## Writing

Docs, UI text, and comments should describe actual behavior and the reader's next actions. Omit irrelevant absent technologies and lists of unimplemented features. Retain actionable safety limitations and recovery guidance.

## Validation and delivery

Run relevant checks before handing off changes:

```sh
gofmt -l .                    # no output; format changed Go files with gofmt -w
go test -race ./...
go vet ./...
node --test internal/webui/testdata/player.test.cjs  # Node 24
actionlint                    # when changing GitHub Actions workflows
```

CI runs formatting, race tests, vet, player tests, and release-planner tests on Linux, macOS, and Windows. Preserve platform portability. Conventional Commits on `main` plan stable semantic versions; archives, installers, and container images build from the event SHA. Main releases and manual `v*` releases publish only after approval through the protected `github-release` environment. The gate covers release tag creation, GHCR pushes, and public GitHub release publication. Generated tags do not trigger a second workflow; manual tag releases use the same gate. Do not approve or bypass the gate without Dan's explicit signoff for that release.

Use Conventional Commit messages and PR titles (`type(optional-scope): imperative lower-case description`). Deliver through a feature branch and pull request; never push directly to `main` or `production`. Dan authorizes feature branches/worktrees, commits, pushes, and pull requests as needed for requested work. Merging to `main` always requires his explicit permission; never merge automatically. A qualifying merge can trigger release planning and artifact builds, but publishing a versioned GitHub Release or GHCR image requires a separate approval in the protected `github-release` environment. Manual `v*` releases use the same approval gate. Do not approve or bypass the gate without Dan's explicit signoff for that release.
