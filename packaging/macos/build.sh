#!/bin/bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
output="${1:-$root/dist}"
version="${2:-0.0.0}"
if [[ ! "$version" =~ ^([0-9]+\.[0-9]+\.[0-9]+)([-+][0-9A-Za-z.+-]+)?$ ]]; then
    printf 'Expected a version such as 1.2.3 or 1.2.3-dev.\n' >&2
    exit 1
fi
bundle_version="${BASH_REMATCH[1]}"
if [[ "$(uname -s)" != Darwin ]]; then
    printf 'Build the macOS app on a Mac with Xcode Command Line Tools.\n' >&2
    exit 1
fi

mkdir -p "$output"
output="$(cd "$output" && pwd)"
staging="$(mktemp -d "$output/.track-tagger.XXXXXXXX")"
trap 'rm -rf "$staging"' EXIT
app="$staging/Track Tagger.app"
mkdir -p "$app/Contents/MacOS"
cp "$root/packaging/macos/Info.plist" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $bundle_version" \
    -c "Set :CFBundleShortVersionString $bundle_version" "$app/Contents/Info.plist"
(
    cd "$root"
    GOOS=darwin CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0 \
        go build -trimpath -ldflags="-s -w" -o "$app/Contents/MacOS/track-tagger-gui" ./cmd/track-tagger-gui
)
mkdir -p "$app/Contents/Resources/note-suggestions"
cp "$root"/internal/notesmodel/assets/* "$app/Contents/Resources/note-suggestions/"
codesign --force --sign - "$app"
codesign --verify --strict "$app"
rm -rf "$output/Track Tagger.app"
mv "$app" "$output/"
