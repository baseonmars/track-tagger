# Offline suggestions in the review app

**Keep the model opt-in for reviewers.** City suggestions are promising. The fresh pilots showed too few venues and no artists or dates to establish per-field reliability. A reviewer still needs to decide whether the quote describes this performance. The US date preference is deterministic and defaults on independently. [Side-by-side comparison with the frozen rules, section classifier and adaptation controls](reviewer-app-controls.md)

The frozen encoder package was replayed through `WebBackend.Suggestions` after library validation. The 200-recording corpus had already been used in research. The 13-recording pilot and its predetermined 21-recording extension each had separate source readings sealed before inference. The replay used the same package for every run.

| Metric | Exposed corpus | Separate pilot | Pilot extension |
|---|---:|---:|---:|
| Requested recordings | 200 | 13 | 21 |
| Validated recording views | 189 | 12 | 20 |
| Eligible review views | 161 | 10 | 18 |
| Views with suggestions | 74 | 2 | 7 |
| Jointly correct suggestions | 86/87 (98.9%) | 3/3 | 8/8 |
| Exact field values among shown suggestions | 86/87 | 3/3 | 8/8 |
| Fields repaired / degraded | 86 / 1 | 3 / 0 | 8 / 0 |
| Eligible known wrong fields repaired | 86/327 (26.3%) | 3/20 (15.0%) | 8/25 (32.0%) |
| Eligible views with no suggestions | 87/161 | 8/10 | 11/18 |
| False positive background lines | 0/3,633 | 0/286 | 0/313 |
| Exact four metadata fields, rules → displayed suggestions | 22/200 → 35/200 | 2/13 → 3/13 | 5/21 → 9/21 |
| Exact four on the source-defined review cohort | 12/128 → 19/128 | 0/7 → 0/7 | 5/16 → 9/16 |

The exposed-corpus review cohort gained seven exact matches with zero losses. Its shown precision was 1/1 artist, 17/18 venue and 68/68 city. The venue error replaced a correctly unknown value with a name that conflicted with a different venue in an embedded earlier transfer note. The pilot produced one correct venue and two correct cities. The extension produced one correct venue and seven correct cities, with four review-cohort gains and zero losses. Neither pilot emitted an artist suggestion. None of the three runs emitted a date suggestion. Both numeric date policies produced the same counts. With only one venue proposal in each pilot, the pilot results leave per-role precision uncertain.

The 95% Wilson intervals for joint shown precision are 93.8–99.8%, 43.9–100% and 67.6–100%. The corresponding upper bounds for background false-positive rates are 0.11%, 1.33% and 1.21%. The source-family bootstrap gives a paired review-cohort improvement interval of +1.6 to +9.4 percentage points on the exposed corpus and +6.3 to +50.0 points on the extension. The first pilot had no paired changes. The replay also reports per-role exact value precision, recovery recall over eligible wrong known fields and recall over reviewed safe source-role lines. Exact value precision counts a matching normalized value even if its evidence is unsuitable. Joint precision also requires the correct role, span and scope.

Plans, browser recording views and fixture bytes were equal before and after the request in all 189, 12 and 20 validated cases. Plan comparison sorted review reasons because some producers iterate maps. The exposed replay reproduced all 11 prior fixture exclusions. Nine packages yielded zero recordings, one yielded two, and one had a nonportable path. Three otherwise validated packages had notes outside the supported types or recording directory scope. Each pilot excluded one package with no supported recording. Accuracy denominators retain all requested cases.

Shown precision requires a matching role and value, correct source coordinates, and source scope free of flagged uncertainty, conflict or other-recording evidence. The exact-four overlay describes the effect of taking all shown values for comparison with source references. The app preserves the parsed recording until an explicit review is saved. Venue and city can enter the draft album when its baseline permits recomposition.

## Source examples

The offsets below are zero-based line indices and half-open Unicode code-point intervals in the normalized note. The UI displays line numbers starting from one.

| Source note | Field | Line | Offset | Quoted evidence | Source assessment |
|---|---|---:|---|---|---|
| `gb2026-10-02_Info.txt` | Venue | 2 | `[0, 34)` | Arizona Veterans Memorial Coliseum | Correct |
| `pachy2019-01-05.txt` | City | 2 | `[29, 43)` | Pittsburgh, PA | Correct |
| `fv2008-09-13.bscs-l.txt` | Venue | 2 | `[0, 11)` | Toads Place | Correct |
| `firehose1986-11-10info.txt` | Venue | 1 | `[0, 15)` | CA Laboratories | Conflicting venue |

## Replay

Run from the repository root with the external cases, source-reference seal and previous catalog executable. The script uses FFmpeg to create a short synthetic FLAC and preserves the original audio filenames and extensions. It opens disposable source trees in dry-run mode.

```sh
cache=../track-tagger-experiment-data
bash tools/review-evaluation/replay.sh \
  "$cache/tiny-span-blind-final-v3/blind-inputs.jsonl" \
  "$cache/tiny-span/blind/canonical-reference/sealed/seal.json" \
  "$cache/tiny-span/catalog" \
  "$cache/review-suggestions-app-replay-repeat"

bash tools/review-evaluation/replay.sh \
  "$cache/product-fresh/cached-screen/blind-inputs.jsonl" \
  "$cache/product-fresh/reference-13/seal.json" \
  "$cache/tiny-span/catalog" \
  "$cache/review-suggestions-fresh13-replay-repeat"

bash tools/review-evaluation/replay.sh \
  "$cache/product-fresh/extension-stage-21/inputs.jsonl" \
  "$cache/product-fresh/extension-stage-21/sealed/seal.json" \
  "$cache/tiny-span/catalog" \
  "$cache/review-suggestions-fresh21-replay-repeat"
```

Each external output includes the score report, input and model checksums, app runner and per-case traces. Those traces may contain note quotations and original filenames.

The source seals used for the exposed corpus, pilot and extension had SHA-256 digests
`7cd9e9ffae4b8733515e81c13ebbe182c953003ceb401397b40183228a3ed669`,
`4120b901983f8aa3c2c5e6871baf4472182349d22533e57c1acf17d7931b4473`, and
`4616bb0522f2069047261eff9bb9cb34f851197b96a6491f7accc2057baba256`, respectively.
The model package digest was
`222c8047fa59c2ba98649e127f685405f124cf363d37b81274b96d4b50f36bd9`.

## Selection, resources and release decision

Two independent Codex source readers examined each pilot recording before inference. Reviewers worked from public note text and relative filenames, marked field evidence, unknowns, conflicting roles, track joins and review membership, and reconciled their findings. The first pilot covered all 440 nonblank lines. Free acquisition returned 341 of 350 more packages. Grouping against known recording, performance, uploader and template connections left 84 separate components from different artist aliases. The preassigned first 21 contributed a further 517 reviewed nonblank lines. The remaining 63 were reserved before product inference. Public pretraining exposure and undocumented connections cannot be excluded. These are agent adjudications. The app interaction test used a synthetic recording and Chrome. Human reviewer acceptance and time saved remain unmeasured.

The native pack combines full adaptation and rank-4 LoRA of Google BERT Tiny. Both encoders must agree on the exact span before the catalog can show it. The earlier experiment compared untrained, head-only, full-fit, and LoRA arms. A 135M-parameter generative candidate already exceeded the size budget in quantized weights alone. Training and comparison details are available in the [pinned research report](https://github.com/baseonmars/track-tagger/blob/761049110eded56866538a6f7384febfcee797b5/experiments/tiny-span/REPORT.md) and [model selection record](https://github.com/baseonmars/track-tagger/blob/761049110eded56866538a6f7384febfcee797b5/experiments/tiny-span/MODEL_CHOICE.md).

On macOS ARM64, the stripped CLI and ad-hoc-signed desktop app totaled 41,904,478 bytes, including the full model, vocabulary, policy, license, notice, runtime and native inference code. Building the same two executables from baseline `f8bd76b` totaled 34,072,888 bytes. The installed increment of 7,831,590 bytes meets the 10 MB target. At one thread on an Apple M2 Max, model load took 79 ms and warm prediction including tokenization and exact-span selection took 8.83 ms p95 CPU on the 200 previously studied recordings. With the default US date setting, complete backend request p95 wall times including input and scope checks were 8.90 ms on the exposed cohort, 25.02 ms on the 13-recording pilot and 21.00 ms on its extension. Request-level CPU time was not separately instrumented; these wall times also include filesystem access. New paid annotation, rented compute and hosted inference cost $0. These runs reused the frozen fits. Electricity and Codex usage were not separately priced. [Native package, revisions and benchmark replay](../../internal/notesmodel/README.md)

The pilots provide 34 fresh recording references. Two failed app scans remain in the denominator. The requirement for 180 fresh adjudications, support on 20 recordings for each enabled field and strict scan completeness remains open. An opt-in reviewer trial can now measure whether people accept the right changes, catch source-scope errors and finish review sooner.
