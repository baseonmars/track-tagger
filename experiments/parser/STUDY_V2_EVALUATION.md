# Source-frozen three-arm evaluator

`study_v2_evaluation.py` evaluates saved predictions only. It makes no model calls.
Keep source gold, line labels, and predictions outside Git. Existing experiment gold
and scores remain immutable.

Freeze independently reviewed source gold first; after validation fixes the model
and enabled roles, create the final evaluation protocol before test inference:

```sh
python3 -m experiments.parser.study_v2_evaluation freeze \
  --sources /external/sources.jsonl --recording-gold /external/recording-gold.jsonl \
  --line-gold /external/line-gold.jsonl --model-sha256 MODEL_SHA256 \
  --output /external/frozen-protocol.json
python3 -m experiments.parser.study_v2_evaluation evaluate \
  --protocol /external/frozen-protocol.json \
  --recording-gold /external/recording-gold.jsonl --line-gold /external/line-gold.jsonl \
  --arms /external/arms.json --line-predictions /external/line-predictions.jsonl \
  --output /external/evaluation.json
```

Outputs are exclusive-created: neither operation overwrites a prior freeze/result.
Both commands print the canonical JSON SHA256, which prediction dispatch must bind
as `freeze_sha256`. An earlier source-label freeze is separate from this final
model-and-source evaluation protocol.

## Inputs

- Source rows: `id`, `group`, `source_input_sha256`, `split: "test"`,
  `notes: {relative_note_name: normalized_line_count}`. Counts must be derived from
  actual canonical input, not annotation coverage. All supplied source lines must
  have labels before freezing.
- Recording gold: `id`, `source_input_sha256`, `label_status`,
  `metadata: {artist,date,venue,city}`, `review_required`. Empty strings explicitly
  mean unknown. Optional `titles`/`mappings` allow newly accepted track-value checks.
  Labels are `agent-reviewed`/`adjudicated` or explicitly `disputed`/`unresolved`.
  Unresolved recordings remain reported in the frozen inventory but are ineligible.
- Line gold: existing complete-line study rows including `id`, `note`,
  `line_index`, `line`, `roles`, `role_disputed`, the explicit boolean flags
  `mixed_scope`/`uncertainty`/`conflict`, `source_input_sha256`,
  `label_status`. Unicode codepoint offsets refer to the normalized source line.
- Arms JSON: `baseline`, `fixed`, `assisted`, each containing `application_commit`
  and `rows`; assisted additionally binds `model_sha256`. Application revisions
  are pinned to `5fd190b` and `fece1a0`. Each row has `id`, source hash, freeze hash,
  and `output: {metadata,ready,review_reasons,mappings,write_plan}` or `error`.
- Assisted rows additionally contain `proposals`, a list of at most one candidate-
  selected proposal per enabled field: `{field,value,note,line_index,start,end,quote}`.
  Proposals are evaluated separately and must not mutate the underlying output.
  Their source-line `mixed_scope`, `uncertainty`, and `conflict` gold flags must be
  false for an eligible current-recording metadata proposal. These ground-truth
  flags measure errors only; they never filter production predictions or rewrite
  candidate outputs. Their exact source spans must reference a nonabstaining line prediction emitting
  the same role. No oracle-guided selection among multiple proposed values occurs.
- Line predictions: `id`, `note`, `line_index`, `source_input_sha256`,
  `freeze_sha256`, `model_sha256`, `roles`, plus optional `abstention` or `error`.
  Abstentions emit no roles. Missing/error outputs count against coverage.

## Measures and gates

The review cohort is exactly the eligible source-gold `review_required` recordings,
not a candidate-selected subset. Metadata accuracy compares all four fields together
with whitespace normalization only. Proposal accuracy overlays candidate-selected
suggestions onto the fixed parser's values without changing production output.
Abstentions retain the fixed value; failed recording outputs count as incorrect.

Role precision is evaluated separately for enabled metadata roles with at least 20
supporting recordings. Background false positives use every nondisputed line labeled
only `other`, `technical_provenance`, or `blank`; the report also counts affected
recordings. Role-disputed lines remain in coverage and are explicitly excluded from
semantic precision/recall denominators. No undocumented removal of failures occurs.

Quantitative gates enforce 180 eligible recordings, 95% per-role precision, 1% maximum
background false-positive rate, 5 percentage-point paired metadata improvement,
source-backed proposals, complete outputs, and exact underlying output parity.
Newly accepted wrong metadata, available track gold, and missed review are flagged
separately from inherited baseline errors. Those checks are proxies; independent
source audit must determine material accepted issues.

Passing numerical gates never sets `integration_approved`: independent source audit,
performance/artifact checks, cross-platform validation and integration review remain
required. Paired improvement intervals resample recording groups. Wilson role
intervals are descriptive line-level intervals and do not establish safety.
