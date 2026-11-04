# Passive note role classifier

`internal/noterole` proposes multiple roles for a line using a bounded passive JSON
linear model. No model is embedded or loaded by the application, and catalog,
readiness, review reasons, mappings and write plans remain unchanged.

Build the shared feature executable with `go build -o /tmp/noterole-features
./cmd/noterole-features`. It reads `{line,prev,next}` JSONL and emits sparse feature
counts. `--model /external/model.json` emits scores and abstaining suggestions.
The extractor owns Unicode lowercase, Unicode letter/digit/underscore words,
word unigrams/bigrams, rune character 2–4 grams after whitespace collapse, and
separate previous/next word features. Empty neighbors have explicit markers.
`noterole.Lines` removes an initial BOM, normalizes CRLF and CR to LF, and omits
one terminal empty line. Retain interior blank lines and original source evidence.
Feature version `noterole-unicode-v1` is intentionally distinct from the old
positive-only research sklearn feature implementation. Do not reuse those weights.

Offline trainer: `python tools/noterole/train.py rows.jsonl /external/model.json
--extractor /tmp/noterole-features --model-id ID`. Use the pinned research Python
training environment. Rows require split train/validation, line/prev/next and
explicit complete roles arrays. The trainer refuses test rows, uses training-only
vocabulary selection and eleven independent sigmoid heads, and selects thresholds
on validation only from the documented fixed grid. Disputed role targets set
`role_disputed=true`; uncertainty about factual content does not erase a known
role. Each head requires positive and negative training examples. Keep provenance,
source hashes, full-line coverage counts and annotation status with the dataset.
No trained artifact is committed or activated by this change.

Role accuracy on source-selected positive lines is not whole-note detection.
Fresh evaluation must include every line, technical/background/blank lines,
multirole targets, unknown counts, detection precision/recall and abstention
coverage. Freeze the model and thresholds before heldout evaluation. A role does
not establish a factual value, date, track ID, recording completeness or readiness.
Zero/OOV source features, threshold margins and incompatible background roles
abstain. Numerical scores are not calibrated proof of correctness.

Run `python -m unittest discover -s tools/noterole -p 'test_*.py'` for cross-language
feature/float64-score parity against a disposable model; no Python runtime or
training dependency is needed by the application. Runtime artifact validation
rejects unknown versions, dimensions, nonfinite/signed-zero parameters and models
over 10 MiB. Any future integration must keep deterministic parser precedence,
source evidence, review and identifier reconciliation, and separately audit every
classifier-dependent newly-ready recording against a fresh frozen end-to-end set.
