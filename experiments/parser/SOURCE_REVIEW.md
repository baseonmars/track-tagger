# Independent source annotation review

Real accuracy labels require independent source passes. The first pass drafts an annotation from complete notes and filenames. The routine CLI second pass reviews that draft against the same complete source, without candidate predictions. For the final full test, independent source authoring/review by Astra, Sol and the harness agent was sealed before reconciliation with Luna drafts and reconciled against complete source; this is a distinct route from the routine hosted reviewer below. `source_review.py` implements the second pass through the shared hosted adapter and cost ledger. Its reviewed labels are agent-reviewed, not human-certified.

After acquisition, grouping, split freezing and explicit execution authorization:

```sh
python3 -m experiments.parser.source_review \
  /Users/dan/code/track-tagger-experiment-data/run/frozen-real.json \
  /Users/dan/code/track-tagger-experiment-data/run/annotations-luna.jsonl \
  /Users/dan/code/track-tagger-experiment-data/run/sol-config.json \
  /Users/dan/code/track-tagger-experiment-data/run \
  --stage-usd 15 --hours 3
```

The CLI reviews validation/test packages only. It requires the existing shared `session.json`, uses its inference deadline, and reserves calls in the persistent shared budget ledger. For this CLI route, the reviewer must be exactly `gpt-6.1-sol` at verified standard input/output prices of $2/$10 per million tokens. The adapter uses low reasoning. The output and audit remain outside Git because they contain source text and full annotations.

Each reviewer returns `accepted`, `corrected`, `disputed` or `unresolved`, a complete oracle and reasons. Only accepted/corrected responses with all required fields, the exact filename inventory, valid dates, exact source citations and consistent title/mapping fields become accuracy eligible. Unmapped files, unmatched titles, duplicate identifiers, unresolved source evidence and non-FLAC inventories require review. An accepted verdict must preserve the draft's semantic annotation; changes require a corrected verdict.

Ambiguous music evidence does not disqualify a label: a correct oracle can leave fields empty, leave mappings unresolved and require review. `disputed` and `unresolved` mean the correct expected safe response could not be established, so those annotations stay outside the accuracy denominator. This distinction preserves false-ready evaluation on ambiguous source packages.

Every successful API response is retained in the external audit alongside the full draft, exact source/draft/response/prompt/config hashes, actual model, request settings, usage, cache status and review time. Resume verifies the same fingerprint before reusing an output. Adapter failures retain available fixed diagnostics and usage without persisting exception messages. Deterministic quote checks prove that a quote exists, not that its semantic interpretation is correct; independent model review and source-only spot checks provide additional evidence, not a guarantee of error-free labels.

`source-review-smoke-report.json` records five synthetic training source reviews. These test the protocol and hosted path; they do not establish real-recording accuracy. Run synthetic protocol checks with:

```sh
python3 -m unittest experiments.parser.tests.test_source_review -v
```

## Final test source review

Source routes include Astra authoring 140 of the final 160 packages and the harness agent authoring 20; common40 used independent Astra/draft review and Sol reviews, with a separate source-only Sol Vulfpeck correction. The audit retains exact routes. All 200 test packages were source-reviewed: 270 notes, 366,490 characters and 3,953 filenames. The reconciled source artifact is external `astra-reconciled-source-labels200.jsonl`, SHA256 `be073c3d1020158038ce6bc5252ebd5cb6e1b2df740a51eba060f766f1f89020`; its audit is `astra-source-audit200-report.json`. Of 197 eligible labels, 81 expect ready and 116 expect review; three cases remain disputed. These are agent-reviewed labels, not human-certified truth.

The final external label freeze is `frozen-test-full200.json`, SHA256 `6102b07a348e08a342134b53d9d808fbc70eca6aff1d51003a8dbb58134c7b9f`. The fixed common-40 subset remains unchanged: 39 eligible labels and one disputed case, with 22 all-FLAC inputs and 21 all-FLAC eligible labels. Source review included every supplied note and filename. Grouping heuristics and citation checks do not establish semantic correctness.

Extraction candidates receive note names/text and relative filenames only. Original Archive reference metadata is withheld. Label conventions include city plus region and event plus physical venue when supported; a mismatch against those conventions is not automatically fabricated information. Candidate predictions are never a source of gold labels. Final scorer v3 was frozen before test dispatch; do not tune prompts or scoring against test outcomes.
