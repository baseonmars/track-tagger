# Project memory

Track Tagger's music scanning and tagging remain local. The Windows launcher checks only GitHub release metadata when its startup check is enabled; it sends no music paths or file contents, and users can disable the check for fully offline use.

## Delivery preferences

- Dan authorizes creating feature branches and isolated worktrees, commits, pushes, and pull requests as needed to complete requested work.
- Merging to `main` always requires Dan's explicit permission. Never merge automatically.
- Keep changes off `main` and `production`. Qualifying Conventional Commit merges to `main` may trigger release planning and artifact builds. Public release, version-tag creation, and GHCR image publication require separate approval through the protected `github-release` environment, including manual `v*` releases. Do not approve or bypass the gate without Dan's explicit signoff for that release.
