#!/usr/bin/env bash
# Run from the Track Tagger checkout. Source notes and per-record output stay external.
set -euo pipefail
if [[ $# != 4 ]]; then
    echo "Usage: $0 EXTERNAL_CASES EXTERNAL_REFERENCE_SEAL PRIOR_CATALOG_BINARY NEW_EXTERNAL_OUTPUT" >&2
    exit 2
fi
cases=$1
seal=$2
prior=$3
out=$4
pack=${MODEL_PACK:-internal/notesmodel/assets/bert-agreement.bin.gz}
parent=$(cd "$(dirname "$out")" && pwd -P)
case "$parent/" in
    "$(pwd -P)/"*) echo "Keep replay outputs outside this checkout" >&2; exit 2 ;;
esac
PYTHONPATH=tools/review-evaluation python3 -B -c \
    'from pathlib import Path; import sys; from score import sealed_inputs; sealed_inputs(Path(sys.argv[1]), Path(sys.argv[2]))' \
    "$cases" "$seal"
mkdir "$out"
ffmpeg -nostdin -hide_banner -loglevel error -f lavfi -i 'anullsrc=r=44100:cl=mono' -t 0.02 \
    -map_metadata -1 -c:a flac "$out/synthetic.flac"
go build -trimpath -tags review_evaluation -o "$out/app-evaluation" ./tools/review-evaluation
go version > "$out/go-version.txt"
"$prior" < "$cases" > "$out/prior-catalog.jsonl"
GOMAXPROCS=1 "$out/app-evaluation" --timing "$out/us-timing.json" --pack "$pack" --synthetic-flac "$out/synthetic.flac" --scratch "$out" \
    < "$cases" > "$out/us.jsonl"
GOMAXPROCS=1 "$out/app-evaluation" --timing "$out/strict-timing.json" --review-ambiguous --pack "$pack" --synthetic-flac "$out/synthetic.flac" \
    --scratch "$out" < "$cases" > "$out/strict.jsonl"
python3 -B tools/review-evaluation/score.py "$cases" "$seal" "$out/prior-catalog.jsonl" "$out/us.jsonl" \
    "$out/strict.jsonl" "$pack" "$out/app-evaluation" "$out/synthetic.flac" "$prior" \
    --us-timing "$out/us-timing.json" --strict-timing "$out/strict-timing.json" > "$out/results.json"
