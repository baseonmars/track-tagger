# Parser experiment harness

This directory runs source-note extraction experiments outside the production parser. Raw Archive notes, full predictions/provider responses, credentials, SQLite ledger and response caches belong outside Git; sanitized public prediction exports retain structured metrics and dequoted values. The hosted adapter reads `OPENAI_API_KEY` from `/Users/dan/code/track-tagger/.env`; it does not execute dotenv contents or copy the file.

Run tests from the repository root:

```sh
python3 -m unittest discover -s experiments/parser/tests -v
```

Acquire packages with `acquire.py`, normalize and merge related packages with `normalize_packages` and `group_packages`, then freeze the resulting JSONL before annotation or predictions:

```sh
python3 -m experiments.parser.runner freeze /external/cases.jsonl /external/frozen.json
```

Groups remain within one split. Actual package counts can differ from 800/200/200 to preserve grouping. The immutable freeze contains diversity-selected fixed validation pilot IDs (up to 30) and fixed Astra test subset IDs (up to 40), with recorded artist/group/decade/feature strata. Freeze records source content hash. Package completeness, artist/decade coverage and near-duplicate grouping still require inspection of the acquisition report before accepting the freeze.

Create an external model configuration with exact accessible model ID, `input_per_million`, `output_per_million`, and `prices_verified: true`. Real runs share the output-token policy `min(16384,max(8192,2048+220*filecount))`; each response records its exact cap, reasoning setting, service tier and resolved model. Initialize the persisted session through `orchestrate.py` before using the guarded runner CLI. Official Standard rates verified 2026-11-03: gpt-6-luna 0.10/0.50, gpt-6.1-sol 2/10, gpt-6-astra 10/50 USD input/output per million tokens. These are the configured rates for the November 3–4 run, not permanent prices. Before a future paid rerun, verify official API input/output, cache-read and cache-write rates, update the model configuration and price-verification record, then set `prices_verified: true`; the gross-rate ledger bound depends on correct rates. `orchestrate.py` model defaults reflect this run. A free authenticated models listing confirmed all three requested IDs. Use `/external/experiment-data/run` as the one shared state directory across every model and annotation phase. Initialize a fresh experiment once, without API calls; never reset an expired existing session in place or renew its clock mid-run:

```sh
python3 -c 'from experiments.parser.orchestrate import session; import sys; session(sys.argv[1])' /external/experiment-data/run
```

Then use that same persisted clock and ledger:

```sh
python3 -m experiments.parser.runner run /external/frozen.json /external/luna.json /external/experiment-data/run --phase candidate-luna-v1 --split validation
python3 -m experiments.parser.runner report /external/experiment-data/run/candidate-luna-v1.jsonl
```

The Responses API uses Standard service tier and low reasoning with strict schema. Maximum input charge reserves the request-byte bound plus 32,768 tokens for schema overhead at the worst-case 2.5× input cache-write rate; output reservation includes all maximum output/reasoning tokens. The short-context ceiling is 128,000 tokens. Every HTTP attempt has a separate reservation; no hidden automatic retries occur. Unknown outcomes retain the entire reservation. Reliable usage settles actual cost even if output is incomplete. Proven usage overruns or unexpected service tiers halt subsequent calls. New ledgers default to a $50 total cap; ordinary opens can only decrease it. This session received an explicit, audited amendment from $50 to an $80 total ceiling (not $80 additional), recorded in the same ledger. Historical uncertain reservations remain charged. Existing in-flight reservations remain charged after a crash. Caches include phase, exact model, prompt, schema and request settings; candidate inputs contain only note names/text and filenames, never oracles.

`annotations.draft` creates independent source-only drafts in a separate cache phase. Drafts never automatically become ground truth. `annotations.adjudicate` requires a separately supplied decision, named reviewer, source-content match, and exact source quotes for populated fields. Runner accuracy includes `adjudicated`, `agent-reviewed` full-source model reviews, and explicit `synthetic` labels. `source_review.py` is the promotion gate for routine hosted draft reviews; the final independently authored/reconciled test route is documented in `SOURCE_REVIEW.md`. The routine gate checks complete file inventory, mapping coherence, source quotes and date precision, and retains a full external audit. Machine source review is not human certification. Keep machine agreement, disputed labels, and unreviewed coverage separately visible. A quote check verifies source presence, not semantic support: a reviewer must still assess whether the quote establishes the claimed value.

Reports retain raw model extraction and deterministic filename reconciliation separately. Reconciliation uses conservative supported identifiers, rejects date prefixes, duplicate IDs and missing titles, and requires review for unmatched titles/files. Incorrect accepted mappings count incorrect mappings in ready cases; `wrong_mappings` includes review cases. False-ready counts readiness against an independently reviewed ambiguous oracle. Raw extraction retains malformed date and title identifiers for scoring; reconciliation clears malformed values and requires review. Semantic exact compares known metadata, titles, readiness and mappings, normalizing every empty-title mapping to zero disc/number on both sides. It excludes free-form unresolved explanations; canonical unresolved-ID agreement is a separate diagnostic because the production baseline does not expose it. Final scorer version 3 additionally trims and collapses whitespace in metadata/title values only, preserving case, punctuation and segue markers. It was frozen before final test dispatch in `results/final-scorer-policy.json`; raw predictions and frozen oracles remain unchanged. Legacy regression oracles omit metadata and therefore do not score metadata accuracy. Evidence coverage reports missing citations and nonexistent quotes separately from accuracy. Generative output with missing or invalid quote citations requires review; matching a quote is a syntax check and does not prove a factual claim. Missing artist/date/venue, empty file inventory, unresolved evidence, and non-FLAC inputs also require review. Production baseline output retains the actual catalog safeguards and readiness unchanged.

Runs checkpoint one JSON object per case and resume completed IDs. Use a unique phase for every revised prompt; allow at most two pilot revisions and freeze the chosen prompt before test predictions. `orchestrate.py` persists a shared seven-hour inference deadline and eight-hour hard session deadline across invocations. Per-model stage spending ceilings persist atomically in the same ledger; ordinary opens only decrease them. An explicit audited `allocate_stage` reallocation can increase a stage within the unchanged shared total cap, as occurred during this run. Failed requests are recorded by exception type without persisting provider error text. Run manifests fingerprint selected inputs, prompts, model configuration, Python code and identifier executable bytes. Reports and exports must omit original source text and credentials.

## Synthetic and local experiments

The frozen V1 fixture is `synthetic-v1.jsonl`, accompanied by `synthetic_v1.py` and the augmented `synthetic-v1-manifest.json`. Regeneration after the generator was renamed has not been verified byte-for-byte; use the frozen file and its hash as the authoritative input. Its labels are generated, not independently reviewed. One known label defect is `synthetic-413695061`: its two supposed conflicting dates are identical. V1's separate venue/city lines cause the scanner to mark every case for review, so its false-ready rate is uninformative. `synthetic-v2.jsonl` is the V2 fixture generated by `synthetic.py`, a baseline-informed exploratory revision with more varied metadata headers; V2 is not an independent confirmation set. Both versions keep template families in separate train, validation and test splits. The line classifier reports classification metrics, not recording or tagging accuracy; its template-based perfect scores do not establish real-world generalization.

The public catalog scanner adapter and raw title/identifier helpers exist only under the `parser_experiment` build tag. It creates disposable placeholder audio and never mutates recordings:

```sh
go build -tags parser_experiment -o /tmp/track-parser-adapter ./experiments/parser/adapter
go test -tags parser_experiment ./experiments/parser/adapter
python -m unittest experiments.parser.test_synthetic
```

For macOS local measurements, install the pinned Python packages into an isolated environment from `local-requirements.txt`; Python 3.9.6 was used for these results. `local_setup.py` explicitly downloads the pinned llama.cpp commit and the official Qwen Q8 weights to the chosen external directory and verifies model SHA256. It builds CPU and Metal backends separately and does not update an existing checkout. Run it with `--root /external/experiment-data --cmake /path/to/cmake`; use a new root if an existing checkout or CMake build directory points elsewhere. Models are from [Qwen3-0.6B-GGUF](https://huggingface.co/Qwen/Qwen3-0.6B-GGUF) and [Qwen3-1.7B-GGUF](https://huggingface.co/Qwen/Qwen3-1.7B-GGUF), whose model cards identify Apache-2.0. These are local research builds, not distributable runtime artifacts. CPU mode disables layer, KV and operation offload; Metal mode requests GPU layers. Both use a 32,768 token context with context shifting disabled, and the adapter rejects requests whose conservative byte bound plus output budget exceeds context.

After setup, benchmark a frozen case. The JSON result contains hashes rather than source text; verbose server logs may contain source text and must remain external:

```sh
python -m experiments.parser.benchmark_local --root /external/experiment-data --cases /external/cases.jsonl --id CASE_ID --backend cpu --model 1.7b --output /external/cpu-benchmark.json
```

Use `--backend metal` for a separate GPU run. The script starts and stops its own loopback server, records first-call and two prompt-cached calls, startup time, token use, output hashes, and an external server resource log. The first call is cold only with respect to that server process; OS disk caches may already be warm. Outputs and token counts can differ across backends and even across calls, so compare latency alongside output size and accuracy. Local run identity includes model hash, pinned runtime commit, backend and context; cache hits are not timing samples. Report errors and exclusions in the requested-case denominator. Final scorer version 3 applies the comparison rules above without changing frozen source or predictions; historic scores and earlier wrapper gates remain separately identified.

For the final real-data pilot, both local and hosted candidates use `min(16384, max(8192, 2048 + 220 * file_count))` output tokens. The local adapter selects this with `output_token_policy: adaptive-v1` in its configuration and records each actual limit. Earlier local benchmarks used a fixed 8,192-token limit.

`freeze.py` verifies manifest-selected source hashes, preserves recording/template groups, and exports a text-free public split manifest. Freeze reviewed validation/test labels only after source review and independent source adjudication. The full 200-case test has independently sealed and reconciled source labels: 197 eligible and three disputed; the common-40 subset is unchanged (39 eligible, one disputed). The label freeze rejects changed group/split/features and unknown IDs. Public exports in `exports.py` retain structured metadata and hashes while replacing evidence quotes with hashes/line references; multiline or unusually long model strings are hash-redacted with an audit flag. Full notes, full prediction rows, original evidence quotes and source-review audits remain external.

All real validation/test candidate runs, including the baseline evaluation runner, require an immutable reviewed label artifact before dispatch. Annotation phases remain separate. A frozen validation pilot subset can precede the full final label freeze; full real test predictions wait for the independent common-40 source audit and final label freeze.

Replay the committed acquisition/split manifests without discovery or audio downloads:

```sh
python3 -m experiments.parser.replay experiments/parser/acquisition-manifest.jsonl experiments/parser/results/real-splits.json /external/parser-replay
```

Replay downloads only the listed original note URLs, verifies byte hashes/sizes and canonical per-case input hashes, and retains original filename inventory and frozen IDs/groups/splits. A missing or changed source produces a nonzero exit and an external replay report; do not silently substitute a new Archive metadata inventory. Public dequoted gold can be joined by case ID for offline score reproduction. Original note bytes remain in the external replay root.

The central [overnight report](../../docs/research/overnight-parser-experiments.md) records final coverage, protocol deviations and delivery status. Local measurements and classifier diagnostics are maintained in [local-results.md](results/local-results.md). `invented_info` is a metadata mismatch/invention proxy, not verified hallucination. Failed outputs remain in requested eligible denominators; inspect error coverage before interpreting empty-field accuracy. Report the eligible all-FLAC stratum alongside the complete cohort. Zero observed unsafe accepts is not proof of safety, and placing every case in review is not successful parsing.

The original real split is not fully template-held-out: the [integrity audit](results/split-template-integrity-audit.json) found one distinctive related-template family crossing train/validation/test despite the Jaccard threshold. Keep the frozen extraction benchmark unchanged and do not claim unseen-template generalization. The final classifier uses a separate source-driven post-hoc cohort, frozen before fit/test, with uploader/group closure and known-family exclusions; see the central report for counts and residual limitations.

## Reproduce the final classifier metrics

The classifier uses trusted, text-free reference labels; source/context hashes verify replayed bytes, not citation semantics. Its positive-line subset excludes uncited/background, multiline/ambiguous and conflicting-class lines. After manifest replay:

```sh
python3 -m experiments.parser.classifier_refs train-replay --frozen /external/parser-replay/replayed-cases.jsonl --partition experiments/parser/results/classifier-source-partition.json --refs experiments/parser/results/classifier-train-validation-refs.jsonl --refs-manifest experiments/parser/results/classifier-train-validation-refs.manifest.json --source-manifest experiments/parser/results/real-splits.json --output /external/classifier-fit
python3 -m experiments.parser.classifier_refs evaluate-replay --frozen /external/parser-replay/replayed-cases.jsonl --partition experiments/parser/results/classifier-source-partition.json --refs experiments/parser/results/classifier-test-refs.jsonl --refs-manifest experiments/parser/results/classifier-test-refs.manifest.json --source-manifest experiments/parser/results/real-splits.json --model /external/classifier-fit/classifier.joblib --training-freeze /external/classifier-fit/training-freeze.json --output /external/classifier-test-report.json
```

The [actual replay proof](results/classifier-public-replay-proof.json) matched confusion matrices and accuracy; serialized model bytes differ. See [test metrics](results/classifier-test-report.json) for class support and selection coverage. These results cannot estimate false positives or establish general raw-line detection.

## Optional derived Q4 model

Q4 conversion uses a separate Python 3.12 environment; Q8 measurements used Python 3.9.6. First create an external root with the pinned `local_setup.py` runtime and initialize its shared session once using the no-API command above. The existing `run/session.json` inference deadline must remain active; use a fresh experiment root rather than reset an expired session. Keep at least 12 GiB free. Install the conversion environment and derive the model:

```sh
uv venv --python 3.12 /external/q4-venv
uv pip install --python /external/q4-venv/bin/python -r experiments/parser/q4-conversion-requirements.txt
python3 -m experiments.parser.q4_setup --root /external/experiment-data --python /external/q4-venv/bin/python --cmake /path/to/cmake
```

The setup verifies pinned official Qwen3-1.7B BF16 source shards, converts with the pinned llama.cpp runtime, and derives Q4_K_M directly; it does not requantize Q8. Existing derived outputs are refused. Weights stay external and provenance is recorded under `run/q4-reproduced-provenance.json`; the experiment snapshot is [q4-provenance.json](results/q4-provenance.json). Use `--model 1.7b-q4` with `benchmark_local` or `pilot_local`. The official Q8 model card names the same base model but does not link exact BF16 source revisions/shards, so comparisons measure source/conversion plus quantization differences, not an isolated causal quantization effect. GGUF `base_model` ancestry referring to the Base model is expected lineage metadata.
