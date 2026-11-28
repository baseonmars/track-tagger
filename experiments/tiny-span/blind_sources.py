"""Acquire public text and filename inventories for independent source labeling."""
import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from hashlib import sha256
import json
from pathlib import Path
import sys
from urllib.parse import urlencode

from inventory import digest, indexed, load, require, verify_study


def write_jsonl(path, rows):
    with path.open("x", encoding="utf-8") as stream:
        for row in rows:
            stream.write(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n")


def run(cache, study, destination, target, workers, historical):
    require(
        not destination.is_relative_to(Path(__file__).resolve().parents[2]),
        "keep source notes outside the repository",
    )
    verify_study(study)
    sys.path.insert(0, str(study))
    from experiments.parser.acquire import acquire, decode_note, fetch
    from experiments.parser.study_v2.components import groups, keys
    from experiments.parser.study_v2.corpus import aliases, uploader
    from experiments.parser.study_v2.labels import source_hash

    def read(package, directory):
        notes = []
        for note in package["notes"]:
            path = directory / note["local_path"]
            data = path.read_bytes()
            require(sha256(data).hexdigest() == note["sha256"], "cached note digest mismatch")
            text, _ = decode_note(data)
            notes.append({"name": note["path"], "text": text.replace("\r\n", "\n").replace("\r", "\n")})
        return notes

    prior = {}
    exposed_ids = set()
    for root in (cache, cache / "v2"):
        for directory in sorted((root / "packages").iterdir()):
            if not directory.is_dir():
                continue
            exposed_ids.add(directory.name)
            if not (directory / "package.json").is_file():
                continue
            package = load(directory / "package.json")
            if package["complete"] and package["notes"] and package["audio_files"]:
                label = f"{root.name}/{directory.name}"
                prior[label] = package, "\n".join(note["text"] for note in read(package, directory))

    exclusions = load(cache / "v2/source-audit-exclusions.json")
    manual_edges = [
        (f"v2/{left}", f"v2/{right}") for left, right in exclusions["manual_edges"]
    ]
    exposed_ids.update(exclusions["excluded_ids"])
    exposed_ids.update(indexed(cache / "v2/selected-acquisition-manifest.jsonl", "package_id"))
    exposed_ids.update(indexed(cache / "v2/neighbor-currentpool-input-manifest.jsonl", "package_id"))
    exposures = {key for package, text in prior.values() for key in keys(package, text)}

    destination.mkdir(parents=True, exist_ok=False)
    old_uploaders = Counter(uploader(package) for package, _ in prior.values() if uploader(package))
    uploader_filter = "".join(
        f" AND NOT uploader:{json.dumps(value)}" for value, _ in old_uploaders.most_common(40)
    )
    query = 'collection:etree AND format:"Text"' + uploader_filter
    discovery = []
    entries = [(query, page) for page in range(1, 11)]
    if historical:
        entries = [
            (query + f" AND date:[{year}-01-01 TO {year + 9}-12-31]", page)
            for page in (1, 2, 3)
            for year in range(1960, 2021, 10)
        ]
    for position, (query_text, page) in enumerate(entries, start=1):
        params = urlencode({
            "q": query_text,
            "fl[]": ["identifier", "creator", "addeddate"],
            "sort[]": ["addeddate desc", "identifier asc"],
            "rows": 250,
            "page": page,
            "output": "json",
        }, doseq=True)
        raw = fetch("https://archive.org/advancedsearch.php?" + params)
        (destination / f"discovery-{position}.json").write_bytes(raw)
        discovery.extend(json.loads(raw)["response"]["docs"])
    candidates = {}
    for row in discovery:
        identifier = row["identifier"]
        if identifier not in exposed_ids:
            candidates.setdefault(identifier, row)

    def download(identifier):
        try:
            package = acquire(identifier, destination)
            notes = read(package, destination / "packages" / identifier)
            text = "\n".join(note["text"] for note in notes)
            files = [item["path"] for item in package["audio_files"]]
            return identifier, package, text, notes, files, None
        except (OSError, ValueError, KeyError) as error:
            return identifier, None, None, None, None, str(error)

    records = dict(prior)
    inputs = {}
    outcomes = []
    remaining = list(candidates)
    with ThreadPoolExecutor(max_workers=workers) as executor:
        while remaining:
            batch, remaining = remaining[: max(30, workers)], remaining[max(30, workers) :]
            for identifier, package, text, notes, files, error in executor.map(download, batch):
                if error:
                    outcomes.append({"id": identifier, "reason": "acquisition failed", "detail": error})
                    continue
                records[identifier] = package, text
                if not uploader(package):
                    outcomes.append({"id": identifier, "reason": "missing uploader"})
                elif keys(package, text) & exposures:
                    outcomes.append({"id": identifier, "reason": "direct old exposure link"})
                else:
                    inputs[identifier] = {"id": identifier, "notes": notes, "files": files}
            component = groups(records, manual_edges)
            exposed_groups = {component[identifier] for identifier in prior}
            representative = {}
            for identifier in inputs:
                group = component[identifier]
                if group not in exposed_groups:
                    representative.setdefault(group, identifier)

            # Take one package per source component, balancing performers where possible.
            selected = []
            counts = {}
            for group, identifier in representative.items():
                names = aliases(records[identifier][0]["artist"])
                if any(counts.get(name, 0) >= 5 for name in names):
                    continue
                selected.append(inputs[identifier] | {"group": group, "split": "blind_test"})
                for name in names:
                    counts[name] = counts.get(name, 0) + 1
            # Treat this early labeling packet as provisional until the final graph is sealed.
            packet = destination / "early-review-inputs.jsonl"
            if len(selected) >= 4 and not packet.exists():
                write_jsonl(packet, selected[:4])
            print(f"Attempted {len(candidates) - len(remaining)}; {len(selected)} eligible components", flush=True)
            if len(selected) >= target:
                break

    selected = selected[:target]
    selected_ids = {case["id"] for case in selected}
    for identifier in inputs.keys() - selected_ids:
        reason = "linked to an old source component" if component[identifier] in exposed_groups else "component or artist cap or target"
        outcomes.append({"id": identifier, "reason": reason})
    write_jsonl(destination / "acquisition-outcomes.jsonl", sorted(outcomes, key=lambda row: row["id"]))
    write_jsonl(destination / "blind-inputs.jsonl", selected)
    write_jsonl(destination / "blind-manifest.jsonl", (
        {"id": case["id"], "group": case["group"], "source_input_sha256": source_hash(case)}
        for case in selected
    ))
    (destination / "blind-freeze.json").write_text(json.dumps({
        "recordings": len(selected),
        "prior_cached_records": len(prior),
        "discovery_available": len(candidates),
        "discovery_attempted": len(candidates) - len(remaining),
        "blind_inputs_sha256": digest(destination / "blind-inputs.jsonl"),
        "blind_manifest_sha256": digest(destination / "blind-manifest.jsonl"),
    }, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for argument in ("cache", "study", "destination"):
        parser.add_argument(argument, type=lambda value: Path(value).resolve())
    parser.add_argument("--target", type=int, default=200)
    parser.add_argument("--workers", type=int, default=16)
    parser.add_argument("--historical", action="store_true")
    run(**vars(parser.parse_args()))
