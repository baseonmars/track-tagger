# Parser improvements and full-note classifier study

**Status: evaluation in progress.** The deterministic parser fixes are independently
reviewable in [PR #36](https://github.com/baseonmars/track-tagger/pull/36). Classifier
assistance remains a research candidate. No classifier has been activated in the
application, and no held-out result is claimed here.

The preceding classifier achieved 98.96% accuracy on selected, source-cited positive
lines. This study tests whole notes, including technical sections, ordinary prose,
multirole lines, and explicit uncertainty. Positive-line accuracy does not establish
reliable complete-recording extraction or safe automatic acceptance.

## Delivered parser changes

The fixed parser retains speculative venue and provisional-title text for review,
keeps performer aliases out of venue fields, and treats ambiguous event/location
headers conservatively. Terminal bracketed duration annotations are removed while
meaningful title punctuation and segues remain intact. Identifier matching and
media-writing behavior remain unchanged.

The comparison pins the current baseline to `5fd190b` and these fixes to `fece1a0`.
The synthetic regression corpus updates 82 title expectations across 18 fixtures
for bracketed durations; source notes are unchanged. Earlier experiment gold and
published scores remain frozen.

## Dataset and reference integrity

The frozen corpus contains 500 fresh packages: 250 training, 50 validation, and
200 test, in 421 source components grouped before splitting. The largest artist
alias contributes four packages (0.8%). Original notes and filename inventories are
retained externally with source hashes; audio is not downloaded. The old corpus
is development/regression material. Fresh held-out sources must pass the frozen
performance, uploader, source-template and manually reviewed family exclusions.

The [source manifest](../../experiments/parser/study_v2/results/source-split-manifest.json)
is frozen at SHA-256 `1a4382766bd306db1c107ab89838abe503e3b9f21f6158e8afd6fc99e012bc2c`.
The source-only audit excluded 18 old-related contributor/template families from
held-out selection and verified all final source hashes and memberships. Technical
shingles, uploader/performance keys, source-email hashes and manual nearest-neighbor
review reduce related-source leakage; they do not prove every template is unseen.
Training includes new recordings from old uploaders, confined to training. Public
Archive notes may also have appeared in pretrained-model training data.

Training produced 239 complete valid drafts from 250 requested packages, covering
13,112 lines; eleven failed packages were excluded from fitting rather than relabeled
as negatives. All 50 validation packages received independent source review, covering
2,889 lines, including 2,440 nonblank lines. Validation/test labels are agent-reviewed,
not human-certified. Test source adjudication is still in progress; no candidate test
predictions have run.

Recording and line-label eligibility are tracked separately. Unknown line roles
remain explicitly unresolved rather than becoming background examples. Complete
metadata alone cannot certify a recording when title/file-reference coverage is
missing. The report will show every requested case, eligible denominator, annotation
failure, prediction failure, abstention and disputed reference.

A five-case training-pilot source audit examined 224 lines, 20 core metadata fields
and 85 numeric mappings. It found no material core-metadata or numeric-mapping
errors in that small sample, but identified inconsistent cover-credit/segue
normalization, historical/guest metadata applicability, and a missing technical
uncertainty flag. Those findings informed the source-label protocol before held-out
labels; they are not a general accuracy estimate.

## Comparison and decision gates

| Candidate | Complete-recording accuracy | Metadata proposal accuracy on frozen review cohort | Unsafe accepted cases | Coverage / failures |
|---|---|---|---|---|
| Pinned baseline | Pending measurement | Pending measurement | Pending source audit | Pending measurement |
| Deterministic fixes | Pending measurement | Pending measurement | Pending source audit | Pending measurement |
| Fixes plus classifier suggestions | Pending measurement | Pending measurement | Underlying output parity required | Pending measurement |

Validation selected C=10 from the predeclared C=0.1, 1, 10 candidates using actual
runtime emissions. Date, venue and city qualified for suggestions; artist did not
meet the validation precision threshold and remains disabled. Validation actual-runtime
macro F1 across the full role task was 0.55716. This metric measures a different task
from the preceding cited-positive-line accuracy.

Classifier proposals are separate from parsed values and target the enabled date,
venue and city roles only. They use source-backed header spans, abstain on ambiguous
source contexts, and pass date values through deterministic validation. They cannot
overwrite parsed metadata, clear review reasons, change readiness or assign files.
The research runner invokes no application writer.

Integration requires all quantitative gates plus independent source and engineering
review: at least 180 complete adjudicated recording references, at least 95% reviewed
nonblank line-reference coverage, at least 95% precision per enabled role across
20 or more recordings, no more than 1% background false-positive lines, and at least
five percentage points of paired metadata improvement on the frozen source-defined
review cohort. Underlying outputs must remain identical between rules-only and
assisted arms. Newly accepted material errors in the deterministic fixes block
those changes independently.

Per-role precision/recall, technical/background false positives, affected recordings,
paired uncertainty intervals, exact title/file results and representative failures
will be populated from saved predictions and independent audits. Passing numerical
gates alone does not approve integration; zero observed errors do not establish safety.

## Performance, cost and reproduction

The model-size target is 10 MB and the added per-recording CPU p95 target is 25 ms
on the M2 Max. Cold loading and large-note behavior are reported separately.
Research-runner wall timings include IPC and date validation and are not CPU
measurements. Windows functional checks do not establish Windows performance.
The frozen model occupies 7,500,280 bytes. On 100 training recordings, the actual
feature, score and proposal-guard CPU path measured 5.228 ms p50 and 13.541 ms p95;
peak sampled process RSS was 46,465,024 bytes. These measurements exclude catalog
precomputation and IPC. The incremental ledger settled 858 requests at $30.549540, with no active
reservations and $19.450460 unused under the $50 ceiling. The
[stage reconciliation](../../experiments/parser/study_v2/results/budget-accounting.json)
retains failed/truncated calls and pilot revisions. These are gross list-price
ledger charges with microdollar rounding, not the net invoice or free-credit use.

This phase has a separate incremental **$50 ceiling**. Its shared ledger reserved
worst-case request costs under aggregate stage caps; revision phases reused those
caps. All requests are settled and no further paid dispatch is planned. Previous-run accounting remains unchanged.

See the [frozen evaluator and runner instructions](../../experiments/parser/STUDY_V2_EVALUATION.md)
for source/model/executable binding, offline replay, resumption and failure handling.
Public exports contain whitelisted metrics, hashes and hashed failure identifiers.
Original notes, provider responses, credentials, private uploader data and downloaded
weights remain outside Git. Final manifests, sanitized summaries, model provenance,
source-audited failure examples and a recommendation will accompany the completed
study. Merging and release publication remain separate decisions.

## Source and full-line reference replay

The committed acquisition manifest lists exact original-note URLs, byte hashes and
relative filename inventories. The replay command downloads only those listed notes
when they are not cached; it downloads no audio and performs no discovery:

```sh
python3 -m experiments.parser.study_v2.replay \
  experiments/parser/study_v2/results/selected-acquisition-manifest.jsonl \
  experiments/parser/study_v2/results/source-split-manifest.json \
  /external/parser-study-v2
python3 -m experiments.parser.study_v2.refs rehydrate \
  experiments/parser/study_v2/results/training-line-refs.jsonl \
  /external/parser-study-v2/replayed-cases.jsonl \
  /external/parser-study-v2/training-lines.jsonl \
  --expected-sha256 e1206a2a35328445798dc515c41a853be45f61919fe04e3c7101c34940a9f369
python3 -m experiments.parser.study_v2.refs rehydrate \
  experiments/parser/study_v2/results/validation-line-refs.jsonl \
  /external/parser-study-v2/replayed-cases.jsonl \
  /external/parser-study-v2/validation-lines.jsonl \
  --expected-sha256 3b90293c94c10af68f55360e5f6d2936e7499caea6c5f5e4dd90597f42602027
```

Use an external output directory; reference rehydration refuses to overwrite an
existing output. The [actual replay proof](../../experiments/parser/study_v2/results/line-reference-replay-proof.json)
reproduced all 13,112 training and 2,889 validation feature, target and membership
rows from the 500 cached original packages. This verifies cached bytes, not continued
remote availability. Context/span hashes verify source bytes; the published reference
digests must be trusted for the meaning of the agent-reviewed labels. These offline
commands require no API credentials.
