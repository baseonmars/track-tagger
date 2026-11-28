"""Replay frozen LoRA suggestions for JSON lines of source notes and scan outputs."""
import argparse
from functools import lru_cache
import json
from pathlib import Path
import resource
import subprocess
import sys
import time

from evaluate import normalize_prediction, sha
from pilot import Predictor, source_lines


def cpu_time():
    children = resource.getrusage(resource.RUSAGE_CHILDREN)
    return time.process_time() + children.ru_utime + children.ru_stime


def run(source, output, asset_root, timings):
    freeze = json.loads((asset_root / "execution-freeze.json").read_bytes())
    directory = asset_root / "lora"
    for name, digest in freeze["packages"]["lora_adapted"].items():
        if sha(directory / name) != digest:
            raise ValueError(f"frozen model package changed: {name}")
    date_catalog = asset_root / "date-catalog"
    if sha(date_catalog) != freeze["prior_artifacts"][2]:
        raise ValueError("frozen date validator changed")
    predictor = Predictor(directory)

    @lru_cache(maxsize=None)
    def validate_date(value):
        run = subprocess.run([date_catalog, "--date"], input=json.dumps(value) + "\n",
                             text=True, encoding="utf-8", capture_output=True, check=True)
        result = json.loads(run.stdout)
        return result["date"] if not result["review_reasons"] else ""

    for line in source:
        if not line.strip():
            continue
        wall_start, cpu_start = (time.perf_counter(), cpu_time()) if timings is not None else (0, 0)
        case = json.loads(line)
        notes = {note["name"]: source_lines(note["text"]) for note in case["notes"]}
        if len(notes) != len(case["notes"]):
            raise ValueError("duplicate note name in recording")
        line_roles, accepted = normalize_prediction(case["id"], predictor.predict(notes),
                                                    notes, case["baseline"], validate_date)
        result = {"id": case["id"], "proposals": accepted,
                  "lines": [{"note": note, "line_index": number, "roles": sorted(roles)}
                            for (note, number), roles in line_roles.items()]}
        output.write(json.dumps(result, ensure_ascii=False, sort_keys=True) + "\n")
        output.flush()
        if timings is not None:
            timings.append({"wall_ms": 1000 * (time.perf_counter() - wall_start),
                            "cpu_ms": 1000 * (cpu_time() - cpu_start)})


if __name__ == "__main__":
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("--assets", type=Path, default=None, help="Asset directory for interpreted replay")
    cli.add_argument("--timing-file", type=Path, help="Save private latency samples to a new path")
    args = cli.parse_args()
    assets = args.assets
    if assets is None and getattr(sys, "frozen", False):
        assets = Path(sys._MEIPASS) / "assets"
    if assets is None:
        cli.error("--assets is required for interpreted replay")
    timings = [] if args.timing_file else None
    run(sys.stdin, sys.stdout, assets, timings)
    if args.timing_file:
        with args.timing_file.open("x") as stream:
            json.dump(timings, stream)
