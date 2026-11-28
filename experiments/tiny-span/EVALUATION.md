# Retrospective comparison replay

Use a checkout at application commit `f8bd76b931fa828cfb9156a2d9a8f29a3aa3cb14` and the
separate #38 study checkout at `1848decbea38a428a7e95e62c428cf7ffb1e6a73`.
Use the previous parser-context checkout for the frozen context candidate, Wilson
intervals and its validation freeze. The evaluator verifies the referenced model,
scorer, date validator and context code hashes against the freeze. The #38 replay
instructions describe rehydrating their external source and line references.

From the tiny-span repository root, set the external cache and an empty output
directory. Install the pilot inference dependencies when using a neural package.
Omit the package option to run the baselines alone. The cache paths below follow
the prior parser-context replay layout.

```sh
cache=../track-tagger-experiment-data
context=../track-tagger-parser-context
study=../track-tagger-parser-study-v2
out=$(mktemp -d)
go build -trimpath -tags parser_experiment -o "$out/catalog" ./experiments/tiny-span/catalog

for split in validation test; do
  python3 experiments/tiny-span/evaluate.py "$split" \
    --cases "$cache/v2/replayed-cases.jsonl" \
    --gold "$cache/v2/$split-gold/recording-gold.jsonl" \
    --lines "$cache/context/run/$split-lines.jsonl" \
    --catalog "$out/catalog" \
    --scorer "$cache/v2/noterole-features" \
    --model "$cache/v2/classifier-model/model.json" \
    --date-catalog "$cache/v2/three-arm-build/catalog-fixed" \
    --study "$study" --context "$context" \
    --output "$out/$split.json" \
    --audit "$out/$split-private.json" \
    --package "head_only=$cache/tiny-span/run/head" \
    --package "full_adapted=$cache/tiny-span/run/full" \
    --package "lora_adapted=$cache/tiny-span/run/lora"
done
```

The evaluator binds source and reference file hashes and checks every normalized
line and recording source hash. The private audit contains original text and
source identifiers. Keep it outside Git. A `fresh` run requires a separately
reviewed seal with `independent_source_review_complete: true` and
`input_sha256` values keyed by `cases`, `gold` and `lines`. The operator is
responsible for establishing independence and group-family separation before
issuing that seal and binding the candidate package before inference.

Each arm uses the same current catalog result. The scan runs twice over
independent disposable fixture directories. The comparison includes metadata,
status, readiness, review reasons, filename joins, and mapped title strings.
Failures are listed separately and fail strict parity even when identical on
both runs. The evaluator runs the scanner in isolation and does not call the
application planner or writer. Its parity result covers the catalog output.

The source-defined review cohort includes failed scans in its exact four-field
denominator. Inference failures contribute no proposals and remain in coverage
and recall denominators. A valid suggestion must use exact codepoint offsets
into the cited line, be backed by the same line role, apply to a review-needed
scan, and change a field. Date values use the frozen validator. Any suggested
wrong value remains in counterfactual scoring, including values that fail
semantic adjudication. The rule-only arm supplies the parser's metadata.
It emits no line-role predictions or separate proposals.

Proposal audits check applicability, line role, normalized recording value, and
explicitly annotated offsets where present. Some reviewed lines have only a
role label, so their exact annotated boundaries are counted separately from
verified literal source slices. The Wilson intervals are descriptive.
The exact-four comparisons use a 10,000-draw paired source-family bootstrap.

## Results

These recordings were inspected in earlier parser research. Validation selected
each neural package's thresholds. The 200-recording regression split measures
behavior after model freeze on previously studied sources.

| Arm | Validation exact four / 24 | Regression exact four / 96 | Regression correct fields degraded | Regression proposals |
|---|---:|---:|---:|---:|
| Current rules | 3 | 4 | 0 | 0 |
| Deterministic span control | 3 | 5 | 0 | 10 |
| Frozen section-aware classifier | 8 | 16 | 3 | 148 |
| Frozen encoder with trained head | 6 | 9 | 6 | 104 |
| Full encoder adaptation | 6 | 11 | 1 | 111 |
| LoRA encoder adaptation | 9 | 14 | 5 | 168 |

LoRA gained 10.42 percentage points over rules on the regression review cohort
(paired family bootstrap 95% interval 4.17 to 17.71 points), including 11 newly
exact and one formerly exact recording. Compared with the frozen section-aware
classifier, LoRA changed the same outcome by −2.08 points (95% interval −8.33
to +4.17). The point estimate favors the section-aware classifier. LoRA and
full adaptation raised precision over the frozen-encoder head, but extraction
errors remained. The regression [metrics](results/regression.json) and
[validation metrics](results/validation.json) preserve all counts and intervals.

| Regression arm | Artist TP / FP / FN | Date TP / FP / FN | Venue TP / FP / FN | City TP / FP / FN | Background FP / 5,037 |
|---|---|---|---|---|---:|
| Deterministic span control | 2 / 0 / 722 | 137 / 0 / 91 | 2 / 0 / 220 | 0 / 0 / 211 | 0 |
| Frozen section-aware classifier | 141 / 8 / 583 | 156 / 0 / 72 | 74 / 4 / 148 | 112 / 1 / 99 | 0 |
| Frozen encoder with trained head | 50 / 8 / 674 | 124 / 4 / 104 | 47 / 5 / 175 | 70 / 0 / 141 | 5 |
| Full encoder adaptation | 114 / 2 / 610 | 147 / 1 / 81 | 43 / 0 / 179 | 95 / 0 / 116 | 2 |
| LoRA encoder adaptation | 108 / 4 / 616 | 141 / 2 / 87 | 85 / 1 / 137 | 120 / 0 / 91 | 3 |

On the regression set, LoRA's precision point estimates were 96.43% artist,
98.60% date, 98.84% venue and 100% city, over 111, 142, 86 and 120 emitting
recordings respectively. LoRA made 168 distinct field proposals. Of these,
139 agreed with the reviewed role and recording value, including 23 with
exact independently annotated boundaries and 116 from lines without separate
annotated boundaries. The remaining 29 had a wrong role (3) or value (26).
Full adaptation had 102/111 proposals matching role and value (14 with
annotated boundaries) while the frozen encoder with a trained head had 78/104
(16 with annotated boundaries).

Validation included 50 reviewed recording references and 2,440 reviewed
nonblank lines of 2,440. The regression set retained 200 requests, with 199
eligible recording references and 8,692 reviewed nonblank lines of 8,827
(98.47%). Validation had 40 equal successful scan pairs and 10 shared failures
(nine yielded zero recordings, one yielded three). Regression had 176 equal
successful scan pairs and 24 shared failures (21 yielded zero recordings and
three yielded two). Strict parity failed for both sets. Both runs used
disposable filenames and note fixtures. All proposal arms had zero inference
failures. The current scan accepted two regression cases requiring source
review, and proposals had no effect on that underlying output.

### Source-checked regression examples

The following evidence is keyed by the normalized source and filename digest.
Line and character indexes start at zero, and end offsets are exclusive.

| Arm and observation | Source SHA-256 | Line and offsets |
|---|---|---|
| LoRA selected a concert venue for artist. The notes state the artist in a separate header line, and the proposal would overwrite the correct current artist. | `a535db9e78364bb6622c80218f5bc20d8aee26de0753324e7020f9a562075e2a` | 2, 0–21 |
| LoRA clipped a parenthesized country from a correct city field. This was the review cohort case that lost exact four-field agreement. | `1b08dc98ac3aef61bde775766b1e44f6480acb81a76ce853722387adfb6b959a` | 2, 0–14 |
| LoRA and full adaptation split a hyphenated city, extracting only the second part plus the state. | `8708d9b7e6354c3bb421119956933e3ba90115fb558688d7c845371049eb8772` | 2, 47–56 |
| LoRA omitted a numeral that is part of the artist name, overwriting a correct current field. | `a2e759965ce51253d5a17f7e1414182729a07f017a7655058820770aea29ed55` | 0, 0–13 |
| LoRA isolated the city from a shared venue/city line and matched the reviewed boundaries and normalized recording value. | `c89f96ce7cb82f8eb6b70f4eb8afc45f2fd38346ee6caa68c7364ea8718dabf8` | 1, 25–39 |
| The frozen section-aware classifier read a venue line as artist even though the artist appeared immediately above it. | `d918af145a98fbb22b789e6bec3dd196da2ed7d557f8ec5ba3f4c13b4acdefff` | 1, 0–22 |
| The head-only encoder used a generated-file timestamp as concert date. | `507bdd8c80603d8dfcaaba7ec15ab234d6d274f11961d901b5fb2b35143da3c4` | 1, 15–30 |

An exact literal slice can still omit part of a name, pick the wrong field or
lose information expected from multiple lines. The line-role precision and
the exact-four metadata measures capture different failure modes.

## Fresh reference agreement before adjudication

Two agents independently reviewed the original notes and filenames for 49
recordings in the preassigned cross-review partition of the final-v3 source
seal. Before adjudication they disagreed on eight metadata values across the
four fields, four review decisions, and five sets of title/file joins. The
readings differed on role labels for 84/1,700 lines. Nine of those differences
involved a metadata role. The readers differed on 33 uncertainty flags and
14 conflict flags. [Dequoted agreement counts](results/blind-cross-review-audit.json)

The material decisions included whether to retain an explicitly parenthesized
country, whether a festival label identifies a physical venue, whether a named
event identifies an artist, and how to retain unknown day-level date evidence.
One source reused three explicit identifiers across soundcheck and the live
set. The three conflicting joins stayed unresolved, while unique IDs remained
available. Another source had unnumbered notes but distinctive title strings in
eight filenames, supporting those eight joins while leaving the repeated title
unresolved. This was source-only review. Candidate inference and aggregate
candidate scoring were outside the agreement audit.

Run the agreement audit with private references and the sealed input JSONL.

```sh
python3 experiments/tiny-span/review_agreement.py \
  "$cache/tiny-span-blind-final-v3/blind-inputs.jsonl" \
  "$cache/tiny-span/blind/reviews-root.jsonl" \
  "$cache/tiny-span/blind/cross-eval.jsonl" \
  "$cache/tiny-span/blind/cross-adjudicated-eval.jsonl"
```
