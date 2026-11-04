# Overnight parser experiments

Retain the production parser and its review workflow. On the same 197 eligible real test cases, hosted models improved strict extraction accuracy but introduced more accepted safeguard violations: baseline four, Luna 22, Sol nine, Astra ten. Wrapped exact accuracy was 8/197, 69/197, 94/197 and 92/197, respectively; exact cases include correct review decisions, not just automatic resolutions. All three hosted candidates fail the observed no-increase criterion for automatic fallback. Local Qwen3 Q8 models accepted no eligible cases: successful outputs required review, and failed requests remained failures; exact accuracy was low. The classifier is promising only for assigning classes to selected cited lines; it has not established general line detection.

These results support further review-assistance experiments, while keeping production parsing and media-writing unchanged. The corpus is nonrandom, has a verified cross-split template family, and uses agent-reviewed labels. Future hosted assistance requires an explicit opt-in product decision because it sends notes and relative filenames outside the otherwise offline app; this experiment used public Archive material. Paid jobs have finished within the $80 total ceiling. Remote checks are tracked on the [feature branch Actions page](https://github.com/baseonmars/track-tagger/actions?query=branch%3Afeat%2Fparser-experiments).

## Corpus and independent labels

The frozen real corpus contains 1,200 complete Archive packages from 749 Archive artist labels across seven decades, with 1,642 notes and no audio. This is a downloads-ranked, artist-balanced selection, not a random Archive sample; 609 packages are from the 2000s. Artist labels include aliases and joined labels rather than entity-resolved people/bands. Recording groups stay within the fixed 800 train / 200 validation / 200 test split. The [split manifest](../../experiments/parser/results/real-splits.json) and [group audit](../../experiments/parser/results/group-audit.json) expose selection and grouping; heuristic grouping is not proof that every semantic duplicate was detected. A later [template integrity audit](../../experiments/parser/results/split-template-integrity-audit.json) confirmed a distinctive source-chain/section/footer family crossing train, validation and test (`afoshee2004-07-21.at831.shnf`, `trashcansinatras2005-05-14.at831.flac16`, `colmeda2004-12-17.at831.flac16`). Whole-package Jaccard values 0.071–0.109 fell below the 0.90 guard. Related-template separation was not fully achieved; this fixed benchmark does not establish unseen-template generalization. Original splits, gold and scores remain unchanged. Candidate inputs contain complete notes and relative filenames, with original Archive reference metadata withheld. All candidates receive canonical decoded UTF-8 text; original encodings remain in provenance (95 Windows-1252, five UTF-16 and 1,542 UTF-8/UTF-8-signature notes). This tests extraction parity, not native decoder coverage. Completeness includes original `.txt`, `.text`, `.nfo` and `.md` notes and original audio filenames, excluding derivatives; see [acquisition](../../experiments/parser/ACQUISITION.md).

The full test labels used independently sealed source authoring and source-only review by Astra, Sol and the harness agent before reconciliation with Luna drafts. Of the remaining 160 beyond common40, Astra authored 140 and the harness agent 20; the common40 routes and a separate Sol-authored Vulfpeck correction are documented in the source audit. Labels were reconciled using all 270 notes (366,490 characters) and 3,953 filenames. They are agent-reviewed, not human-certified. Reviewers and candidates share model families despite independent source-authoring contexts. Public Archive notes may also have appeared in pretrained-model corpora; that exposure cannot be verified or excluded. Of 200 cases, 197 have eligible labels: 81 expect ready and 116 expect review; three remain disputed and stay outside accuracy denominators. The fixed common-40 subset has 39 eligible labels (13 ready, 26 review), one disputed case, and 22 all-FLAC inputs / 21 all-FLAC eligible labels. See the [source-review protocol](../../experiments/parser/SOURCE_REVIEW.md).

The external reconciled labels have SHA256 `be073c3d1020158038ce6bc5252ebd5cb6e1b2df740a51eba060f766f1f89020`; the frozen full-test artifact has SHA256 `6102b07a348e08a342134b53d9d808fbc70eca6aff1d51003a8dbb58134c7b9f`. Originals, source quotes, raw responses, model weights, caches, credentials and the ledger remain outside Git under `/Users/dan/code/track-tagger-experiment-data`.

## Comparison rules and coverage

[Scorer v3](../../experiments/parser/results/final-scorer-policy.json) and the [shared r2 prompt](../../experiments/parser/results/candidate-r2-prompt.txt) were frozen before final test predictions. Scoring collapses whitespace and trims metadata/title values, preserving case, punctuation and segue markers. Empty-title mappings compare as zero disc / zero number / empty title. Free-form unresolved rationale is excluded from exact accuracy, with unresolved identifier agreement reported separately. Labels include city plus region and event plus physical venue where supported; differences are not automatically fabrication.

The evaluation retains raw extraction separately from the deterministic evidence/filename wrapper. Exact accuracy includes known metadata, titles, mappings and readiness. Title accuracy is keyed by canonical disc:track identifiers, not a bag of recognized title strings: correct text under a missing or wrong identifier does not count. False-ready compares readiness against independently reviewed ambiguity; incorrect accepted mappings and accepted nonexact cases are separate measures. `invented_info` is a mismatch/invention proxy, not verified hallucination. All eligible requested references contribute expected title/mapping denominators, including failed and unattempted cases; correct counts come from observed outputs. Missing requests remain a separate coverage gap. Error defaults can count unknown empty metadata fields as correct, so field accuracy must be read with failure coverage or a successful-output conditional table. The tables below and local report show the all-FLAC eligible stratum alongside the full cohort. Zero observed errors does not establish safety; universal review does not establish correct parsing. Here, ready and accepted describe catalog/experimental extraction decisions from notes and filenames, not verified end-to-end media-write approval. No real audio bytes, existing tags/artwork or payload hashes were inspected; the production app retains additional plan, integrity and preservation checks. Material accepted extraction issues do not establish that a real library was or would be mutated incorrectly through every application gate.

The same 40 requested packages and 39 eligible labels underlie this primary comparison; all three hosted models returned valid outputs for all 40. Readiness counts below use only eligible labels. The remaining disputed package was accepted by all hosted models; that unresolved-reference acceptance is recorded separately and is not a confirmed error.

| Common-40 r2 candidate | Raw exact / 39 | Wrapped exact / 39 (95% Wilson interval) | Ready / review or error | Accepted exact / nonexact | False-ready | Strict accepted mapping mismatches |
|---|---:|---:|---:|---:|---:|---:|
| Baseline | 0 | 0 (0–9.0%) | 0 / 39 | 0 / 0 | 0 | 0 |
| Luna | 16 | 10 (14.6–41.1%) | 9 / 30 | 2 / 7 | 3 | 52 |
| Sol | 17 | 12 (18.6–46.4%) | 6 / 33 | 3 / 3 | 1 | 0 |
| Astra | 19 | 15 (24.9–54.1%) | 7 / 32 | 4 / 3 | 1 | 0 |
| Qwen3 0.6B Q8 Metal | 0 | 0 (0–9.0%) | 0 / 39 | 0 / 0 | 0 | 0 |
| Qwen3 1.7B Q8 Metal | 0 | 1 (0.5–13.2%) | 0 / 39 | 0 / 0 | 0 | 0 |
| Qwen3 1.7B Q4 Metal (supplemental) | 0 | 1 (0.5–13.2%) | 0 / 39 | 0 / 0 | 0 | 0 |

The baseline produced 21 outputs and 19 scope/discovery errors. Qwen3 Q8 0.6B and 1.7B had 12 and 11 errors, and supplemental Q4 had nine; failed outputs remain in eligible denominators. Zero accepted issues from review/error outcomes is not evidence of useful automatic resolution. These small-cohort marginal intervals do not establish pairwise superiority.

| Common-40 fields, wrapped | Titles / 633 | Mappings / 743 | All-FLAC exact / 21 | All-FLAC titles / 274 | All-FLAC mappings / 343 |
|---|---:|---:|---:|---:|---:|
| Baseline | 134 | 121 | 0 | 134 | 119 |
| Luna | 594 | 476 | 4 | 249 | 213 |
| Sol | 633 | 531 | 6 | 274 | 262 |
| Astra | 626 | 523 | 7 | 267 | 254 |
| Qwen3 0.6B Q8 Metal | 16 | 49 | 0 | 16 | 34 |
| Qwen3 1.7B Q8 Metal | 364 | 249 | 0 | 162 | 107 |
| Qwen3 1.7B Q4 Metal (supplemental) | 166 | 172 | 0 | 89 | 74 |

All-FLAC has 22 requested packages and 21 eligible labels. Hosted false-ready and accepted mismatch counts are unchanged within that stratum. [Public cohort summaries](../../experiments/parser/results/summaries) include raw field metrics, complete coverage and supported-audio strata. The wrapper's identifier/citation policy can discard otherwise plausible model mappings; high title extraction accuracy alone does not establish safe file assignment.

The standalone baseline full-200 run completed 200 requests / 197 eligible labels: 159 outputs, 41 scope/discovery errors, 8 exact cases, 10 ready cases, five accepted exact and five accepted nonexact cases, two false-ready cases and 13 strict accepted mapping differences. Three of its eight exact cases correctly required review. Source audit found four material accepted issues and one separator-only difference. Of 13 strict mapping differences, 12 retain durations and one promotes an uncertain title; no numeric wrong-file swap was demonstrated. Do not compare this full-cohort safety count against a model's smaller common-40 count. All three hosted models completed the same 200 requested / 197 eligible cohort. Both Qwen3 full-200 runs completed.

| Full-200 candidate | Valid outputs / 200 | Raw exact | Wrapped exact / 197 (95% Wilson) | Ready / accepted exact | False-ready | Strict accepted mapping differences |
|---|---:|---:|---:|---:|---:|---:|
| Baseline | 159 | 8 | 8 (2.1–7.8%) | 10 / 5 | 2 | 13 |
| Luna | 199 | 88 | 69 (28.7–41.9%) | 72 / 32 | 22 | 123 |
| Sol | 200 | 114 | 94 (40.9–54.7%) | 67 / 46 | 9 | 13 |
| Astra | 199 | 109 | 92 (39.9–53.7%) | 66 / 45 | 10 | 13 |
| Qwen3 0.6B Q8 Metal | 150 | 0 | 1 (0.1–2.8%) | 0 / 0 | 0 | 0 |
| Qwen3 1.7B Q8 Metal | 138 | 1 | 5 (1.1–5.8%) | 0 / 0 | 0 | 0 |

Luna and Astra each had one genuine schema failure retained without retry. Source audits found 22 accepted safeguards violated for Luna (18 metadata/mapping/scope, three completeness-only, one fingerprint-only) and nine for Sol (five uncertain-title, two mixed-scope, two completeness). Astra had ten (five uncertain-title, two mixed-scope, three completeness) plus 11 presentation differences among its 21 accepted nonexact cases. The baseline had four material accepted issues on these same 197 cases. Even excluding completeness/integrity issues, the comparison is 18 / 7 / 7 / 4 for Luna / Sol / Astra / baseline. Luna's other 18 accepted nonexact cases and Sol's other 12 are presentation differences. Each hosted model accepted two disputed cases separately. No numeric wrong-file swaps were proven by these source audits. See [Luna audit](../../experiments/parser/results/luna-full200-accepted-source-audit.json), [Sol audit](../../experiments/parser/results/sol-full200-accepted-source-audit.json), and [Astra audit](../../experiments/parser/results/astra-full200-accepted-source-audit.json). Greater strict exact accuracy did not satisfy the no-increase acceptance safeguard criterion. Qwen3 0.6B accepted no eligible cases; successful outputs required review and errors remained failures; validated title/mapping accuracy was 17/3,153 and 519/3,888. Among 154 eligible all-FLAC cases (157 requested), it was exact in 1/154 with 17/2,376 titles and 378/2,965 mappings. Its raw output had 34 false-ready cases and 476 incorrect accepted fields; the wrapper suppressed acceptance without establishing useful resolution. Qwen3 1.7B had 62 errors and accepted no eligible cases; successful outputs required review: 1/197 raw exact and 5/197 wrapped exact, with 1,588/3,153 titles and 1,639/3,888 wrapped mappings. Its all-FLAC stratum was 2/154 exact, 1,117/2,376 titles and 1,147/2,965 mappings.

Common-40 metadata matches below are exact after whitespace normalization. Each entry is emitted matches / 39 eligible cases; errors contribute zero even when the reference field is empty. Baseline emitted 20 eligible outputs with 19 errors; hosted models emitted all 39. This diagnostic avoids the frozen scorer's empty-field error default and does not change primary scores.

| Candidate | Artist | Date | Venue | City |
|---|---:|---:|---:|---:|
| Baseline | 15/39 | 9/39 | 2/39 | 2/39 |
| Luna | 36/39 | 38/39 | 28/39 | 26/39 |
| Sol | 36/39 | 33/39 | 31/39 | 36/39 |
| Astra | 37/39 | 35/39 | 31/39 | 35/39 |

[Case-paired common40](../../experiments/parser/results/summaries/common40-paired-exact-bootstrap.json) and [full200 bootstrap intervals](../../experiments/parser/results/summaries/full200-paired-exact-bootstrap.json) resample these fixed cases (10,000 draws, fixed seed); they quantify conditional benchmark variation, not representative-population uncertainty.

| Full-200 wrapped fields | Titles / 3,153 | Mappings / 3,888 | All-FLAC exact / eligible |
|---|---:|---:|---:|
| Baseline | 1584 | 1828 | 8 / 154 |
| Luna | 3040 | 2931 | 49 / 154 |
| Sol | 3118 | 3191 | 72 / 154 |
| Astra | 3111 | 3165 | 70 / 154 |

Successful-output latency and logical single-pass usage cost are descriptive; errors are excluded from latency, not accuracy. Common40 is reused inside full200, so these logical costs must not be summed as ledger debits.

| Hosted cohort | Successful latency p50 / p95 seconds | Logical usage cost USD |
|---|---:|---:|
| Luna common40 | 14.31 / 27.80 | $0.042656 |
| Sol common40 | 13.13 / 27.10 | $0.905566 |
| Astra common40 | 20.34 / 43.73 | $4.597077 |
| Luna full200 | 12.11 / 19.61 | $0.206326 |
| Sol full200 | 12.42 / 20.24 | $4.335051 |
| Astra full200 | 16.83 / 27.77 | $22.015802 |

Hosted candidates use strict-schema Responses API output, low reasoning and Standard tier (`service_tier: default`), with `min(16384,max(8192,2048+220*filecount))` maximum output tokens. The source-note evidence enum is restricted to supplied note names. The frozen [r2 policy](../../experiments/parser/results/candidate-r2-policy.json) records prompt SHA256 `fd455c99057d1e85c7524886802e0a523ad1677ef7127a02d724ae0514ed93b6` and base schema SHA256 `baaa083cc2bca5bf02c8d1a188d900a8765d11a0e449f159802aa1913e96b308`.

| Requested model ID | Standard input / output USD per million tokens |
|---|---:|
| `gpt-6-luna` | $0.10 / $0.50 |
| `gpt-6.1-sol` | $2 / $10 |
| `gpt-6-astra` | $10 / $50 |

These configured rates were verified for this run on 2026-11-03; cache writes and reads are accounted separately. The [provider snapshot](../../experiments/parser/results/provider-access-pricing.json) records [official API pricing](https://developers.openai.com/api/docs/pricing) and [prompt-cache pricing](https://developers.openai.com/api/docs/guides/prompt-caching); future paid runs must reverify rates before setting `prices_verified: true`. Final manifests retain requested and server-reported IDs and each actual output cap.

The full-200 baseline has 269 strict wrong mapping fields across ready and review cases. Its accepted-field source audit is described above; strict comparison differences are not all wrong song identities.

The baseline keeps actual catalog metadata, mappings and readiness; its raw title helper is diagnostic only. Scope errors include unsupported non-FLAC inventories and must not be mistaken for model extraction failures. The earlier 193/200 regression exact score uses an oracle without metadata and is not comparable to the new metadata-inclusive exact measure. Hosted requested and server-reported model IDs identify the API route; they do not establish immutable model versions.

Detailed title/mapping metrics, raw-output metrics, all-FLAC strata, timings, model/runtime hashes, synthetic findings and classifier limitations are in [local-results.md](../../experiments/parser/results/local-results.md). The current local evidence supports keeping these models experimental. All three hosted r2 validation pilots completed 30/30 outputs without errors: raw exact Luna/Sol/Astra 18/20/20, wrapped exact 12/14/14. Each wrapper accepted five cases, four exact and one nonexact; those are validation diagnostics, not held-out safety estimates.

Source-feature strata use frozen source-only flags and the same IDs across candidates. They are overlapping descriptive signals, not exclusive curated format/template families. Complete requested/error/ready/false-ready and raw/wrapped counts are linked in [full200 feature summaries](../../experiments/parser/results/summaries/sol-full200-source-features.json) and the shared [definitions](../../experiments/parser/results/source-feature-strata-definition-full200.json).

| Full200 signal | Eligible | Baseline wrapped exact | Luna wrapped exact | Sol wrapped exact | Astra wrapped exact |
|---|---:|---:|---:|---:|---:|
| disc-set-heading-signal | 102 | 6 | 35 | 51 | 49 |
| duration-signal | 64 | 1 | 20 | 29 | 30 |
| technical-section-signal | 131 | 6 | 51 | 67 | 66 |
| multiple-date-token-signal | 6 | 0 | 1 | 2 | 1 |
| multiple-original-notes | 66 | 3 | 25 | 38 | 37 |
| nested-original-audio-paths | 16 | 0 | 5 | 7 | 7 |
| no-listed-source-feature | 10 | 0 | 3 | 5 | 4 |
| transcoded-source-encoding | 1 | 0 | 1 | 0 | 0 |

## Source-audited failures

The [Luna/Sol audit](../../experiments/parser/results/hosted-common40-source-audit.json), and [Astra audit](../../experiments/parser/results/astra-common40-source-audit.json) distinguish strict representation differences from material issues without changing frozen labels or scores. Luna's 52 strict accepted mapping differences include 27 unsupported positional joins and 25 performer-footnote prefix differences; a mismatch is not automatically a wrong-song swap. The audits are agent source reviews, not independent human certification.

| Case and source | Observed accepted behavior | Interpretation |
|---|---|---|
| [spin1989-04-15.flac16](https://archive.org/details/spin1989-04-15.flac16) | Luna assigned 16 titles from an unnumbered list | Unsupported positional join; file/song identity was not established by identifiers |
| [amt2006-05-16.flac16](https://archive.org/details/amt2006-05-16.flac16) | Luna assigned 11 titles without sufficient identifier evidence | Another unsupported positional join |
| [localh1999-06-24-fm103.flac16](https://archive.org/details/localh1999-06-24-fm103.flac16) | All three hosted candidates marked a mixed-provenance compilation ready | Correct file-title IDs do not resolve shared concert date/venue applicability |
| [dbb1999-06-20.akg.flac16](https://archive.org/details/dbb1999-06-20.akg.flac16) | Source semicolon retained instead of reference comma | Representation difference, not invented metadata |

On the matched 39 eligible packages, the baseline accepted no cases and had no material accepted issues; Luna had three, Sol one and Astra one. Consequently all three fail the proposed automatic-fallback criterion of no increase in material accepted issues on this cohort. All three also accepted [ToyFactoryProject2026-05-19](https://archive.org/details/ToyFactoryProject2026-05-19), whose reference was disputed before predictions; source uncertainty prevents calling that a proven error. The [full-200 baseline audit](../../experiments/parser/results/baseline-accepted-source-audit.json) documents its separate failures.

## Synthetic, classifier and performance limits

Synthetic V1 has a known false conflicting-date label and headers that put every baseline case in review. V2 is a baseline-informed exploratory revision, with 5,000 generated cases split by family (3,014 / 988 / 998); its labels are not independently reviewed. Its baseline scorer-v3 result is 3,017/5,000 exact with 18,171/20,621 correct mappings. The 1,054 incorrect accepted mapping fields include bracket-duration conventions; they are not validated production defects. The fixed synthetic200 baseline had 62/200 exact, 35 ready and zero false-ready cases. Luna completed 200 requests with 199 valid outputs: 102/200 raw exact, 109/200 wrapped exact and 37 wrapped false-ready cases. These remain exploratory generated-label measurements; Sol completed 200/200 valid outputs, with 151/200 raw and wrapped exact and 43 wrapped false-ready. Astra completed its preselected40 with 30/40 raw/wrapped exact and nine false-ready. On the matched synthetic40, wrapped exact baseline/Luna/Sol/Astra was 16/20/30/30, with false-ready 0/9/9/9; full200 and common40 counts must not be ranked across unmatched cohorts. Local Q8 models also ran the same synthetic40: both were 0/40 wrapped exact and accepted no cases; successful outputs required review while errors remained failures. The 0.6B run had no response errors but 0/154 ID-keyed titles and 11/165 wrapped mappings; all 106 emitted title keys were filenames, with 16 expected title strings under wrong keys. This is a key/schema semantics failure, not proof that no title text was recognized. The 1.7B run had 27 duplicate-identifier errors, 24/154 titles and 24/165 wrapped mappings. Full request payloads were not retained for independent reconstruction of that diagnostic; pinned weights/settings were verified. Valid JSON is not semantic validity. The exact [synthetic200](../../experiments/parser/results/frozen-synthetic-test200.json) and [common40](../../experiments/parser/results/frozen-synthetic-common40.json) inputs/labels are committed for reproducibility.

The synthetic classifier's roughly 95% held-out line accuracy measures artificial templates. The initial real classifier uses 117 draft-labeled training cases and positive source-cited lines only: 1,639 train lines and 640 validation lines, with 95.625% validation accuracy. Without background and technical negatives it cannot estimate false positives or justify replacing line detection. The initial 117-case diagnostic is obsolete/non-primary and may include template overlap. Before any final classifier fit/test, a [source-driven post-hoc cohort](../../experiments/parser/results/classifier-source-partition.json) was frozen at 459 train / 144 validation / 200 test cases through uploader/group closure and known-family exclusions, using no candidate predictions or test labels. It reduces identified overlap without guaranteeing all cross-uploader templates are disjoint; the original 800/200/200 extraction benchmark is unchanged. Final training annotation coverage is 797 successful source-only Luna weak drafts from 800 attempted cases; three genuine schema failures are excluded. Validation labels are frozen at 195 eligible / five disputed out of 200, with pilot30 unchanged; together validation/test have 392 eligible and eight disputed labels, none unreviewed.

The final classifier retained 457/459 train cases with 7,184 cited positive lines and 140/144 validation cases with 2,711 lines. Validation accuracy was 98.045% (macro-F1 0.9393). Its untouched original test200 contributed 3,655 cited positive lines from 197 reviewed cases: 98.960% accuracy, macro-F1 0.9663, versus a training-selected title-majority baseline of 84.405%. Fit took 9.71s wall under shared host load (26.69s user, 11.33s system, 372.7 MB max RSS); the fitted model is 3,562,225 bytes. Selection excludes uncited/background lines, multiline or ambiguous citations, and lines cited for conflicting classes. This is classification among uniquely cited single-label positive lines, not general line detection or recording accuracy. Without uncited/background negatives it still cannot measure false positives or justify replacing rules. The [actual public replay proof](../../experiments/parser/results/classifier-public-replay-proof.json) reproduces exact train/validation and test confusion metrics from all 1,200 rehydrated sources and frozen text-free references; serialized joblib bytes differ, so binary determinism is not claimed. Trusted published reference digests supply label semantics, while source/context hashes verify bytes. [Per-class test metrics](../../experiments/parser/results/classifier-test-report.json) and the [inference benchmark](../../experiments/parser/results/classifier-inference-benchmark.json) are public: transform/predict on 3,655 selected test lines took 0.302/0.322/0.314s with identical prediction hashes, excluding model load and source rehydration, under shared host load.

Measurements use an Apple M2 Max (12 CPU cores, 30 GPU cores), 64 GiB memory, macOS 27.2 build 26B123 and Metal 4. CPU builds disable Metal, GPU layers, KV and operation offload; Metal reported 29/29 layers offloaded. The three-call synthetic CPU/Metal comparison generated different token lengths and is descriptive, not a controlled speedup estimate. Three real CPU cases were preselected independently and measured three times each. Qwen3 0.6B and 1.7B had mixed cold/warm wall p50/p95 of 23.62/41.50s and 71.58/89.30s, with maximum sampled RSS 5.73 and 8.39 GB. These nine-call summaries mix first and warm requests with unequal output lengths. Startup is recorded separately and excluded from those wall quantiles; OS caches may already be warm. For the same three cases on Metal, 0.6B and 1.7B wall p50/p95 were 8.72/12.50s and 20.73/27.79s, with max sampled RSS 4.99/6.24 GB. These nine-call-per-model comparisons mix first and prompt-cached requests and unequal outputs, so they are not controlled speedup estimates. See the local report for individual calls. Baseline timings use disposable placeholder audio and include temporary-tree overhead, not real FLAC hashing or a full app scan. Windows local inference performance is untested. Hosted provider CPU time, peak memory and model size are unavailable; they were not measured as equivalents to local resource metrics.

The optional Q4_K_M diagnostic derives a 1,282,439,616-byte model from verified official BF16 source with pinned conversion/runtime dependencies; [provenance](../../experiments/parser/results/q4-provenance.json) and the [setup recipe](../../experiments/parser/README.md#optional-derived-q4-model) retain the source/runtime hashes. On the fixed three real validation cases (nine first/warm calls per backend), CPU wall p50/p95 was 56.45/70.67s, summed server CPU 336.91/422.13s and max sampled RSS 6.69 GB; Metal was 12.54/20.82s wall, 36.67/59.33s CPU and 5.66 GB RSS. Output lengths differ. The official Q8 card does not establish exact BF16 revision/shard linkage, so this is a source/conversion-plus-quantization comparison, not a pure quantization effect. Q4 validation30 completed with seven duplicate-identifier errors, 0/30 wrapped exact, 153/556 titles and 114/611 mappings; successful outputs required review and errors remained failures. Its common40 expansion was selected by a pre-test time forecast leaving a 30-minute margin before the cutoff, without a quality threshold. That run completed 40 requested / 39 eligible cases with nine errors (eight duplicate identifiers, one incomplete): 0/39 raw exact, 1/39 wrapped exact, 166/633 titles and 172/743 wrapped mappings (106/743 raw). It accepted no eligible cases: successful outputs required review, with errors separately retained and zero false-ready or incorrect accepted mappings. On 21 all-FLAC eligible cases it was 0/21 exact, 89/274 titles and 74/343 mappings. The corresponding Q8 common40 was 1/39 exact, 364/633 titles and 249/743 mappings. These results do not support replacing the parser with either local configuration.

## Cost and protocol deviations

The user initially authorized $50, then explicitly raised the total ceiling to $80 if needed. This is one total cap, not an additional $80. The same SQLite ledger records the audited amendment; ordinary ledger opens can only decrease a cap. A cost/time-only [forecast](../../experiments/parser/results/astra-real-full200-budget-forecast.json), reviewed after core paid phases drained, approved up to 160 additional Astra test requests with a $22.98544 padded projection and $12.877325 retained reserve. Its common40 was reused. The final [billing reconciliation](../../experiments/parser/results/budget-accounting-final.json) records $44.703968425 usage-derived gross list-price cost, $0.002017 conservative missing-usage estimates, $0.001015575 rounding/retention excess and $16.848992 retained unknown maxima: total budget footprint $61.555993, zero active reservations and $18.444007 headroom under $80. Dispatch is closed and paid workers are drained. Net invoices and free-credit consumption are unknown; Firecrawl cost was zero.

An earlier credit-exhaustion incident left 342 historical uncertain calls without reliable response status. Their conservative maximum charges remain in the ledger; no refund or zero-charge assumption is made. Missing cache-write accounting was corrected using 1.25× input pricing for 30-minute writes and 2.5× for 12-hour writes ([official cache pricing](https://developers.openai.com/api/docs/guides/prompt-caching)). Refreshing known usage does not debit it twice. New requests reserve the worst-case 2.5× input rate plus maximum output/reasoning cost. First transport/accounting failure trips a shared circuit breaker; durable attempt claims and shared reservations prevent silent duplication. After sustained healthy provider operation, the coordinating agent increased the global HTTP worker ceiling from two to four under the existing task/budget authorization, retaining the same budget, claims and circuit breaker; the external concurrency audit records this operational amendment. Only a separately audited transport retry is permitted; genuine malformed/incomplete outputs remain failures.

One early local prediction ran before the validation gold freeze. It was sealed uninspected, excluded, and replaced by a fresh phase. Scorer 2→3 comparison fixes preceded final test dispatch. Historical synthetic V1 defects and baseline-informed V2 changes remain disclosed above. No prompt or scorer tuning is allowed on held-out test outcomes.

## Reproduction and delivery

Use the [harness README](../../experiments/parser/README.md) for model configuration, guarded execution and local setup. Replay only the committed acquisition and split manifests:

```sh
python3 -m experiments.parser.replay experiments/parser/acquisition-manifest.jsonl experiments/parser/results/real-splits.json /external/parser-replay
python3 -m unittest discover -s experiments/parser/tests -v
go build -tags parser_experiment -o /tmp/track-parser-adapter ./experiments/parser/adapter
go test -tags parser_experiment ./experiments/parser/adapter
```

Replay verifies original note byte hashes and frozen input hashes without downloading audio; a changed source fails rather than silently substituting inventory. Public [test gold](../../experiments/parser/results/gold-real-test200.jsonl), [validation gold](../../experiments/parser/results/gold-real-validation30.jsonl), prediction JSONL and selected-ID/hash manifests retain structured values and evidence hashes/line offsets, with original quotes removed. Safe [model configs](../../experiments/parser/results/configs) record requested IDs, rates and final settings. Hosted server aliases are not immutable weights; local model/runtime hashes are recorded in the local report. The hosted adapter reads the original `/Users/dan/code/track-tagger/.env` without copying or logging it; paid requery commands can supply `--env-file` for an explicit credential file and must supply the built `--identifier-adapter`. Offline replay/aggregation requires no credentials.

The [local verification record](../../experiments/parser/results/verification-checks.json) reports clean Go formatting; passing Go race/vet, tagged adapter/catalog race checks, Node 24 player tests (3/3), actionlint, 86 harness tests with optional dependencies (the stdlib-only run has one intentional dependency skip), 17 acquisition tests and two Hypothesis synthetic tests. Unchanged production package race results were cached; tagged catalog checks ran fresh. Remote checks are tracked on the [feature branch Actions page](https://github.com/baseonmars/track-tagger/actions?query=branch%3Afeat%2Fparser-experiments). This feature branch requires review; it has not been merged or released.

After replay, join dequoted gold and verify the published baseline without paid inference:

```sh
python3 -m experiments.parser.public_reproduce join /external/parser-replay/replayed-cases.jsonl experiments/parser/results/gold-real-test200.jsonl /external/joined200.json
python3 -m experiments.parser.public_reproduce check experiments/parser/results/candidate-baseline-real-full200-r2.jsonl /external/joined200.json
python3 -m experiments.parser.public_reproduce aggregate experiments/parser/results/candidate-baseline-real-full200-r2.jsonl
```

The public gold has no redacted values. Exported prediction semantic checks have zero mismatches: baseline 195/197 eligible rows, Luna 197/197 and Sol 195/197. Two baseline and two Sol prediction rows contain redactions and are excluded from those semantic checks; stored-score aggregation still reproduces all 197. A separate [fresh executable baseline reproduction](../../experiments/parser/results/summaries/fresh-public-baseline-reevaluation-proof.json), using replayed public gold and private fresh outputs, matched all 200 output/score/error-class rows (197 eligible), including 8 exact and 2 false-ready. Replay verified all 1,200 canonical input hashes from cached original notes; it did not retest current remote availability. This is not byte-exact rescoring of redacted prediction values. Common-40 predictions reused in full-200 cohorts retain their original inference latency and logical per-record cost; they do not represent new cache-lookup timing or new spend. The shared ledger counts actual calls once. Dedicated local warm benchmarks bypass the harness JSON cache while using the model server's prompt cache.

Cached hybrid routing experiments use the actual baseline review/error decision, retaining production-ready cases and falling back on the hosted output only for review/errors. Public [hybrid summaries](../../experiments/parser/results/summaries) cover common40 for all three hosted models and full200 for Luna/Sol/Astra. These are simulations from saved predictions: zero additional API calls, logical fallback cost only, and summed saved baseline/fallback latency rather than measured routed deployment latency. They do not establish deployment behavior or automatically fix accepted baseline issues.

| Cached hybrid cohort | Exact / eligible | Ready | False-ready | Logical fallback cost USD |
|---|---:|---:|---:|---:|
| Luna common40 | 10 / 39 | 9 | 3 | $0.042656 |
| Sol common40 | 12 / 39 | 6 | 1 | $0.905566 |
| Astra common40 | 15 / 39 | 7 | 1 | $4.597077 |
| Luna full200 | 69 / 197 | 77 | 24 | $0.196666 |
| Sol full200 | 91 / 197 | 69 | 11 | $4.114809 |
| Astra full200 | 89 / 197 | 68 | 12 | $20.906293 |

To rerun the baseline itself from replayed sources and public gold, build the tagged adapter and use disposable external outputs:

```sh
go build -tags parser_experiment -o /external/track-parser-adapter ./experiments/parser/adapter
python3 - <<'PYCODE'
from experiments.parser.runner import read_cases, run
from experiments.parser.adapters import BaselineAdapter
from experiments.parser.rescore import rescore
run(read_cases('/external/joined200.json'),
    BaselineAdapter('/external/track-parser-adapter'),
    '/external/baseline-fresh.jsonl', 'baseline-replay')
rescore('/external/baseline-fresh.jsonl', '/external/joined200.json',
        '/external/baseline-fresh-v3.jsonl')
PYCODE
```

This fresh evaluation can score all 197 eligible labels because gold values are unredacted; public prediction export may redact individual long output strings. Existing output paths must be new. No audio or credentials are needed.
