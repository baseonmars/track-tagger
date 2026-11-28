# Real-source inventory and blind holdout

The two pre-existing note caches had no eligible unexposed source component. The context study had already run both its candidate and the original classifier on all 211 of its eligible representatives. Another 63 usable v2 packages shared those representatives' source components. The audit regenerated the earlier context inventory from source and matched its manifest bytes. It verified matching inference membership for both prior runs and matching source hashes for the original classifier. The context candidate prediction rows contain null source hashes, so only their membership could be checked directly.

| Available source reference | Recordings reviewed | Remaining recording references | Reviewed nonblank lines | Total nonblank lines |
|---|---:|---:|---:|---:|
| Legacy source review | 197 | 3 disputed | — | — |
| #38 validation | 50 | 0 | 2,440 | 2,440 |
| #38 test | 199 | 1 unresolved | 8,692 | 8,827 |
| Context study source review | 4 | 207 unresolved | 181 | 9,232 |

All four rows are development or regression references for this study. The review counts describe the original reference statuses. The legacy study used a different reference contract and the four context reviews retain unresolved track joins. The original discovery files contain 12,297 identifiers without a local package directory. The discovery results contain item metadata only. They do not supply package text, original filename inventories, or independent line and recording annotations.

## Replay the prior exposure audit

The first invocation requires the original caches and research checkouts. The prior context source screen was uncommitted, so the audit requires its observed SHA-256. The #38 code is pinned to 1848dec. Replaying the source screen checks the cached note bytes before grouping.

```sh
python3 -B experiments/tiny-span/inventory.py \
  ../track-tagger-experiment-data ../track-tagger-parser-study-v2 \
  ../track-tagger-parser-context
```

The checked aggregate counts are in [source inventory](results/source-inventory.json).

## New source discovery

The new acquisition uses Internet Archive's public etree discovery and original file inventory APIs. It stores original notes and only the audio filenames. Prior exposure is screened by known identifier and by the transitive source-component graph. The graph includes complete packages from both older caches, proposed test packages from all new discovery passes, and the earlier manually reviewed relationships. Its keys cover uploader, normalized artist and performance date, whole-note hash, technical-source lines and shingles, and source-email hashes. These checks can miss alternate metadata, paraphrased templates, and undocumented exposure.

Pool one representative per surviving component, with at most five per normalized artist alias. Recent discovery, uploader-filtered text discovery and historical-decade discovery use different search slices. Provisional authoring packets contain only source notes and filename inventories. Final selection is made from the combined pool, so a provisional recording can be excluded if a later package connects its family to prior exposure. The final source seal binds normalized note text, original names and file inventory by digest. Group and seal construction run offline against cached packages.

```sh
cache=../track-tagger-experiment-data
study=../track-tagger-parser-study-v2
python3 -B experiments/tiny-span/blind_sources.py "$cache" "$study" "$cache/tiny-span-recent"
python3 -B experiments/tiny-span/blind_sources.py "$cache" "$study" "$cache/tiny-span-historical" --historical
python3 -B experiments/tiny-span/seal_sources.py "$cache" "$study" "$cache/tiny-span-sealed" \
  "$cache/tiny-span-recent" "$cache/tiny-span-historical"
```

Source acquisition and source sealing are separate from annotation. The role and recording gates require two independent, source-only readings, adjudication and a separate reference seal. Keep the archived package bytes, blind source inventories and source labels outside Git.

## Frozen source set

Four source-discovery passes retrieved 2,762 unique complete new packages. Three passes were stopped once the combined screen reached the target. The final offline screen grouped them with 2,396 complete prior packages, excluded 2,390 new packages related to prior exposure and selected 200 distinct components. Another 159 packages shared an already represented component and 13 packages remained over the target. The largest artist alias contributed two selected recordings.

Two additional template checks joined repeated scaffolds to older source components. One checks the leading blank, the four header slots, adjacent `Source:` and `Transfer:` labels, intervening blanks and the first numbered track. The checks admit empty metadata slots and varying blank runs. It matched 47 prior and 31 new complete packages. FLAC fingerprint-only packages with or without the xACT generator header formed a separate family of 6 prior and 22 new packages. A fingerprint alongside a concert note did not trigger this family rule. Related templates that evade these fixed source checks may remain.

The combined selection used immutable source-only packet order and a fixed hash ordering for newly eligible records. The packet counts in the freeze count membership, independent of whether each source received any annotations. The retained source inventory and package ledger are external. Its aggregate freeze is in [blind source freeze](results/blind-source-freeze.json).

Run this replay to verify the prior audit and the final 200 source-only rows against the existing cache. It reproduces the source freeze, package ledger, blind manifest and normalized input bytes without network access.

```sh
bash experiments/tiny-span/replay_sources.sh
```

## Independent references

Two separate source readers examined every frozen packet before reconciliation. Adjudication recorded 200 recording references, including source-backed unknown fields and track joins. Reviewers resolved 5,899 of 5,901 nonblank lines and identified 128 recordings that require review. Of the 200 recordings, 101 have all four metadata values and a resolved title join for every supplied audio file. The answers retain 420 explicitly unresolved file joins.

The reference replay assembles adjudications in frozen source order, binds 18 original review artifacts, checks exact spans and original filename inventories, and compares the sealed recording and line references against the private original. The two provisional sources that received more than one adjudication use the earlier independent reader pair. Raw notes, reference spans, reviewer narratives and filename inventories remain external.
