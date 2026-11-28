#!/usr/bin/env bash
# Recompute the same overlays from the sealed private cache without contacting a service.
set -euo pipefail
cd "$(dirname "$0")/../.."
data=$(cd "${DATA:-../track-tagger-experiment-data}" && pwd)
study=$(cd "${STUDY:-../track-tagger-parser-study-v2}" && pwd)
ctx=$(cd "${CONTEXT:-../track-tagger-parser-context}" && pwd)
exp=experiments/tiny-span
py=${PYTHON:-$data/tiny-span/.venv/bin/python}
out=$(mktemp -d)
echo "Replay outputs in $out"
export HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 TOKENIZERS_PARALLELISM=false PYTHONDONTWRITEBYTECODE=1
f=$data/tiny-span-blind-final-v3
g=$data/tiny-span/blind/canonical-reference/sealed
catalog=$data/tiny-span-eval/catalog
for split in validation test fresh; do
  cases=$data/v2/replayed-cases.jsonl
  gold=$data/v2/$split-gold/recording-gold.jsonl
  lines=$data/context/run/$split-lines.jsonl
  seal=()
  expected=$split
  if [[ $split == test ]]; then expected=regression; fi
  if [[ $split == fresh ]]; then
    cases=$f/blind-inputs.jsonl
    gold=$g/recording-gold.jsonl
    lines=$g/line-gold.jsonl
    seal=(--seal "$g/seal.json")
  fi
  "$py" "$exp/evaluate.py" "$split" --cases "$cases" --gold "$gold" --lines "$lines" \
    --catalog "$catalog" --scorer "$data/v2/noterole-features" --model "$data/v2/classifier-model/model.json" \
    --date-catalog "$data/v2/three-arm-build/catalog-fixed" --study "$study" --context "$ctx" \
    --output "$out/$split.json" --audit "$out/$split-private.json" ${seal[@]+"${seal[@]}"} \
    --package "head_only=$data/tiny-span/run/head" --package "full_adapted=$data/tiny-span/run/full" \
    --package "lora_adapted=$data/tiny-span/run/lora"
  cmp "$out/$split.json" "$exp/results/$expected.json"
done
"$py" "$exp/gates.py" "$out/fresh.json" "$cases" "$gold" "$out/fresh-gates.json"
cmp "$out/fresh-gates.json" "$exp/results/fresh-gates.json"
"$py" "$exp/challenge.py" "$data/tiny-span/run/lora" "$out/challenge.json" "$data/context/fresh/fresh-inputs.jsonl"
cmp "$out/challenge.json" "$exp/results/challenge.json"
"$py" "$exp/suggestions.py" --result "$out/fresh.json" --audit "$out/fresh-private.json" \
  --cases "$cases" --gold "$gold" --lines "$lines" --catalog "$catalog" --output "$out/fresh-suggestions.json"
cmp "$out/fresh-suggestions.json" "$exp/results/fresh-suggestions.json"
