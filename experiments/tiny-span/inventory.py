"""Audit real-source cache exposures without running a parser or candidate."""
import argparse
from collections import Counter
from hashlib import sha256
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

STUDY_COMMIT = "1848decbea38a428a7e95e62c428cf7ffb1e6a73"
CONTEXT_SCREEN_SHA = "cdf7767783a0ac809a56730b6a178a2a30f7508b16967b8b8a6cddd186b06aa8"


def jsonl(path):
    with path.open(encoding="utf-8") as stream:
        for line in stream:
            yield json.loads(line)


def indexed(path, key="id"):
    result = {}
    for row in jsonl(path):
        identifier = row[key]
        if identifier in result:
            raise ValueError(f"duplicate identifier in {path}")
        result[identifier] = row
    return result


def load(path):
    return json.loads(path.read_bytes())


def canonical_digest(rows):
    return sha256(json.dumps(rows, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


def digest(path):
    return sha256(path.read_bytes()).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def role_inventory(path):
    rows = list(jsonl(path))
    nonblank = [row for row in rows if row["line"].strip()]
    return {
        "source_lines": len(rows),
        "nonblank_lines": len(nonblank),
        "reviewed_nondisputed_nonblank_lines": sum(
            not row["role_disputed"] and row["label_status"] in ("agent-reviewed", "adjudicated")
            for row in nonblank
        ),
    }


def verify_study(study):
    study_head = subprocess.check_output(["git", "-C", study, "rev-parse", "HEAD"], text=True).strip()
    require(study_head == STUDY_COMMIT, "unexpected #38 study commit")
    require(
        not subprocess.check_output(["git", "-C", study, "status", "--porcelain", "--", "experiments/parser"]),
        "#38 parser research code differs from frozen commit",
    )


def audit(cache, study, context):
    v2 = cache / "v2"
    fresh = cache / "context/fresh"
    prior_run = cache / "context/run"

    screen = context / "experiments/context/fresh_screen.py"
    require(digest(screen) == CONTEXT_SCREEN_SHA, "unexpected prior context source screen")
    verify_study(study)
    with tempfile.TemporaryDirectory(prefix="tiny-span-inventory-") as temp:
        scratch = Path(temp)
        subprocess.run(
            [sys.executable, "-B", screen, cache, study, scratch],
            check=True,
            stdout=subprocess.DEVNULL,
        )
        for name in ("fresh-sources.jsonl", "fresh-inputs.jsonl", "fresh-audit.json"):
            require(digest(scratch / name) == digest(fresh / name), f"prior screen differed for {name}")

    fresh_sources = indexed(fresh / "fresh-sources.jsonl", "package_id")
    fresh_predictions = indexed(prior_run / "context-fresh-predictions.jsonl")
    require(fresh_sources.keys() == fresh_predictions.keys(), "prior context inference membership differs")
    prior_original = indexed(prior_run / "original-fresh.jsonl")
    require(fresh_sources.keys() == prior_original.keys(), "prior original inference membership differs")
    require(
        all(
            row["source_input_sha256"] == prior_original[identifier]["source_input_sha256"]
            for identifier, row in fresh_sources.items()
        ),
        "prior context inference source hash differs",
    )
    context_recording_gold = indexed(fresh / "fresh-recording-gold.jsonl")
    require(context_recording_gold.keys() == fresh_sources.keys(), "prior context gold membership differs")
    context_gold_freeze = load(fresh / "fresh-label-audit.json")
    for name, expected in context_gold_freeze["sha256"].items():
        require(digest(fresh / name) == expected, f"prior context gold freeze differs for {name}")

    legacy_gold = indexed(cache / "run/astra-reconciled-source-labels200.jsonl")
    training_manifest = indexed(v2 / "replayed-cases.jsonl")
    v2_refs = {}
    for split in ("validation", "test"):
        directory = v2 / f"{split}-gold"
        recording_path = directory / "recording-gold.jsonl"
        line_path = directory / "line-gold.jsonl"
        freeze = load(directory / "gold-freeze.json")
        require(canonical_digest(list(jsonl(recording_path))) == freeze["recording_gold_sha256"], f"{split} recording freeze differs")
        require(canonical_digest(list(jsonl(line_path))) == freeze["line_gold_sha256"], f"{split} line freeze differs")
        recordings = indexed(recording_path)
        require(
            recordings.keys() == {identifier for identifier, row in training_manifest.items() if row["split"] == split},
            f"{split} gold membership differs",
        )
        v2_refs[split] = {
            "recording_statuses": dict(Counter(row["label_status"] for row in recordings.values())),
            **role_inventory(line_path),
        }

    source_screen = load(fresh / "fresh-audit.json")
    cached_ids = {
        child.name
        for directory in (cache, v2)
        for child in (directory / "packages").iterdir()
        if child.is_dir()
    }
    discovery_ids = {
        row["identifier"]
        for path in ("discovery.json", "v2/discovery-recent-text.json", "v2/discovery-agent-candidates.json")
        for row in load(cache / path)
    }
    return {
        "prior_context_source_screen": source_screen,
        "prior_context_inference_recordings": len(fresh_predictions),
        "prior_context_inference_artifact_sha256": digest(prior_run / "context-fresh-predictions.jsonl"),
        "cached_discovery_only_identifiers": len(discovery_ids - cached_ids),
        "eligible_unexposed_cached_components": len(fresh_sources.keys() - fresh_predictions.keys()),
        "eligible_unexposed_complete_recording_references": 0,
        "available_development_regression_gold": {
            "legacy_200": dict(Counter(row["label_status"] for row in legacy_gold.values())),
            "study_v2_package_splits": dict(Counter(row["split"] for row in training_manifest.values())),
            "study_v2": v2_refs,
            "prior_context": {
                "recording_statuses": dict(Counter(row["label_status"] for row in context_recording_gold.values())),
                **role_inventory(fresh / "fresh-line-gold.jsonl"),
            },
        },
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for argument in ("cache", "study", "context"):
        parser.add_argument(argument, type=Path)
    arguments = {name: value.resolve() for name, value in vars(parser.parse_args()).items()}
    print(json.dumps(audit(**arguments), indent=2, sort_keys=True))
