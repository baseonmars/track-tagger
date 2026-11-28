"""Compare the packaged LoRA CLI with the sealed private suggestion audit."""
import argparse
import json
import math
from pathlib import Path
import statistics
import subprocess
import tempfile
import time

from evaluate import rows, run_catalog, sha


def percentile(values, percentile_number):
    return round(sorted(values)[math.ceil(len(values) * percentile_number / 100) - 1], 3)


def main():
    cli = argparse.ArgumentParser(description=__doc__)
    for name in ("bundle", "cases", "seal", "fresh-result", "audit", "catalog", "output"):
        cli.add_argument("--" + name, type=Path, required=True)
    args = cli.parse_args()
    result = json.loads(args.fresh_result.read_text())
    seal = json.loads(args.seal.read_text())
    if (result["phase"] != "fresh" or not seal["independent_source_review_complete"]
            or result["input_sha256"]["seal"] != sha(args.seal)
            or result["input_sha256"]["cases"] != sha(args.cases)
            or result["catalog_binary_sha256"] != sha(args.catalog)):
        raise ValueError("sources or catalog differ from the sealed fresh evaluation")
    audit = json.loads(args.audit.read_text())
    source = rows(args.cases)
    baseline = run_catalog(args.catalog, source)
    errors = {cid: row["error"] for cid, row in baseline.items() if "error" in row}
    if errors != audit["catalog_errors"]:
        raise ValueError("catalog errors differ from the fresh private audit")
    lora = audit["arms"]["lora_adapted"]
    if lora["prediction_failures"]:
        raise ValueError("fresh audit contains predictor failures requiring explicit handling")
    expected = {case["id"]: [] for case in source}
    for item in lora["proposals"]:
        expected[item["id"]].append({key: value for key, value in item.items()
                                     if key not in {"id", "status", "source_input_sha256"}})
    payload = "".join(json.dumps({"id": row["id"], "notes": row["notes"], "baseline": baseline[row["id"]]},
                                 ensure_ascii=False) + "\n" for row in source)
    exe = args.bundle.resolve() / "track-tagger-span"
    command = ["/usr/bin/sandbox-exec", "-p", "(version 1)(allow default)(deny network*)", str(exe)]
    timings = []
    with tempfile.TemporaryDirectory() as clean_home:
        clean = Path(clean_home)
        env = {"HOME": clean_home, "PATH": "/usr/bin:/bin", "TOKENIZERS_PARALLELISM": "false",
               "HF_HUB_OFFLINE": "1", "TRANSFORMERS_OFFLINE": "1"}
        for _ in range(3):
            start = time.perf_counter()
            subprocess.run(command, input="", text=True, capture_output=True, check=True, cwd=clean, env=env)
            timings.append(round(1000 * (time.perf_counter() - start), 3))
        raw = clean / "private-timings.json"
        run = subprocess.run([*command, "--timing-file", str(raw)], input=payload, text=True,
                             capture_output=True, encoding="utf-8", check=True, cwd=clean, env=env)
        samples = json.loads(raw.read_text())
        actual = [json.loads(line) for line in run.stdout.split("\n") if line]
    if [row["id"] for row in actual] != list(expected) or len(samples) != len(expected):
        raise ValueError("bundle returned different recording membership or timing sample count")
    for row in actual:
        if row["proposals"] != expected[row["id"]]:
            raise ValueError("bundle suggestions differ from the frozen audit")
    report = {
        "executable_sha256": sha(exe),
        "sealed_source_sha256": sha(args.cases),
        "private_audit_sha256": sha(args.audit),
        "recordings": len(expected),
        "failed_catalog_scans_with_identical_abstention": len(errors),
        "displayed_proposals_with_exact_match": sum(map(len, expected.values())),
        "network_denied": True,
        "cold_process_wall_ms": timings,
        "cold_process_median_wall_ms": statistics.median(timings),
        "warm_recording_latency": {
            metric: {f"p{pct}": percentile([row[metric] for row in samples], pct)
                     for pct in (50, 95, 99)} for metric in ("cpu_ms", "wall_ms")
        },
        "timing_scope": "Per recording includes parsing input, proposal validation and output, including date child CPU. Catalog scan and model startup are outside warm samples. Fresh process startup uses the warm OS filesystem cache.",
    }
    with args.output.open("x") as stream:
        json.dump(report, stream, sort_keys=True, indent=2)
        stream.write("\n")


if __name__ == "__main__":
    main()
