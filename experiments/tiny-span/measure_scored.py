"""Time the scored suggestion path with its original Go date validation."""
import argparse
import functools
import json
from pathlib import Path
import resource
import subprocess
import time

from evaluate import normalize_prediction, run_catalog
from measure import percentile, source_sample
from pilot import Predictor, sha256, source_lines


def child_cpu():
    children = resource.getrusage(resource.RUSAGE_CHILDREN)
    return children.ru_utime + children.ru_stime


def duration(rows):
    return {name: {f"p{p}": percentile([r[index] for r in rows], p) for p in (50, 95, 99)}
            for index, name in enumerate(("wall_ms", "python_cpu_ms", "date_process_cpu_ms", "total_cpu_ms"))}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("package", "v2", "catalog", "date-catalog", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    samples = source_sample(args.v2, 100)
    ids = {ident for ident, _ in samples}
    with (args.v2 / "replayed-cases.jsonl").open() as file:
        applications = run_catalog(args.catalog, [row for line in file if
                                   (row := json.loads(line))["split"] == "train" and row["id"] in ids])
    p = Predictor(args.package)
    calls = 0

    def parse_date(value):
        nonlocal calls
        calls += 1
        run = subprocess.run([args.date_catalog, "--date"], input=json.dumps(value) + "\n",
                             text=True, encoding="utf-8", capture_output=True, check=True)
        result = json.loads(run.stdout)
        return result["date"] if not result["review_reasons"] else ""

    def predict(ident, texts, date):
        notes = {name: source_lines(text) for name, text in texts.items()}
        return normalize_prediction(ident, p.predict(notes), notes, applications[ident], date)

    # Load the model and Go binary before timed runs.
    predict(*samples[0], parse_date)
    cached = functools.lru_cache(maxsize=None)(parse_date)
    result = {}
    outputs = {}
    for name, date in (("without_date_cache", parse_date), ("cohort_date_cache", cached)):
        timings = []
        dates_only = []
        calls = 0
        for _ in range(3):
            for ident, texts in samples:
                count = calls
                start = time.perf_counter()
                cpu = time.process_time()
                children = child_cpu()
                output = predict(ident, texts, date)
                py_cpu = time.process_time() - cpu
                go_cpu = child_cpu() - children
                periods = (time.perf_counter() - start, py_cpu, go_cpu, py_cpu + go_cpu)
                timings.append(periods)
                if calls > count:
                    dates_only.append(periods)
                if name == "without_date_cache":
                    outputs[ident] = output
                elif outputs[ident] != output:
                    raise ValueError("date memoization changed an evaluation decision")
        result[name] = dict(operations=len(timings), go_date_processes=calls,
                            timings=duration(timings), with_date_subprocess=duration(dates_only) if dates_only else None)
    result.update(recordings=len(samples), threads=1,
                  accepted_recordings=sum(bool(row[1]) for row in outputs.values()),
                  baseline_scan_failures=sum(bool(row.get("error")) for row in applications.values()),
                  package_bytes=sum(p.stat().st_size for p in args.package.iterdir()),
                  date_validator_binary_bytes=args.date_catalog.stat().st_size,
                  date_validator_binary_sha256=sha256(args.date_catalog),
                  catalog_binary_sha256=sha256(args.catalog),
                  evaluate_py_sha256=sha256(Path(__file__).with_name("evaluate.py")),
                  notes="Production scan excluded. Existing catalog date parsing runs via a separate Go executable.")
    with args.output.open("x") as stream:
        json.dump(result, stream, sort_keys=True, indent=2)
        stream.write("\n")


if __name__ == "__main__":
    main()
