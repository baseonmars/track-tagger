# Notes parser evaluation

The frozen synthetic corpus exercises parser behavior against source-derived note formats while keeping original Internet Archive text and all media outside the repository. The corpus contains 200 synthetic cases derived from 200 distinct inspected live recordings: 140 development and 60 held-out. It covers 20 performing acts, with no more than 38 cases per act, and recordings from the 1970s, 1980s, 1990s, 2010s and 2020s. The [corpus description](notes-corpus.md) documents provenance, transformations, source discrepancies and the frozen corpus hashes.

Discovery and source-page inspection used Firecrawl MCP. Original note text bytes and item inventories were retrieved directly from Internet Archive metadata/download endpoints; no audio was downloaded. Source-derived examples were shortened to excerpts, with names and titles replaced by fictional text. Oracle titles and per-file mappings were independently authored from inspected source evidence, without parser output. The held-out cases were frozen before parser implementation and should not be used to tune changes.

## Results

| Split | Measure | Baseline | Candidate |
| --- | --- | ---: | ---: |
| Development | Exact cases | 133/140 | 134/140 |
| Development | Correct titles | 669/714 | 684/714 |
| Development | Correct file mappings | 678/698 | 678/698 |
| Development | Incorrectly accepted mappings | 1 | 0 |
| Development | False-ready cases | 0 | 0 |
| Development | Cases sent to review | 56 | 55 |
| Held-out | Exact cases | 54/60 | 59/60 |
| Held-out | Correct titles | 243/261 | 261/261 |
| Held-out | Correct file mappings | 261/279 | 279/279 |
| Held-out | Incorrectly accepted mappings | 5 | 0 |
| Held-out | False-ready cases | 1 | 0 |
| Held-out | Cases sent to review | 32 | 30 |

| Benchmark subset | Median time/op | Median bytes/op | Median allocs/op |
| --- | ---: | ---: | ---: |
| Development baseline | 6.645 ms | 257,437 | 5,150 |
| Development candidate (post-review) | 7.370 ms | 458,770 | 7,911 |
| Held-out baseline | 2.870 ms | 100,789 | 2,020 |
| Held-out candidate (post-review) | 2.922 ms | 180,982 | 3,127 |

Title accuracy uses the number of expected nonempty titles as its denominator; mapping accuracy uses expected per-file mappings. Exact cases require every title and mapping to match, the unresolved-title set to match, and the recording's ready/review status to match. Incorrectly accepted mappings count files accepted with the wrong disc, number or title. False-ready counts cases marked ready when the oracle expects review. Cases sent to review is a count, not an accuracy measure; fewer reviews can be useful only when the safety counts remain zero.

The candidate's six remaining development mismatches are `fixture-010`, `fixture-071`, `fixture-072`, `fixture-073`, `fixture-132` and `fixture-134`. The one held-out mismatch is `fixture-029`: its frozen oracle marks it as not requiring review despite six expected titles and only four files. The candidate conservatively reports missing identifiers for `1:8` and `1:9` and requests review. The oracle was not changed after evaluation; this case is a corpus-label limitation, not evidence that the candidate should accept the incomplete recording.

The initial held-out evaluation was blind to parser implementation work. Subsequent independent static review found two safety issues: equal-indentation checksum rows could be treated as tracks, and a raw decoded-text size guard was needed at 4 MiB. Those fixes did not change corpus cases or oracle expectations. Final reevaluation after those fixes produced the same title, mapping, exact-case and safety metrics shown above; the updated benchmark medians are shown in the table. Both initial and post-review raw reports are retained below.

Benchmark figures are medians of five runs with one CPU, a 300 ms target per benchmark, and allocation reporting. They compare total development and held-out corpus parsing time, bytes processed, allocations and bytes allocated per operation; they do not measure end-to-end application latency. The post-review candidate is modestly slower and allocates more than baseline on both splits.

The reported environment is Windows/amd64 on an AMD Ryzen 7 9800X3D, Go 1.27.3, with benchmarks constrained to one CPU. The player test used Node 24.19.0.

## Reproduction

Run from the repository root with Go installed. The standard test checks safety: it fails on false-ready cases and incorrectly accepted mappings. It reports exact mismatches without failing for them. Set `TRACK_TAGGER_CORPUS_REPORT_ONLY=1` to collect baseline metrics even when safety mismatches exist. Set `TRACK_TAGGER_CORPUS_REQUIRE_EXACT=1` to make every exact-case mismatch fail. The split variable limits evaluation to one frozen partition.

PowerShell candidate run:

```powershell
go test ./internal/catalog -run '^TestNotesCorpus$' -v
$env:TRACK_TAGGER_CORPUS_SPLIT = 'development'
go test ./internal/catalog -run '^TestNotesCorpus$' -v
$env:TRACK_TAGGER_CORPUS_SPLIT = 'held-out'
go test ./internal/catalog -run '^TestNotesCorpus$' -v
Remove-Item Env:TRACK_TAGGER_CORPUS_SPLIT
go test ./internal/catalog -run '^$' -bench '^BenchmarkNotesCorpus$' -benchmem -cpu=1 -count=5 -benchtime=300ms
```

POSIX shell candidate run:

```sh
go test ./internal/catalog -run '^TestNotesCorpus$' -v
TRACK_TAGGER_CORPUS_SPLIT=development go test ./internal/catalog -run '^TestNotesCorpus$' -v
TRACK_TAGGER_CORPUS_SPLIT=held-out go test ./internal/catalog -run '^TestNotesCorpus$' -v
go test ./internal/catalog -run '^$' -bench '^BenchmarkNotesCorpus$' -benchmem -cpu=1 -count=5 -benchtime=300ms
```

To reproduce the baseline without changing the current checkout, create a separate worktree at `ab34cb3`, which contains the frozen corpus and harness with the original production parser (`ffecbdf`):

```sh
git worktree add ../track-tagger-baseline ab34cb3
cd ../track-tagger-baseline
```

Then run the report-only test and benchmark commands above in that worktree. For PowerShell, set the report-only variable before the test:

```powershell
$env:TRACK_TAGGER_CORPUS_REPORT_ONLY = '1'
go test ./internal/catalog -run '^TestNotesCorpus$' -v
Remove-Item Env:TRACK_TAGGER_CORPUS_REPORT_ONLY
go test ./internal/catalog -run '^$' -bench '^BenchmarkNotesCorpus$' -benchmem -cpu=1 -count=5 -benchtime=300ms
```

For a POSIX baseline run, prefix the corpus test command with `TRACK_TAGGER_CORPUS_REPORT_ONLY=1`; this allows baseline mismatches to be reported without failing the test. For example: `TRACK_TAGGER_CORPUS_REPORT_ONLY=1 go test ./internal/catalog -run '^TestNotesCorpus$' -v`.

The recorded baseline/candidate JSON is produced by `TestNotesCorpus` logging its metrics. Formatting, `go vet`, six CGO-disabled cross-builds, the Node 24 player test, `go test ./...`, and a 15-second notes-parser fuzz run passed. Native race validation is provided by PR CI; local race execution was unavailable because CGO was disabled and no C compiler was installed.

The raw reports and benchmark output are retained in [`results/`](results/): baseline metrics, initial candidate metrics, final post-review candidate metrics, and the initial and post-review five-run benchmark logs. They contain no original note text.
