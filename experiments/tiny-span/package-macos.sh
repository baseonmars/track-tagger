#!/usr/bin/env bash
# Build and smoke-test the frozen offline research CLI in a new external directory.
set -euo pipefail
cd "$(dirname "$0")/../.."
exp=experiments/tiny-span
data=$(cd "${DATA:-../track-tagger-experiment-data}" && pwd)
if [[ $(uname -s)/$(uname -m) != Darwin/arm64 ]]; then
  echo "Packaging replay targets macOS arm64" >&2
  exit 2
fi
if [[ $# != 1 || -e $1 ]]; then
  echo "Provide one new output directory outside this worktree" >&2
  exit 2
fi
mkdir "$1"
out=$(cd "$1" && pwd)
if [[ "$out/" == "$(pwd -P)/"* ]]; then
  rmdir "$out"
  echo "Output directory must be outside this worktree" >&2
  exit 2
fi
py=${PYTHON:-$data/tiny-span/macos-package/.venv/bin/python}
export HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 TOKENIZERS_PARALLELISM=false PYTHONDONTWRITEBYTECODE=1
mkdir -p "$out/assets/lora" "$out/clean-home"
cp "$data/tiny-span/run/lora/"{model.onnx,policy.json,vocab.txt} "$out/assets/lora"
cp "$data/v2/three-arm-build/catalog-fixed" "$out/assets/date-catalog"
cp "$exp/results/execution-freeze.json" "$out/assets"
"$py" - "$exp" <<'PY'
from hashlib import sha256
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
frozen = json.loads((root / "results" / "execution-freeze.json").read_bytes())
for name in ("pilot.py", "evaluate.py"):
    if sha256((root / name).read_bytes()).hexdigest() != frozen["entrypoints"][name]:
        raise SystemExit(f"Frozen entrypoint changed: {name}")
PY
"$py" -m PyInstaller --noconfirm --onedir --name track-tagger-span \
  --distpath "$out/dist" --workpath "$out/work" --specpath "$out" \
  --add-data "$out/assets/lora:assets/lora" \
  --add-data "$out/assets/execution-freeze.json:assets" "$exp/offline_cli.py"
bundle=$out/dist/track-tagger-span
cp "$out/assets/date-catalog" "$bundle/_internal/assets/date-catalog"
chmod +x "$bundle/_internal/assets/date-catalog"
cat > "$out/smoke.jsonl" <<'JSONL'
{"id":"header","notes":[{"name":"note.txt","text":"Cloudglass Trio\n2004-03-26\nThe Blue Vault\nMoonside, RI\n\nSource: audience recording."}],"baseline":{"ready":false,"metadata":{}}}
{"id":"labels","notes":[{"name":"note.txt","text":"Artist: Rising Lake Quartet\nDate: 2014-09-27\nVenue: Juniper Pavilion\nCity: Port Lenox, WA"}],"baseline":{"ready":false,"metadata":{}}}
{"id":"date","notes":[{"name":"test.txt","text":"2012-03-08\nCloudglass Trio\nThe Blue Vault\nMoonside, RI"}],"baseline":{"ready":false,"metadata":{"artist":"Cloudglass Trio","venue":"The Blue Vault","city":"Moonside, RI"}}}
{"id":"locked","notes":[{"name":"test.txt","text":"Cloudglass Trio\n2004-03-26\nThe Blue Vault\nMoonside, RI"}],"baseline":{"ready":true,"metadata":{}}}
{"id":"background","notes":[{"name":"safe.txt","text":"Source: digital recording\nTape transfer done by staff."}],"baseline":{"ready":false,"metadata":{}}}
JSONL
"$py" "$exp/offline_cli.py" --assets "$out/assets" < "$out/smoke.jsonl" > "$out/interpreted.jsonl"
# Relocate before the smoke test and prohibit reads from the original
# installation and build-time Python. Retain only system runtime access.
ditto "$bundle" "$out/clean-home/track-tagger-span"
profile=$("$py" - "$bundle" <<'PYPROFILE'
import json
from pathlib import Path
import sys

denied_paths = [Path(sys.argv[1]), Path(sys.prefix), Path(sys.base_prefix)]
blocked = " ".join(f"(subpath {json.dumps(str(p.resolve()))})" for p in denied_paths)
print(f"(version 1)(allow default)(deny network*)(deny file-read* {blocked})")
PYPROFILE
)
cd "$out/clean-home"
env -i HOME="$out/clean-home" PATH=/usr/bin:/bin HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 \
  TOKENIZERS_PARALLELISM=false /usr/bin/sandbox-exec -p "$profile" \
  ./track-tagger-span/track-tagger-span < "$out/smoke.jsonl" > "$out/bundled.jsonl"
codesign --verify --strict "$bundle/track-tagger-span"
codesign --verify --strict "$bundle/_internal/assets/date-catalog"
cmp "$out/interpreted.jsonl" "$out/bundled.jsonl"
"$py" - "$bundle" "$out" <<'PY'
from hashlib import sha256
from importlib.metadata import version
import json
from pathlib import Path
import platform
import sys

bundle, out = map(Path, sys.argv[1:])
files = [p for p in bundle.rglob("*") if p.is_file() and not p.is_symlink()]
links = [p for p in bundle.rglob("*") if p.is_symlink()]
digest = lambda p: sha256(p.read_bytes()).hexdigest()
rows = [json.loads(line) for line in (out / "bundled.jsonl").read_text().splitlines()]
assert any(p["field"] == "date" for row in rows for p in row["proposals"]), "date validator not exercised"
assert all(not row["proposals"] for row in rows if row["id"] in {"locked", "background"})
assets = bundle / "_internal" / "assets"
report = {
    "platform": platform.platform(),
    "python": platform.python_version(),
    "pyinstaller": version("pyinstaller"),
    "distribution": "macOS arm64 onedir, regular files plus relative symlinks",
    "regular_file_count": len(files),
    "uncompressed_regular_file_bytes": sum(p.stat().st_size for p in files),
    "symlink_count": len(links),
    "symlink_target_string_bytes": sum(p.lstat().st_size for p in links),
    "entry_executable_bytes": (bundle / "track-tagger-span").stat().st_size,
    "entry_executable_sha256": digest(bundle / "track-tagger-span"),
    "asset_sha256": {p.relative_to(assets).as_posix(): digest(p)
                     for p in sorted(assets.rglob("*")) if p.is_file()},
    "smoke": {"cases": len(rows), "network_denied": True,
              "relocated_and_build_python_access_denied": True,
              "interpreted_and_bundled_outputs_byte_identical": True,
              "date_validator_exercised": True, "ready_and_background_rows_abstained": True,
              "sha256": digest(out / "bundled.jsonl")},
}
(out / "bundle-result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
PY
echo "$out/bundle-result.json"
