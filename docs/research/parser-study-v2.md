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

The acquisition target is 500 fresh packages: 250 training, 50 validation, and
200 test, grouped before splitting. Original notes and filename inventories are
retained externally with source hashes; audio is not downloaded. The old corpus
is development/regression material. Fresh held-out sources must pass the frozen
performance, uploader, source-template and manually reviewed family exclusions.

Final acquired counts, split hashes, source-family audit findings, and independently
reviewed reference coverage will be populated from the frozen manifests. Training
labels are drafts. Validation/test labels are independently source-reviewed before
candidate predictions; they are agent-reviewed, not human-certified.

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

Classifier proposals are separate from parsed values and target artist, date,
venue and city only. They use source-backed header spans, abstain on ambiguous
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
Final measurements and the spending reconciliation are pending.

This phase has a separate incremental **$50 ceiling**. Its shared ledger reserves
worst-case request costs and retains uncertain charges. Final charged cost, retained
reserves, and remaining headroom will come from that ledger; unused budget need
not be spent. Previous-run accounting remains unchanged.

See the [frozen evaluator and runner instructions](../../experiments/parser/STUDY_V2_EVALUATION.md)
for source/model/executable binding, offline replay, resumption and failure handling.
Public exports contain whitelisted metrics, hashes and hashed failure identifiers.
Original notes, provider responses, credentials, private uploader data and downloaded
weights remain outside Git. Final manifests, sanitized summaries, model provenance,
source-audited failure examples and a recommendation will accompany the completed
study. Merging and release publication remain separate decisions.
