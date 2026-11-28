# Tiny BERT span pilot

The candidate learns to point to spans in the first ten lines of each note's first contiguous header. It uses the current line and up to ten WordPieces from each adjacent line. The same head predicts start and end positions for concert artist, date, venue and city independently, including a null position. The pipeline discards overlong focus lines (more than 40 WordPieces), excludes candidate spans longer than 24 WordPieces, and screens headings, filenames, historical mentions and uncertainty markers. It accepts only literal source slices. It abstains on a recording-level field when eligible lines propose different values. The evaluator applies the current recording's review state and existing field values and passes date proposals through the study's Go validator. [Evaluator contract](EVALUATION.md).

The model is Google BERT Tiny under Apache 2.0. All arms use the same pretrained 4,370,440-parameter encoder and boundary architecture, including the span head. A fixed random head provides a no-fit control. The head-only arm optimizes 1,032 parameters while the pretrained encoder stays frozen. Full fitting optimizes all 4,370,440. Rank-4 LoRA fits 5,128 parameters including the head and merges query/value adapters into the encoder before export. [Pinned BERT model card and original weight files](https://huggingface.co/google/bert_uncased_L-2_H-128_A-2/tree/30b0a37ccaaa32f332884b96992754e246e48c5f).

## Fit and selection

Five CPU epochs used 857 weak source lines from the frozen v2 train pool (250 recordings across 172 groups) and 1,100 generated lines from 480 synthetic recordings. A weak source span uses a unique exact annotated quote or the trimmed text from a single-role line after removing an explicit field label. Date-only weak spans also have to parse as dates. The train labels provided 234 artist, 184 date, 197 venue and 205 city aligned positives. Disputed roles were omitted. Lines labeled mixed scope, uncertain or conflicting yielded null targets. The separate stress set uses fictional values and layouts reserved for stress, including composites, reversed orders, technical decoys and prior shows. Its 240 cases never contribute gradient updates or threshold selection.

On the 50 grouped validation recordings, each cutoff had to have at least five emitting recordings and 95% exact source-supported precision for the field at no more than 1% background false positives. Exact here means the reviewed role and recording value match and, where annotated, the same source offsets match. Recall denominators include eligible role lines supported by a known recording value. The grid is a validation operating-point choice. The independent evaluation reports emissions and proposal evidence separately.

| Validation exact emissions | Artist | Date | Venue | City | Background FP |
|---|---:|---:|---:|---:|---:|
| Deterministic explicit-label/date control | 0/0 | 31/31 | 0/0 | 0/0 | 0/1,361 |
| Head-only | 10/10 | 24/25 | 11/11 | 22/23 | 1/1,361 |
| Full fit | 29/30 | 34/34 | 12/12 | 27/27 | 0/1,361 |
| LoRA | 23/24 | 33/34 | 23/24 | 40/42 | 0/1,361 |

The frozen LoRA primary emits the most exact validation spans under these criteria (119 versus 102 and 67), with thresholds 5, 5, 6 and 6 for artist, date, venue and city. The untrained random head enabled no roles after calibration. At zero margin it was exact on 10/172 artist, 0/0 date, 0/65 venue and 1/146 city spans, with 9/1,361 background false positives.

With frozen thresholds on 240 stress notes, LoRA returned 33/33 exact artist, 136/136 date, 67/76 venue and 30/43 city spans against 192 references per role. Full fitting yielded 33/33, 165/165, 15/15 and 56/56. The overlap and decoy cases exposed a loss of recall, and LoRA city precision dropped to 69.8% in these reserved synthetic layouts. These counts describe fabricated records and do not measure live-note precision. [Pilot results](results/pilot-results.json). [Random head results](results/random-head-results.json).

## CPU and size

The ONNX export stores int8 word embeddings with per-row float32 scales and leaves the other layers in float32. It dequantizes only the rows used at inference. The frozen LoRA model, vocabulary and policy total 6,161,820 bytes, compared with 17,527,968 bytes for the FP32 graph alone. LoRA and full fit returned identical recording-level proposal spans across both representations on 100 train recordings. The head-only arm differed on four. The full Python dependency closure occupies 205,300,439 bytes on this macOS installation, excluding the interpreter. The research date validator executable adds 5,151,698 bytes. The production scanner already contains date parsing. A standalone deployment of this research stack exceeds 10 MB.

On the Apple M2 Max with one ONNX CPU thread, the 300 warm LoRA runs over 100 training groups reached p95 6.725 ms CPU and 8.061 ms wall for the predictor alone. A separate end-to-end run using the evaluator's unchanged review overlay and actual Go date subprocess reached p95 6.477 ms added CPU (including subprocess CPU) and 6.856 ms wall without date caching. With scoring-path caching these were 5.566 ms and 5.846 ms. The production scan is a common input to both arms and runs outside the timed portion. Fresh Python process startup plus load/exit took 665.6–677.3 ms with the operating system's filesystem cache already warm. The timed CPU refit and exact output replay took 64.91 s wall, 57.12 s user and 3.86 s system. The encoder pilot incurred zero paid calls and downloaded only model/dependency files. [Predictor runtime](results/lora-runtime.json). [Scored runtime](results/lora-scored-runtime.json).

## Replay

Use Python 3.12 and the [pinned pilot dependencies](requirements-pilot.txt). The original model files live in external `$DATA/tiny-span/bert`. The optional refit verifies the pinned file hashes before loading. The study cache supplies `$DATA/v2/replayed-cases.jsonl` (SHA-256 `a39f2dbcbba2c3b3b25c5e544ec8cfa275d66abb4fe12ad4881ee7be5c295e33`) and the train and validation references. The fitted packages live in external `$DATA/tiny-span/run`. The replay hashes predictions including offsets and rounded scores for each frozen package on 100 grouped train recordings. A local deterministic refit reproduced the report and exact prediction digests byte for byte on this host.

```sh
bash experiments/tiny-span/replay-pilot.sh
FIT=1 bash experiments/tiny-span/replay-pilot.sh
```

Replay does not require model downloads or network inference once the cache and dependencies are present. Set `DATA` and `PYTHON` to override the sibling experiment cache and its Python virtual environment. Set `--random-head` on the fit script to rerun the negative control in a new output directory.
