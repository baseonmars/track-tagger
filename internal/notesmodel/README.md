# Native notes agreement model

The optional local pack contains the same quantized word embeddings, float32 encoder weights and span-head thresholds used in the full-adaptation and LoRA research arms. Both encoders must emit the same role and exact Unicode source offsets for an item to reach catalog validation. The catalog checks eligibility and date semantics. Request cancellation stops scoring between source windows. The model caps recordings at 16 notes and 40 candidate windows, and ends a note’s opening section at any source line over 16 KiB. The model neither normalizes dates nor writes metadata.

Use `assets/bert-agreement.bin.gz` from a checkout. Its size is **7,569,156 bytes** and SHA-256 is `222c8047fa59c2ba98649e127f685405f124cf363d37b81274b96d4b50f36bd9`. Runtime loading requires only that approved local pack and the compiled application. Go executes both two-layer encoders, with ARM64 and AVX/FMA AMD64 dot products plus a scalar fallback. The asset stores the full-fit int8 word embedding as a lossless delta from the LoRA table. All row scales and remaining weights retain their frozen float32 values. The checkout includes the [license](assets/LICENSE-APACHE-2.0.txt) and [model attribution](assets/NOTICE.txt).

The original Google checkpoint is `google/bert_uncased_L-2_H-128_A-2` revision `30b0a37ccaaa32f332884b96992754e246e48c5f`. The frozen ONNX exports used for conversion have these SHA-256 hashes.

| Arm | SHA-256 |
| --- | --- |
| LoRA | `214311b9fb1ea2a6b601bc3473b4e9efd34acad07ec7bdba5b6dcda8ad12f098` |
| Full adaptation | `f384135eab1f3828d49dedf326cef95c05109e42624016efba18f40795d62ed0` |

## Rebuild and verify

The native runtime uses no Python dependency. Rebuilding the data pack requires Python with NumPy 2.5.5 and ONNX 1.19.0, and the external research exports in `run/{lora,full}`. The exporter verifies the original graph and vocabulary hashes before conversion.

```sh
research=../track-tagger-experiment-data/tiny-span
python3 internal/notesmodel/tools/export.py "$research/run" /tmp/bert-agreement.bin.gz
cmp /tmp/bert-agreement.bin.gz internal/notesmodel/assets/bert-agreement.bin.gz
go test ./internal/notesmodel
```

The private source fixture can be reproduced from the frozen research checkout, model exports and source inputs. The Go replay compares pre-catalog source spans for both arms and their exact intersection. Use a new output path when exporting.

```sh
data=../track-tagger-experiment-data
"$data/tiny-span/.venv/bin/python" -B internal/notesmodel/tools/reference.py \
  ../track-tagger-tiny-span/experiments/tiny-span "$data/tiny-span/run" \
  "$data/tiny-span-blind-final-v3/blind-inputs.jsonl" /tmp/native-span-reference.jsonl
GOMAXPROCS=1 TRACK_TAGGER_NOTES_MODEL_PARITY_INPUT=/tmp/native-span-reference.jsonl \
  go test -count=1 -run 'Test(FrozenReference|CPULatency)' -v ./internal/notesmodel
```

ARM64 native replay matched all 464 LoRA spans, 384 full-adaptation spans and 341 intersected spans on the 200 sealed recordings. A separate synthetic regression verifies that a four-digit-year day-first numeric date can reach downstream validation. CPU timing on an Apple M2 Max with Go 1.27.2 and `GOMAXPROCS=1` measured 600 warm predictions over those 200 recordings. Process CPU p50/p95/p99 were 4.641/8.834/30.372 ms, and wall p50/p95/p99 were 4.633/8.828/30.367 ms. The verified pack load took 79.31 ms wall. This measures both encoders and exact intersection before catalog filtering, with no subprocess or network. Linux ARM64 and translated Linux AMD64 reproduced the same spans. Windows ARM64 and AMD64 cross-compilation passed. Native Windows latency was unmeasured.
