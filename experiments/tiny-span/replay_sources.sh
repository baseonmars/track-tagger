#!/usr/bin/env bash
# Run from the research checkout. Replays the approved external source freeze offline.
set -euo pipefail
cache=${CACHE:-../track-tagger-experiment-data}
study=${STUDY:-../track-tagger-parser-study-v2}
context=${CONTEXT:-../track-tagger-parser-context}
frozen="$cache/tiny-span-blind-final-v3"
scratch=$(mktemp -d)
trap 'rm -r "$scratch"' EXIT

python3 -B experiments/tiny-span/inventory.py "$cache" "$study" "$context" > "$scratch/source-inventory.json"
cmp "$scratch/source-inventory.json" experiments/tiny-span/results/source-inventory.json

priority=(--priority "$cache/tiny-span-blind/provisional-review-inputs.jsonl"
          --priority "$cache/tiny-span-blind-diverse/early-review-inputs.jsonl")
for number in {2..9}; do
    priority+=(--priority "$cache/tiny-span-blind-snapshot-$number/blind-inputs.jsonl")
done
priority+=(--priority "$cache/tiny-span-blind-final/blind-inputs.jsonl"
           --priority "$cache/tiny-span-blind-final-v2/blind-inputs.jsonl")
python3 -B experiments/tiny-span/seal_sources.py "$cache" "$study" "$scratch/sealed" \
    "$cache/tiny-span-blind" "$cache/tiny-span-blind-diverse" \
    "$cache/tiny-span-blind-historical" "$cache/tiny-span-blind-deposit" \
    --package-index "$frozen/package-index.json" "${priority[@]}"
for artifact in package-index.json blind-manifest.jsonl blind-inputs.jsonl blind-freeze.json; do
    cmp "$scratch/sealed/$artifact" "$frozen/$artifact"
done
cmp "$scratch/sealed/blind-freeze.json" experiments/tiny-span/results/blind-source-freeze.json

reviews="$cache/tiny-span/blind"
references="$reviews/canonical-reference"
python3 -B experiments/tiny-span/assemble_blind.py "$frozen/blind-inputs.jsonl" "$reviews" "$scratch/references"
python3 -B experiments/tiny-span/blind_seal.py "$frozen/blind-inputs.jsonl" \
    "$scratch/references/adjudicated.jsonl" "$scratch/references/reference-proof" \
    "$scratch/references/sealed" "$study"
cmp "$scratch/references/adjudicated.jsonl" "$references/adjudicated.jsonl"
diff -r "$scratch/references/reference-proof" "$references/reference-proof"
diff -r "$scratch/references/sealed" "$references/sealed"
