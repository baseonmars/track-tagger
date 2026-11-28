# Offline macOS CPU bundle

The research CLI packages the frozen LoRA candidate, Python and ONNX Runtime, WordPiece vocabulary, policy and original Go date validator. The macOS arm64 executable reads source note text and an existing catalog preview per JSONL input row. It emits suggestions and line roles with the frozen evaluator's validation, date parsing and review policy. It accepts no audio paths and operates on the supplied preview data. [Measured package contents and verification](results/macos-package.json).

Each input row has an `id`, a `notes` array of `{"name":"note.txt","text":"…"}` entries, and a `baseline` with the application's `ready` and `metadata` or original scan error. A ready recording or scan failure abstains from display. The executable and `_internal` directory together form the distribution. PyInstaller uses ad hoc signing for the local build. The package has no notarization or cross-platform verification.

The bundle occupies 111,131,126 bytes across 105 regular files. All 188 displayed proposals matched the sealed audit across 200 recordings, including abstention on all 11 catalog failures. Warm p95 added latency on this M2 Max was 6.068 ms CPU and 6.481 ms wall. Three fresh process launches took approximately 148, 148 and 1,113 ms, with a median of 148.45 ms.

## Build and verify

Use CPython 3.12.2 on macOS arm64 and the pinned packaging dependencies. The venv and frozen model artifacts live under the external experiment data directory. The build checks the hashes of the frozen Python entrypoints, and the CLI checks the frozen graph, vocabulary, policy and validator hashes before inference.

```sh
export DATA="$(cd ../track-tagger-experiment-data && pwd)"
package="$DATA/tiny-span/macos-package"
mkdir -p "$package"
uv venv --python 3.12.2 "$package/.venv"
uv pip install --python "$package/.venv/bin/python" -r experiments/tiny-span/requirements-macos-package.txt
bash experiments/tiny-span/package-macos.sh "$package/replay"
"$package/replay/dist/track-tagger-span/track-tagger-span" < "$package/replay/smoke.jsonl"
```

Use a new output directory for each build. The script writes an uncompressed file-count and byte-size report and compares five synthetic cases to interpreted execution. The smoke test copies the built package to a clean directory and denies network and file reads from the original package, build environment and base Python installation. It verifies an accepted date and abstention on ready and background examples. The byte total sums files as distributed inside the directory. The embedded runtime archives retain PyInstaller's internal compression.

With the sealed source inputs and exact private evaluator outputs present, compare the package across all fresh recordings. The command replays the frozen preview scanner to supply baseline inputs. The package only replays the proposal overlay and its date validator.

```sh
fresh="$DATA/tiny-span-eval/fresh-bound"
sealed="$DATA/tiny-span/blind/canonical-reference/sealed"
"$package/.venv/bin/python" -B experiments/tiny-span/package_validate.py \
  --bundle "$package/replay/dist/track-tagger-span" \
  --cases "$DATA/tiny-span-blind-final-v3/blind-inputs.jsonl" \
  --seal "$sealed/seal.json" --fresh-result "$fresh/fresh.json" \
  --audit "$fresh/fresh-private.json" --catalog "$DATA/tiny-span-eval/catalog" \
  --output "$package/replay/fresh-parity.json"
```

This comparison checks every displayed proposal, including source location, quote, normalized value and score. The packaged runtime is sandboxed with network access denied for the full comparison. Cold latency measures fresh process execution with the OS filesystem cache already warm. Warm CPU and wall times cover the packaged CLI from JSON input parsing to writing the suggestions, including the Go date subprocess when run. The existing catalog preview scan is outside the added latency. Rebuilding can change signing and executable bytes. Compare frozen asset hashes and semantic smoke parity across builds.
