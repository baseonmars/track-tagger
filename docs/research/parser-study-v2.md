# Parser improvements and full-note classifier study

**Recommendation: retain rules-only parsing; do not integrate the classifier.**
The frozen classifier increased exact metadata proposals on the source-defined
review cohort from 4/96 to 12/96 (+8.33 percentage points; paired group bootstrap
95% interval +2.08 to +14.58 points), but every enabled metadata role failed the
95% test precision gate. Suggestions remained separate from parsed values. The
rules-only and assisted arms had identical outputs in all 176 successfully parsed
packages; the other 24 packages failed in both arms and fail the strict completeness
and parity gates. No new accepted errors were observed from the deterministic fixes.

The deterministic fixes remain independently reviewable in
[PR #36](https://github.com/baseonmars/track-tagger/pull/36), the experimental
classifier in [PR #37](https://github.com/baseonmars/track-tagger/pull/37), and this
study in [PR #38](https://github.com/baseonmars/track-tagger/pull/38). No classifier
has been activated in the application.

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
not human-certified. The test reference was sealed before candidate predictions: 199 recordings have
complete reviewed metadata/title/file references, and one remains unresolved. All
10,612 source lines remain in the denominator; 8,692/8,827 nonblank lines (98.47%)
have reviewed, nondisputed role references. The unresolved source was not converted
to background labels.

Recording and line-label eligibility are tracked separately. Unknown line roles
remain explicitly unresolved rather than becoming background examples. Complete
metadata alone cannot certify a recording when title/file-reference coverage is
missing. The frozen summaries retain requested cases, eligible denominators, annotation
failures, prediction failures, abstentions and unresolved references.

Source authors and reviewers used separate contexts with notes and relative filenames,
without candidate outputs. Most held-out references used two Sol contexts; long-note
or failed-output cases used Astra or independent source-first agent adjudication.
A failed or truncated author did not count as an independent successful label pass.
The test author produced 183 complete valid outputs from 200 requests; the reviewer
produced 185, with valid components retained and explicit source adjudications sealed
before prediction. NTL exceeded both Sol and Astra output caps, then received full
manual source authoring and independent review; LOS remained unresolved. The
provider failures and charges remain in the audit and budget denominators.

Pilot schema refinements replaced model-counted character offsets with unique exact
quotes and deterministic Unicode offsets. A later JSONL reader fix preserved literal
Unicode line separators; it landed before the final execution manifest and before
any candidate test prediction. Neither changed the frozen source text or split.
The source audit clarified cover-credit/segue preservation, historical metadata scope
and technical-only uncertainty before held-out annotation. The reused canonical
mapping schema cannot express a known file ID with unknown title as a complete join;
those joins stay explicitly unknown, with known IDs retained separately in the audit.

A five-case training-pilot source audit examined 224 lines, 20 core metadata fields
and 85 numeric mappings. It found no material core-metadata or numeric-mapping
errors in that small sample, but identified inconsistent cover-credit/segue
normalization, historical/guest metadata applicability, and a missing technical
uncertainty flag. Those findings informed the source-label protocol before held-out
labels; they are not a general accuracy estimate.

## Comparison and decision gates

| Candidate | Complete-recording accuracy | Metadata proposal accuracy on frozen review cohort | Reference-required reviews accepted | Coverage / failures |
|---|---|---|---|---|
| Pinned baseline | 1/199 | 4/96 | 2 false-ready; audit below | 200 requested; 24 failures |
| Deterministic fixes | 2/199 | 4/96 | Same 2 false-ready | 200 requested; 24 failures |
| Fixes plus classifier suggestions | 2/199 | 12/96 proposed; parsed values remain 4/96 | Same underlying output | 176 exact output pairs; 24 shared failures |

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

The 199 eligible recording references include failures in all accuracy denominators.
All three arms matched all four metadata fields in 21/199 cases and accepted ten
eligible recordings; two of those were review-required by the source reference.
Complete-recording exactness additionally requires all title and file joins.
The review cohort contains 96 source-defined review-required recordings. Proposed
metadata is a counterfactual suggestion metric, not a mutation or automatic acceptance.

| Enabled role | TP / FP / FN | Precision (Wilson 95%) | Recall |
|---|---|---|---|
| Date | 209 / 59 / 19 | 77.99% (72.65–82.53%) | 91.67% |
| Venue | 54 / 4 / 168 | 93.10% (83.57–97.29%) | 24.32% |
| City | 109 / 14 / 102 | 88.62% (81.80–93.10%) | 51.66% |

The full eleven-role diagnostic scores 10,477 reviewed lines; 135 unknown-role
lines remain outside supervised role metrics. Exact multilabel agreement was
6,322/10,477 (60.34%), and macro F1 was 0.52737. All 10,612 frozen Go replay rows
matched the saved roles and scores exactly. These are post-freeze diagnostics;
they did not change the primary evaluator, model or role thresholds. Macro F1 is
a line-role diagnostic, not overall parser accuracy. Blank-role F1 is zero because
the frozen runtime intentionally abstains on OOV blank input; it does not measure
a learned blank-line detector. Continuation F1 (0.030) and other/prose F1 (0.238)
remain substantive weaknesses. No post-test blank-line normalization was added to
the primary scores.

| Role | TP / FP / FN | Precision | Recall | F1 |
|---|---|---|---|---|
| artist | 306 / 48 / 418 | 86.44% | 42.27% | 0.568 |
| date | 209 / 59 / 19 | 77.99% | 91.67% | 0.843 |
| venue | 54 / 4 / 168 | 93.10% | 24.32% | 0.386 |
| city | 109 / 14 / 102 | 88.62% | 51.66% | 0.653 |
| title | 3097 / 131 / 620 | 95.94% | 83.32% | 0.892 |
| heading | 407 / 14 / 137 | 96.67% | 74.82% | 0.844 |
| event | 18 / 2 / 40 | 90.00% | 31.03% | 0.462 |
| continuation | 2 / 3 / 128 | 40.00% | 1.54% | 0.030 |
| technical_provenance | 3006 / 63 / 695 | 97.95% | 81.22% | 0.888 |
| other | 60 / 67 / 318 | 47.24% | 15.87% | 0.238 |
| blank | 0 / 4 / 1785 | 0.00% | 0.00% | 0.000 |

Across all eleven roles, the classifier emitted on 7,350 lines and abstained on
3,262 (1,802 OOV, 1,139 no-role, 296 margin, 25 incompatible-role). By contrast,
10,180 lines had no **enabled metadata** emission, including lines correctly
classified into title, technical or other roles. That number is not the raw
classifier abstention count. Only 432 lines emitted an enabled metadata role.

The enabled metadata heads produced false positives on 46/5,037 background lines
(0.913%), affecting 20/200 recordings. On the same background definition, validation had 2/1,361 false-positive lines
(0.147%); the earlier 2/2,621 denominator also included other metadata roles and
is not directly comparable. The background gate passed; the per-role
precision gate failed. Role labels measure line content, while proposal eligibility
also requires current-recording applicability. No emitted proposal used a
source-reference span marked uncertain, conflicting or mixed-scope. Marginal line
intervals are descriptive and do not establish population safety. The fixed model,
thresholds, role allowlist and executable identities were frozen before this test;
no test-driven tuning or classifier activation followed the results.

See the [frozen summary](../../experiments/parser/study_v2/results/test-evaluation-summary.json)
and [strict parity diagnostic](../../experiments/parser/study_v2/results/test-parity-diagnostic.json).
The evaluator conservatively flags missing outputs as parity failures; the diagnostic
separately confirms that all 176 successful output pairs are equal and all 24
failure pairs have the same error class. Neither reclassifies a failure as a parse.

Representative source-checked false role emissions illustrate the failure modes:
a date head selected a technical upload timestamp and a dated track filename; a city
head selected a technical file list and a personnel roster; a venue head selected
a project alias and historical narrative. These are line-role errors, not automatically
accepted metadata values. The [dequoted examples](../../experiments/parser/study_v2/results/classifier-test-false-role-examples.json)
retain case IDs, source hashes and line/span offsets without original source quotes.
No labels, thresholds or proposals changed after this inspection.

## Accepted cases and failure interpretation

An independent source audit examined all ten ready cases: 535 source lines and
181 filenames. It found no newly ready cases, changed metadata or changed numeric
file identifiers from the deterministic fixes. Twenty title changes in two recordings
correctly removed duration annotations; 49 other differences only reordered review
reasons. These observations support the bounded parser fixes, not classifier activation.

The two strict reference-required reviews accepted by both parsers have different
causes. `bbragg2006-03-26` accepted an unknown title placeholder and omitted the city;
all 36 source files were present. `tm1995-03-17.flac16` has an existing venue/city split
error and a conservative review reference for checksum filename padding, while all
22 actual track-number joins are unambiguous. The strict false-ready count remains
two; it is not a count of proven numeric file swaps. Other ready-case metadata
representation differences remain in exact-match metrics.

The [dequoted audit](../../experiments/parser/study_v2/results/accepted-case-source-audit.json)
is agent-reviewed. Readiness here is a notes-and-filenames extraction decision, not
verified end-to-end media-write approval. Actual audio, existing tags and artwork
were not inspected; production integrity and preservation checks remain unchanged.

## Performance, cost and reproduction

The model-size target is 10 MB and the added per-recording CPU p95 target is 25 ms
on the M2 Max. Cold loading and large-note behavior are reported separately.
Research-runner wall timings include IPC and date validation and are not CPU
measurements. Windows functional checks do not establish Windows performance.
The frozen model occupies 7,500,280 bytes. Across 100 runs over 25 distinct training recordings (20 size quantiles and the five
largest, four repetitions each), the actual
feature, score and proposal-guard CPU path measured 5.228 ms p50 and 13.541 ms p95.
Cold model/parser startup measured about 56 ms separately; peak process RSS
was 46,465,024 bytes. These measurements exclude catalog
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
Original notes, provider responses, credentials, private uploader data and learned
weights/vocabulary remain outside Git. The linked manifests, sanitized summaries,
model provenance and source audits document the completed study. Merging and
release publication remain separate decisions.

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
rows from the 500 cached original packages. The same proof also reproduces all
10,612 test reference rows (reference digest
`7e08e50afc90507f2ac9e9c2bfc76a066744363621e7bd65d192b448595f1a1e`). This verifies cached bytes, not continued
remote availability. Context/span hashes verify source bytes; the published reference
digests must be trusted for the meaning of the agent-reviewed labels. These offline
commands require no API credentials.


Local verification passed 149 Python harness tests, with one intentional optional
classifier-dependency skip in the standard-library environment. The pinned full
requirements run that regression in CI. Remote checks are tracked on
[the study branch](https://github.com/baseonmars/track-tagger/actions?query=branch%3Afeat%2Fparser-study-v2).
The model/execution/gold protocol and all candidate outputs remain frozen; subsequent
work added source audits, diagnostics and documentation only.

Classifier fitting and validation selection use the committed
[`tools/noterole/study.py` at `96e8de2`](https://github.com/baseonmars/track-tagger/blob/96e8de2/tools/noterole/study.py)
in the classifier PR, rather than its generic trainer. From that checkout, with
rehydrated references in the external directory above:

```sh
python3 -m pip install -r tools/noterole/requirements.txt
go build -o /external/parser-study-v2/noterole-features ./cmd/noterole-features
python3 tools/noterole/study.py fit \
  /external/parser-study-v2/training-lines.jsonl \
  /external/parser-study-v2/noterole-features /external/parser-study-v2/refit
python3 tools/noterole/study.py select \
  /external/parser-study-v2/validation-lines.jsonl \
  /external/parser-study-v2/noterole-features /external/parser-study-v2/refit
```

Use the pinned dependency environment and a new output directory. The
[model freeze](../../experiments/parser/study_v2/results/model-freeze.json) records
actual parameters, thresholds, validation selection and artifact identities. Rebuilding
on another toolchain may change executable bytes; the published identities describe
the measured run. Learned vocabulary and weights stay external. The committed
`experiments.parser.study_v2.diagnostics` command reproduces all eleven-role metrics
from rehydrated line references and saved private predictions, optionally replaying
Go scores with `--scorer` and `--model`. Its committed-code check reproduced the
saved role metrics and abstention counts exactly. Candidate outputs remain private;
the published whitelist summary retains aggregate results and frozen input hashes.
