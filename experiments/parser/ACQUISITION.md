# Original recording package acquisition

Run from the repository root:

```sh
python3 experiments/parser/acquire.py \
  --root /Users/dan/code/track-tagger-experiment-data \
  --target 1200 --artist-limit 30 --workers 8 --hours 4
python3 -m unittest discover -s experiments/parser -p test_acquire.py -v
```

The acquisition uses Archive.org's public advanced-search and item-metadata APIs, followed by direct original text downloads. It downloads no audio. Discovery is stratified by recording decade (1960s through 2020s), with up to 2,000 downloads-ranked results per decade. A fixed-seed round robin across creator buckets balances candidate order. The cap applies again to the artist in the acquired item metadata. This is a reproducible convenience sample, not a random sample of the Archive.

Original material stays outside Git under `ROOT/packages/IDENTIFIER/`. A package contains `package.json`, the original metadata response `archive-metadata.json`, and byte-preserved original notes under `notes/`. Original audio paths, sizes, formats and available checksums are recorded; derivative audio files are excluded. Every original `.txt`, `.text`, `.nfo` and `.md` file is required. Checksum manifests and machine metadata documents are not treated as notes. Original notes with unknown document suffixes are outside this definition of completeness.

The acquirer rejects missing original notes/audio inventories, unsafe paths, binary/empty notes, notes above 4 MiB, total note packages above 16 MiB, and size/checksum mismatches. Downloads use bounded concurrency, 35-second request timeouts and three attempts. Publication of `package.json` is the package completion marker. Restarting verifies cached note hashes and reuses complete packages; partial packages are retried. A completed run writes a Git-safe manifest and aggregate acquisition report. Failure records and progress reports remain outside Git. Timeouts apply to individual network reads; a running batch finishes before the overall time limit is checked.

The manifest records identifiers, artists, dates, source URLs, acquisition timestamps, reported rights/license URLs, original note hashes/encodings/sizes, audio paths and the metadata hash. It contains no original note text or free-form reference metadata. Public availability and reported license URLs do not establish permission to redistribute the original notes. Inspect item-specific terms before sharing source material.

`normalize_packages(Path(ROOT), manifest=PATH)` returns candidate inputs with the same decoded UTF-8 note text for every candidate. It keeps original filenames and the complete original audio inventory; raw source encoding stays in provenance. Original bytes and hashes remain available for audits. Flags mark non-FLAC audio, audio extensions outside the current scanner, note extensions outside `.txt`/`.nfo`, and encodings requiring transcoding beyond UTF-8/Windows-1252. These packages support parser experiments; this transcode step means results are not a byte-for-byte test of production scanner decoding. Reference metadata remains in package provenance and is never model input or an automatic oracle.

Before labels or predictions, `group_packages(cases)` merges alternate sources with the same normalized artist/date, exact normalized notes, near-duplicate notes (five-word-shingle Jaccard at least 0.85), and similar templates (at least 0.90 after replacing track-number lines and numbers). Similarity merges require at least 30 shared shingles; short notes instead require at least five shared shingles and identical normalized line structure. Groups merge transitively and receive a stable hash of sorted member IDs. The grouping is a conservative text heuristic, not a guarantee that paraphrased templates or incorrectly dated alternate sources are found. The split harness must keep every merged group within one frozen partition and record any resulting quota shortfalls.

`acquisition-report.json` is the authoritative actual count/diversity report. Target counts do not imply acquisition success. Audio-format diversity describes source inventories, not support for writing those formats.
