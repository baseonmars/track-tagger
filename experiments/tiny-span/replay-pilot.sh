#!/usr/bin/env bash
# Replay frozen pilot inference using cached packages. Optional FIT=1 refits locally.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
cd "$repo"
data=$(cd "${DATA:-../track-tagger-experiment-data}" && pwd)
python=${PYTHON:-$data/tiny-span/.venv/bin/python}
code=experiments/tiny-span
scratch=$(mktemp -d)
echo "Replay outputs in $scratch"
export PYTHONDONTWRITEBYTECODE=1 HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 TOKENIZERS_PARALLELISM=false
packages=$data/tiny-span/run
if [[ ${FIT:-0} == 1 ]]; then
  packages=$scratch/refit
  "$python" "$code/fit.py" --v2 "$data/v2" --base "$data/tiny-span/bert" --out "$packages"
  cmp "$packages/pilot-results.json" "$code/results/pilot-results.json"
fi
for arm in head full lora; do
  "$python" "$code/measure.py" --v2 "$data/v2" --package "$packages/$arm" \
    --check "$code/results/$arm-runtime.json" --output "$scratch/$arm-replay.json"
done
