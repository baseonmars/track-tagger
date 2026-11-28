# Frozen controls on the two product pilots

The 13-recording pilot and its 21-recording extension used separate source
references sealed before inference. The 13 cases came from cached acquisitions
with no known prior labels or predictions. The extension used the first 21
components in the frozen 84-component selection from a new acquisition pass.
Two independent readers reviewed every recording and all 440 and 517
nonblank lines respectively. The remaining 63 components were held back.
[Product replay and reference identities](reviewer-app.md)

| Arm | Pilot exact four, all / 13 | Pilot exact four, review / 7 | Pilot correct / offered | Extension exact four, all / 21 | Extension exact four, review / 16 | Extension correct / offered |
|---|---:|---:|---:|---:|---:|---:|
| Frozen rules | 2 | 0 | — | 5 | 5 | — |
| Deterministic span control | 2 | 0 | 0/0 | 5 | 5 | 0/0 |
| Frozen section-aware classifier | 2 | 0 | 2/3 | 8 | 8 | 8/10 |
| Frozen encoder with trained head | 2 | 0 | 1/2 | 7 | 7 | 8/13 |
| Full encoder adaptation | 3 | 0 | 4/4 | 9 | 9 | 10/10 |
| LoRA encoder adaptation | 5 | 1 | 9/11 | 10 | 9 | 14/19 |
| Product suggestions, guarded agreement | 3 | 0 | 3/3 | 9 | 9 | 8/8 |

Exact four compares artist, concert date, venue and city together, overlaying
the arm's suggestions for scoring. Failures stay in the denominator. The six
research arms use the same frozen current-main catalog scan and retain their
existing thresholds and proposal guards. The product uses the actual
`WebBackend.Suggestions` path and its own scope guards. Product results were
identical under both numeric date policies on these pilots. The original
catalog exact-four counts agreed between the two runners.

A correct research proposal has valid literal source offsets, applicable
scope, matching reviewed role and normalized value, and matching annotated
offsets when present. All correct research proposals here matched separately
annotated offsets. Product scoring validates the submitted source slice and
compares its role, scope and value. These are observed proposal counts for
each arm's own eligibility policy. The research line-level metrics count
emissions before recording-level proposal selection. [Product scorer](../../tools/review-evaluation/score.py)

The 13-case pilot had 12 successful matched scans and one shared zero-recording
failure. The extension had 20 matched scans and one shared zero-recording
failure. No model dispatch failed. The failures block strict parity under
the original gate. Each pilot falls short of 180 adjudicated references and
20 emitting recordings per enabled role. The observed 3/3 and 8/8 product
precision has Wilson 95% intervals of 43.9–100% and 67.6–100% respectively.

LoRA improved the 21-case review cohort by four exact matches over rules
and one over the section-aware classifier. Its paired source-family 95%
interval versus the classifier was −12.5 to +25.0 percentage points. LoRA
and the section-aware classifier degraded two and one previously correct
fields respectively on that extension. The frozen head degraded two. No
research overlay lost a previously exact four-field review case. These small
pilots leave the product's relative utility and per-role precision uncertain.

### Source checks

Indexes are zero-based and character endpoints are exclusive. Digests bind
normalized source notes and original relative filenames.

| Source SHA-256 | Note line and span | Observed LoRA control error |
|---|---|---|
| `7f4df811f5067b1ad6693e3cb5047c85a4da54f7957c2f8b0bb2bb5777bc59b6` | 2, [0, 8) | Selected a city prefix for venue. The venue appears on the preceding date line. |
| `175ef86cbf1095b61ce80f341c4c78ee7b2777f9931bc958c936f0c5a5b702a7` | 0, [0, 31) | Selected the entire artist-and-venue header for venue. |
| `eab528e14cf43dcc10cfe93303ff590ae70ed03fc834d204659d78490ecc9006` | 2, [0, 14) | Clipped the second half of a venue name. |
| `79fcba78334b253f46dd8443089f69cc57826482716dbbce50f444bbb4923a8b` | 2, [0, 15) | Selected a private birthday event title as a physical venue. |

None of these four cases displayed a venue proposal in the product replay.
In the extension, seven city proposals and one venue proposal were correct.
The pilots displayed no artist or date proposal.

## Replay

Use the archived external #38 study checkout, the frozen section-aware
checkout, and the tiny-span evaluation worktree with its execution manifest.
The seals bind the case, recording-reference, line-reference and execution
digests. Keep notes, references, models and the private per-case audits in the
external cache.

```sh
cache=../track-tagger-experiment-data
proof="$cache/product-fresh"
study=../track-tagger-parser-study-v2
context=../track-tagger-parser-context
exp=../track-tagger-tiny-span/experiments/tiny-span
py="$cache/tiny-span/.venv/bin/python"
out=$(mktemp -d)
export HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 TOKENIZERS_PARALLELISM=false PYTHONDONTWRITEBYTECODE=1

for count in 13 21; do
  cases="$proof/cached-screen/blind-inputs.jsonl"
  ref="$proof/reference-13"
  if [ "$count" = 21 ]; then
    cases="$proof/extension-stage-21/inputs.jsonl"
    ref="$proof/extension-stage-21/sealed"
  fi

  "$py" "$exp/evaluate.py" fresh --cases "$cases" --seal "$ref/seal.json" \
    --gold "$ref/recording-gold.jsonl" --lines "$ref/line-gold.jsonl" \
    --catalog "$cache/tiny-span-eval/catalog" --scorer "$cache/v2/noterole-features" \
    --model "$cache/v2/classifier-model/model.json" \
    --date-catalog "$cache/v2/three-arm-build/catalog-fixed" \
    --study "$study" --context "$context" \
    --output "$out/control-$count.json" --audit "$out/control-$count-private.json" \
    --package "head_only=$cache/tiny-span/run/head" \
    --package "full_adapted=$cache/tiny-span/run/full" \
    --package "lora_adapted=$cache/tiny-span/run/lora"

  "$py" "$exp/gates.py" "$out/control-$count.json" "$cases" \
    "$ref/recording-gold.jsonl" "$out/control-$count-gates.json"
done
```

The sealed source assembly, acquisition and cache replay are recorded in the
external `product-fresh/SOURCE_REPLAY.md`. The frozen research output and
private audit reproduced byte-for-byte across two runs.
