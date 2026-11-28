"""Measure CPU inference and verify byte-identical predictions from frozen packages."""
import argparse
from collections import Counter
import hashlib
from importlib.metadata import distribution
import json
from pathlib import Path
import platform
import subprocess
import sys
import os
import tempfile
import time

from pilot import Predictor, sha256, source_lines


def source_sample(v2, count):
    with (v2 / "replayed-cases.jsonl").open() as file:
        train = [row for line in file if (row := json.loads(line))["split"] == "train"]
    train.sort(key=lambda row: hashlib.sha256(row["id"].encode()).hexdigest())
    used = set()
    samples = []
    for row in train:
        if row["group"] in used:
            continue
        used.add(row["group"])
        samples.append((row["id"], {note["name"]: note["text"] for note in row["notes"]}))
        if len(samples) == count:
            break
    return samples


def encode(v):
    return json.dumps(v, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def percentile(samples, p):
    values = sorted(samples)
    return round(values[(len(values) * p + 99) // 100 - 1] * 1000, 3)


def recording(predictor, texts):
    return predictor.predict({name: source_lines(text) for name, text in texts.items()})


def proof(predictor, samples):
    results = hashlib.sha256()
    semantics = hashlib.sha256()
    for ident, texts in samples:
        value = recording(predictor, texts)
        results.update(encode([ident, value]) + b"\n")
        for kind in ("emissions", "proposals"):
            for row in value[kind]:
                row.pop("score", None)
        semantics.update(encode([ident, value]) + b"\n")
    return results.hexdigest(), semantics.hexdigest()


def cold(package, threads):
    code = (
        "import json,time,sys; start=time.perf_counter(); cpu=time.process_time(); "
        "from pilot import Predictor; p=Predictor(sys.argv[1],threads=int(sys.argv[2])); "
        "print(json.dumps([time.perf_counter()-start,time.process_time()-cpu]))"
    )
    wall = time.perf_counter()
    result = subprocess.check_output([sys.executable, "-B", "-c", code, str(package.resolve()), str(threads)],
                                     cwd=Path(__file__).resolve().parent,
                                     env={**os.environ, "HF_HUB_OFFLINE": "1", "TOKENIZERS_PARALLELISM": "false"})
    import_wall, cpu = json.loads(result)
    return {"whole_process_wall_ms": round(1000 * (time.perf_counter() - wall), 3),
            "model_import_and_load_wall_ms": round(1000 * import_wall, 3),
            "model_import_and_load_cpu_ms": round(1000 * cpu, 3)}


def runtime_bytes():
    """Installed dependency footprint for running neural inference in Python."""
    from packaging.requirements import Requirement
    from packaging.utils import canonicalize_name

    pending = {"onnxruntime", "tokenizers", "numpy"}
    result = {}
    while pending:
        name = pending.pop()
        dist = distribution(name)
        paths = [dist.locate_file(item) for item in dist.files]
        result[name] = {"version": dist.version, "bytes": sum(x.stat().st_size for x in paths if x.is_file())}
        for text in dist.requires or []:
            requirement = Requirement(text)
            dependency = canonicalize_name(requirement.name)
            if dependency not in result and (requirement.marker is None or requirement.marker.evaluate()):
                pending.add(dependency)
    return result


def compare_fp32(package, fp32, predictor, samples, threads):
    identical = 0
    counts = Counter()
    with tempfile.TemporaryDirectory() as scratch:
        temp = Path(scratch)
        (temp / "model.onnx").symlink_to(fp32.resolve())
        (temp / "vocab.txt").symlink_to((package / "vocab.txt").resolve())
        policy = dict(predictor.config)
        policy["files"] = {**policy["files"], "model.onnx": sha256(fp32)}
        (temp / "policy.json").write_bytes(encode(policy))
        alternative = Predictor(temp, threads=threads)
        for _, texts in samples:
            proposals = []
            for p in (predictor, alternative):
                output = recording(p, texts)
                proposals.append({(r["field"], r["note"], r["line_index"], r["start"], r["end"])
                                  for r in output["proposals"]})
            identical += proposals[0] == proposals[1]
            counts.update(quantized_only=len(proposals[0] - proposals[1]), fp32_only=len(proposals[1] - proposals[0]),
                          shared=len(proposals[0] & proposals[1]))
    return {"identical_recordings": identical, "recordings": len(samples), "proposal_counts": dict(counts)}


def main():
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("--package", type=Path, required=True)
    cli.add_argument("--v2", type=Path, required=True)
    cli.add_argument("--threads", type=int, default=1)
    cli.add_argument("--recordings", type=int, default=100)
    cli.add_argument("--repetitions", type=int, default=3)
    cli.add_argument("--output", type=Path, required=True)
    cli.add_argument("--check", type=Path)
    cli.add_argument("--fp32", type=Path)
    args = cli.parse_args()
    if min(args.threads, args.repetitions, args.recordings) < 1:
        cli.error("threads, repetitions, and recordings must be positive")
    samples = source_sample(args.v2, args.recordings)
    if len(samples) != args.recordings:
        cli.error("fewer training groups than requested",)
    predictor = Predictor(args.package, threads=args.threads)
    replay_sha, semantic_sha = proof(predictor, samples)
    result = {
        "prediction_replay_sha256": replay_sha, "prediction_semantics_sha256": semantic_sha,
        "policy_sha256": sha256(args.package / "policy.json"),
        "pilot_py_sha256": sha256(Path(__file__).with_name("pilot.py")),
        "training_sample_sha256": hashlib.sha256(encode(samples)).hexdigest(),
        "package_bytes": sum(p.stat().st_size for p in args.package.iterdir()),
        "recordings": len(samples), "repetitions": args.repetitions, "threads": args.threads,
    }
    if args.check:
        expected = json.loads(args.check.read_text())
        for key in ("prediction_replay_sha256", "prediction_semantics_sha256", "policy_sha256",
                    "pilot_py_sha256", "training_sample_sha256", "package_bytes", "threads"):
            if result[key] != expected[key]:
                raise SystemExit(f"frozen replay mismatch: {key}")
    else:
        cpu_samples = []
        wall_samples = []
        for _ in range(args.repetitions):
            for _, texts in samples:
                start = time.perf_counter()
                cpu = time.process_time()
                recording(predictor, texts)
                cpu_samples.append(time.process_time() - cpu)
                wall_samples.append(time.perf_counter() - start)
        for name, timing in (("warm_cpu_ms", cpu_samples), ("warm_wall_ms", wall_samples)):
            result[name] = {f"p{p}": percentile(timing, p) for p in (50, 95, 99)}
        if args.fp32:
            result["quant_vs_fp32"] = compare_fp32(args.package, args.fp32, predictor, samples, args.threads)
        result["cold"] = [cold(args.package, args.threads) for _ in range(3)]
        result["platform"] = platform.platform()
        result["cpu"] = (subprocess.check_output(["sysctl", "-n", "machdep.cpu.brand_string"], text=True).strip()
                         if sys.platform == "darwin" else platform.processor())
        result["logical_cores"] = os.cpu_count()
        result["python"] = platform.python_version()
        result["runtime_distributions"] = runtime_bytes()
        result["runtime_distributions_bytes"] = sum(x["bytes"] for x in result["runtime_distributions"].values())
    with args.output.open("x") as file:
        json.dump(result, file, sort_keys=True, indent=2)
        file.write("\n")


if __name__ == "__main__":
    main()
