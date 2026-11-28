# Can a tiny pretrained model suggest concert metadata?

**Keep LoRA BERT Tiny as a reviewer-only research prototype. Keep the current parser.** On 128 fresh recordings independently marked for review, applying the suggestions would make all four metadata fields exact on 31 recordings, up from 12 with rules. Nineteen recordings gained an exact result and none lost one. The gain was 14.84 percentage points (paired family bootstrap 95% interval +9.38 to +21.09). The gain over the prior section-aware classifier was four recordings, +3.13 points (−3.13 to +9.38). That comparison remains open. Five suggestions would fill fields that reviewers correctly left unknown. The deterministic span control produced no review-cohort gain.

## Comparison

Exact four means the artist, date, venue and city all match the sealed reference after overlaying the displayed proposals. Failed catalog scans count in the denominator. The validation and old regression cohorts had previous exposure. The fresh cohort was fixed before inference.

| Method | Validation exact / 24 | Old regression exact / 96 | Fresh exact / 128 | Fresh proposals with correct value and role / shown | Fresh fields repaired / degraded |
|---|---:|---:|---:|---:|---:|
| Current rules | 3 | 4 | 12 | — | 0 / 0 |
| Deterministic span extraction | 3 | 5 | 12 | 6 / 7 | 6 / 1 |
| Prior section-aware classifier | 8 | 16 | 27 | 124 / 152 | 125 / 3 |
| BERT with only the head trained | 6 | 9 | 17 | 80 / 100 | 81 / 4 |
| BERT fully fitted | 6 | 11 | 20 | 112 / 118 | 113 / 3 |
| BERT with rank-4 LoRA and fitted head | 9 | 14 | **31** | **167 / 188** | **171 / 5** |

Training contributed. The same pretrained encoder with an untrained span head had no role pass validation calibration and therefore reproduced rules under its abstain-all policy. LoRA recovered more fields than either trained alternative. Compared with full fitting, LoRA gained 8.59 points on fresh review-cohort exact four (paired 95% interval +3.91 to +14.06). Full fitting produced fewer proposals with higher joint precision. LoRA's fresh joint precision was **88.8%** (Wilson 95% interval 83.5–92.6%). Of 188 proposals, 166 also matched independently annotated offsets, one matched the role and normalized value with a different boundary, 12 had a wrong value, six used scope-flagged evidence and three used the wrong role. A matching value from a conflicting or mixed-scope line can repair the field in the overlay and still fail the evidence test. [Per-arm metrics](results/fresh.json) · [Gate and suggestion counts](results/fresh-gates.json)

| Fresh LoRA field | Correct role lines / emitted | Recording support | Role recall | Exact value and current role / suggestions shown |
|---|---:|---:|---:|---:|
| Artist | 99 / 101 | 99 | 99 / 221 | 3 / 3 |
| Date | 145 / 145 | 144 | 145 / 211 | 7 / 8 |
| Venue | 77 / 80 | 80 | 77 / 194 | 54 / 67 |
| City | 137 / 138 | 137 | 137 / 194 | 103 / 110 |

All four line-role precision point estimates cleared 95% with support above 20. The lower 95% bounds were 93.1% for artist and 89.5% for venue. LoRA emitted on zero of 3,633 background lines (Wilson upper bound 0.11%). It abstained on 6,789 of 7,238 lines and showed no proposed change on 77 of 200 recordings, including the 11 with failed catalog scans. The distinction matters most for venues, where 96.3% line-role precision translated into 80.6% correct, source-supported suggestions. Field-specific intervals and counts for every arm are in the linked results.

Of the 188 displayed suggestions, **171 had the exact field value** (91.0%, Wilson 95% interval 86.0–94.3%). By role, this was 3/3 artist, 7/8 date, 55/67 venue and 106/110 city. LoRA repaired 171/366 known missing or wrong fields eligible for suggestions (46.7%, 95% interval 41.7–51.8%), or 171/370 across successful scans. It preserved all 322 correct nonblank fields in those scans. Another 39 known fields sat behind catalog failures. [Exact value, repair recall, degradation and intervals for each arm](results/fresh-suggestions.json)

## What the source said

Offsets are zero-based Unicode codepoints within the cited note line, with an exclusive end. Every proposal retains its field, normalized value, exact quote, filename and offsets, checked against the original decoded note. Full audits stay in the external replay directory.

- **Combined header, fresh.** In `wdshed2005-5-18.txt`, line 1 reads “5/18/2005 Sully's Pub, Hartford, CT”. LoRA supplied city “Hartford, CT” at [23,35), filling the blank left by rules. On the previously scored Asheville example in the request it abstained on city. [Fresh source](https://archive.org/details/wdshed2005-5-18.flac16)
- **Wrong role, fresh.** In `WAHFU-2026-09-19.txt`, line 2 [0,9) is “Porchfest”. The reference identifies a festival and leaves the physical venue unknown. LoRA offered “Porchfest” as venue. Its separate city proposal on line 3 was correct. [Fresh source](https://archive.org/details/WAHFU2026-09-19)
- **Conflicting date, fresh.** The Driftwood note's filename ends in `2026-07-17.txt`, while line 4 [0,13) says “July 18, 2026”. LoRA would fill the correctly blank date with 2026-07-18 despite the recorded conflict. [Fresh source](https://archive.org/details/Driftwood2026-07-18.MV)
- **Wrong artist, old regression.** In `311-1997-12-06_KROQAcousticXmas.txt`, line 2 [0,21) names “Universal Ampitheater”. LoRA offered that venue as the artist, replacing the correct artist, 311. This failure and country or district truncation help explain why success at identifying a metadata line overstates reviewer value. [Regression audit](EVALUATION.md)

## References, completeness and parity

The earlier 211 packages had all been scored and only four had independently reviewed recording references. The inventory found zero eligible unexposed cached components. We acquired public notes and relative filenames, grouped recording, performance, uploader and recognizable template connections against prior sources, and selected 200 distinct new components. Repeated headers and fingerprint-only templates were explicitly excluded. Similarity detection can miss an undocumented relationship. [Source inventory and grouping](SOURCE_INVENTORY.md)

Four Codex sessions (the coordinating reviewer and three GPT-6.2 Sol agents) produced and independently cross-reviewed source readings. Each selected case had at least two readers. They saw the notes and relative filenames and were blind to predictions on those cases. Gold, field evidence, review membership, every line and file inventory were sealed before scoring. These are agent adjudications without human certification. A local Qwen3-1.7B draft-labeling pilot made 37 calls (48,185 input and 48,199 output tokens). Source review found unreliable evidence and joins. No draft was automatically accepted. An audited 49-case subset had eight initial field-value disagreements and four review-cohort disagreements. Explicit unknowns survive adjudication. [Agreement audit](EVALUATION.md#fresh-reference-agreement-before-adjudication)

All 200 recording inventories were adjudicated, with 5,899/5,901 nonblank lines reviewed (99.97%). Of 2,427 file joins, 420 remained unresolved. Only 124 recordings had every join resolved, 156 had all four fields populated and 101 satisfied both. The original 180-reference test counts genuine unknowns in a reviewed inventory. A requirement for 180 fully populated metadata-and-track references would fail.

Two independent disposable-fixture scans had identical metadata, readiness, review reasons and file-to-title mappings on 189/200. Nine yielded no recording, one yielded two, and one had a relative filename with a colon that the portable research harness rejected. All 11 stayed in the evaluation. Strict completeness and parity therefore failed. No suggestion invoked a writer or modified an underlying scan. Two source-required reviews had already been accepted by rules. The offline overlay introduced zero new acceptances. Full app write planning and cross-platform deployment were outside this experiment.

## Synthetic results and cost

On 240 held-out synthetic notes with 192 positive references per field, LoRA returned 33/33 exact artist, 136/136 date, 67/76 venue and 30/43 city spans. On nine additional positive/incidental pairs it got 12/18 decisions right. It distinguished a show date from upload, filename and historical dates, but proposed an itinerary city and missed a multi-line venue. The selector searches the first contiguous header and proposes only a single-line slice, limiting recall by design. [Stress results](results/challenge.json) · [Training, controls and grouped validation](PILOT.md)

The Apache-2.0 Google BERT Tiny checkpoint was trained locally on an Apple M2 Max. The rank-4 adapters and span head used 5,128 trainable parameters. The timed deterministic CPU refit and replay of all three fitted arms took 64.91 s wall. At one ONNX Runtime CPU thread the 300 warm end-to-end measurements over 100 training recordings included tokenization, selection, the review overlay and the Go date subprocess. Uncached p95 was **6.48 ms added CPU / 6.86 ms wall**, passing 25 ms. Fresh-process startup/load/exit was 666–677 ms with the OS cache warm. The merged network, vocabulary and policy occupy **6,161,820 bytes**. Including the standalone validator and installed inference dependencies brings the measured files to **216,613,957 bytes** before the interpreter and scripts. A self-contained macOS arm64 CPU bundle with the interpreter, runtime, tokenizer, weights, policy and frozen date validator was built and tested with network access denied. Its 105 files total **111,131,126 bytes**. It reproduced all 188 suggestions and every abstention on the 200 fresh recordings. Bundled warm p95 was **6.07 ms CPU / 6.48 ms wall** including tokenization and span selection, with a median **148 ms** fresh process start/load/exit (range 148–1,113 ms across three starts). The deployable artifact misses 10 MB by over elevenfold. Existing quantized SmolLM2-135M weights alone exceed 100 MB, so the generative LoRA/QLoRA design was researched but left untrained. Paid annotation, hosted inference and rented compute cost **$0**. The 5090 was unnecessary. Local electricity and the existing Codex session were not separately metered. [Model revisions, license and decoder design](MODEL_CHOICE.md) · [CPU measurements](PILOT.md#cpu-and-size) · [Offline bundle and exact build commands](MACOS_PACKAGE.md)

## Replay

From this worktree with the sibling caches and local Python 3.12 environment described in the pilot, run:

```sh
bash experiments/tiny-span/replay_sources.sh
FIT=1 bash experiments/tiny-span/replay-pilot.sh
bash experiments/tiny-span/replay.sh
```

The first command reproduces the source and reference seals. The second refits the encoder arms and checks their digests. The third compares offline scores on validation, the exposed regression set and the fresh holdout. It prints the directory containing private audits. The application baseline is `f8bd76b931fa828cfb9156a2d9a8f29a3aa3cb14`, the prior study is `1848decbea38a428a7e95e62c428cf7ffb1e6a73`, and the new source, recording and line reference SHA-256s are recorded in the fresh results. The execution freeze binds the prior section classifier, evaluator, selector, model files, tokenizer, enabled roles and thresholds. This fresh set is exposed for any later iteration. [Execution freeze](results/execution-freeze.json) · [Dataset revision](results/blind-source-freeze.json)
