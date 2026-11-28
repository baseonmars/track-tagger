"""Replay a sealed source cohort with a single unchanged application scan path."""
import argparse
from collections import Counter, defaultdict
from functools import lru_cache
import hashlib
import json
from pathlib import Path
import random
import subprocess
import sys

CATALOG_COMMIT = "f8bd76b931fa828cfb9156a2d9a8f29a3aa3cb14"
STUDY_COMMIT = "1848decbea38a428a7e95e62c428cf7ffb1e6a73"
CONTEXT = "experiments/context"
CONTEXT_FREEZE = "results/context-model-freeze.json"
FIELDS = ("artist", "date", "venue", "city")
BACKGROUND = {"blank", "other", "technical_provenance"}
REVIEWED = {"agent-reviewed", "adjudicated"}
FLAGS = ("conflict", "uncertainty", "mixed_scope")


def rows(path):
    with path.open(encoding="utf-8") as stream:
        return [json.loads(line) for line in stream if line.strip()]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def jsonl(data):
    return "".join(json.dumps(row, ensure_ascii=False) + "\n" for row in data)


def run_catalog(binary, cases):
    proc = subprocess.run([binary], input=jsonl(cases), text=True,
                          capture_output=True, encoding="utf-8", check=True)
    values = [json.loads(line) for line in proc.stdout.split("\n") if line]
    if [row["id"] for row in values] != [case["id"] for case in cases]:
        raise ValueError("application scan returned different recording membership/order")
    return {row["id"]: row for row in values}


def indexed(data, key):
    result = {key(row): row for row in data}
    if len(result) != len(data):
        raise ValueError("input contains duplicate references")
    return result


def sha_identity(path, expected):
    if sha(path) != expected:
        raise ValueError(f"input {path.name} differs from frozen SHA-256")


def verify_inputs(cases, recording_rows, line_rows, source_hash, source_lines):
    index = indexed(cases, lambda row: row["id"])
    gold = indexed(recording_rows, lambda row: row["id"])
    labels = indexed(line_rows, lambda row: (row["id"], row["note"], row["line_index"]))
    if set(gold) != set(index):
        raise ValueError("recording gold and source membership differ")
    expected = set()
    notes = {}
    for case_id, case in index.items():
        digest = source_hash(case)
        recording = gold[case_id]
        if digest != recording["source_input_sha256"]:
            raise ValueError(f"recording source hash differs for {case_id}")
        notes[case_id] = {note["name"]: source_lines(note["text"]) for note in case["notes"]}
        if len(notes[case_id]) != len(case["notes"]):
            raise ValueError(f"duplicate source notes for {case_id}")
        for name, source in notes[case_id].items():
            for i, line in enumerate(source):
                key = case_id, name, i
                expected.add(key)
                item = labels[key]
                if (item["line"] != line or item["source_input_sha256"] != digest
                        or item["group"] != case["group"]):
                    raise ValueError(f"line source binding differs for {case_id}")
    if expected != set(labels):
        raise ValueError("line gold and source line membership differ")
    return gold, labels, notes


def rate(success, total):
    return success / total if total else None


def exact(metadata, gold):
    return all(" ".join(metadata.get(role, "").split()) ==
               " ".join(gold.get(role, "").split()) for role in FIELDS)


def bootstrap(groups, seed=42):
    samples = list(groups.values())
    if not samples:
        return None
    rng = random.Random(seed)
    result = []
    for _ in range(10_000):
        drawn = rng.choices(samples, k=len(samples))
        result.append(sum(sum(group) for group in drawn) / sum(map(len, drawn)))
    result.sort()
    return [result[249], result[9749]]


def normalize_prediction(case_id, output, notes, baseline, parse_date):
    assigned = indexed(output["lines"], lambda row: (row["note"], row["line_index"]))
    for (note, line_index), line in assigned.items():
        if note not in notes or type(line_index) is not int or not 0 <= line_index < len(notes[note]):
            raise ValueError(f"unknown line emission for {case_id}")
        if not set(line["roles"]) <= set(FIELDS):
            raise ValueError(f"unknown pilot role for {case_id}")
    accepted = []
    indexed(output["proposals"], lambda row: row["field"])
    for proposal in output["proposals"]:
        start, end = proposal["start"], proposal["end"]
        note, line_index = proposal["note"], proposal["line_index"]
        role, quote = proposal["field"], proposal["quote"]
        item = assigned.get((note, line_index), {})
        if role not in item.get("roles", []):
            raise ValueError(f"proposal does not reference same-role emission for {case_id}")
        line = notes[note][line_index]
        if type(start) is not int or type(end) is not int or not 0 <= start < end <= len(line) or line[start:end] != quote:
            raise ValueError(f"non-exact source span for {case_id}")
        value = parse_date(quote) if role == "date" else quote
        if not value or baseline.get("ready") is not False or "metadata" not in baseline:
            continue
        if " ".join(value.split()) != " ".join(baseline["metadata"].get(role, "").split()):
            accepted.append({**proposal, "value": value})
    return {key: set(row["roles"]) for key, row in assigned.items()}, accepted


def assess_proposal(proposal, label, metadata):
    role = proposal["field"]
    if label["role_disputed"] or label["label_status"] not in REVIEWED:
        return "unknown_line_role"
    if any(label[flag] for flag in FLAGS):
        return "unsafe_source_scope"
    if role not in label["roles"]:
        return "wrong_line_role"
    if " ".join(proposal["value"].split()) != " ".join(metadata.get(role, "").split()):
        return "wrong_recording_value"
    references = [span for span in label["spans"] if role in span["roles"]]
    if not references:
        return "matches_role_and_value_no_annotated_offsets"
    if any(span["start"] == proposal["start"] and span["end"] == proposal["end"]
           for span in references):
        return "matches_role_value_and_annotated_offsets"
    return "matches_role_and_value_different_annotated_offsets"


def score(cases, gold, labels, baseline, assigned, proposals, failed, interval, compared):
    fields = {role: Counter() for role in FIELDS}
    marked = defaultdict(set)
    background = Counter()
    affected = set()
    coverage = Counter(source_lines=len(labels))
    for key, label in labels.items():
        case_id = key[0]
        predicted = assigned.get(key, set())
        nonblank = bool(label["line"].strip())
        coverage["nonblank_source_lines"] += nonblank
        coverage["runtime_error_lines"] += case_id in failed
        coverage["emission_lines"] += bool(predicted)
        if label["role_disputed"] or label["label_status"] not in REVIEWED:
            coverage["unknown_role_lines"] += 1
            continue
        coverage["reviewed_nonblank_lines"] += nonblank
        expected = set(label["roles"])
        if expected and expected <= BACKGROUND:
            background["lines"] += 1
            if predicted:
                background["fp"] += 1
                affected.add(case_id)
        for role in expected & set(FIELDS):
            fields[role]["tp" if role in predicted else "fn"] += 1
        for role in predicted - expected:
            fields[role]["fp"] += 1
        for role in predicted:
            marked[role].add(case_id)

    exact_counts = Counter()
    proposal_counts = Counter()
    per_role = defaultdict(Counter)
    families = defaultdict(list)
    audit = []
    for case in cases:
        case_id = case["id"]
        reference = gold[case_id]
        proposals_for_case = proposals.get(case_id, [])
        exact_counts["emitting_recordings"] += bool(proposals_for_case)
        reviewed = reference["label_status"] in REVIEWED
        for proposal in proposals_for_case:
            label = labels[(case_id, proposal["note"], proposal["line_index"])]
            reason = (assess_proposal(proposal, label, reference["metadata"])
                      if reviewed else "unknown_recording_reference")
            proposal_counts[reason] += 1
            per_role[proposal["field"]][reason] += 1
            audit.append({"id": case_id, "source_input_sha256": reference["source_input_sha256"],
                          "status": reason, **proposal})
        if not reviewed:
            continue
        exact_counts["reviewed_recordings"] += 1
        in_cohort = reference["review_required"]
        exact_counts["review_cohort"] += in_cohort
        result, comparison = baseline[case_id], compared.get(case_id, [])
        metadata = result.get("metadata")
        if metadata is None:
            if in_cohort:
                families[case["group"]].append(0)
            continue
        suggested = metadata | {p["field"]: p["value"] for p in proposals_for_case}
        existing = metadata | {p["field"]: p["value"] for p in comparison}
        ref = reference["metadata"]
        original_exact, suggested_exact = exact(metadata, ref), exact(suggested, ref)
        exact_counts["all_metadata_exact"] += suggested_exact
        exact_counts["underlying_metadata_mapping_readiness_exact"] += (
            original_exact and result["mappings"] == reference["mappings"] and result["ready"] == (not in_cohort))
        exact_counts["underlying_reference_required_reviews_accepted"] += result["ready"] and in_cohort
        for role in FIELDS:
            original_correct = " ".join(metadata.get(role, "").split()) == " ".join(ref.get(role, "").split())
            new_correct = " ".join(suggested.get(role, "").split()) == " ".join(ref.get(role, "").split())
            exact_counts["fields_repaired"] += new_correct and not original_correct
            exact_counts["fields_degraded"] += original_correct and not new_correct
        if in_cohort:
            exact_counts["review_cohort_exact"] += suggested_exact
            exact_counts["review_cohort_gains"] += suggested_exact and not original_exact
            exact_counts["review_cohort_losses"] += original_exact and not suggested_exact
            families[case["group"]].append(int(suggested_exact) - int(exact(existing, ref)))

    roles = {}
    for role, counts in fields.items():
        tp, fp, fn = (counts[key] for key in ("tp", "fp", "fn"))
        roles[role] = dict(tp=tp, fp=fp, fn=fn, precision=rate(tp, tp + fp),
                           recall=rate(tp, tp + fn), precision_ci95=interval(tp, tp + fp),
                           recall_ci95=interval(tp, tp + fn), emitting_recordings=len(marked[role]))
    known_total = background["lines"]
    return {
        "recordings": dict(exact_counts),
        "requested_recordings": len(cases),
        "prediction_failures": len(failed),
        "eligible_recording_reference_fraction": rate(exact_counts["reviewed_recordings"], len(cases)),
        "proposal_count": sum(proposal_counts.values()),
        "proposal_assessment": dict(proposal_counts),
        "proposal_assessment_by_role": {role: dict(per_role[role]) for role in FIELDS},
        "roles": roles,
        "background": dict(lines=known_total, fp=background["fp"], rate=rate(background["fp"], known_total),
                           ci95=interval(background["fp"], known_total), affected_recordings=len(affected)),
        "coverage": {**coverage,
                     "reviewed_nonblank_fraction": rate(coverage["reviewed_nonblank_lines"], coverage["nonblank_source_lines"]),
                     "role_abstention_lines": len(labels) - coverage["runtime_error_lines"] - coverage["emission_lines"],
                     "proposal_abstention_recordings": len(cases) - len(failed) - exact_counts["emitting_recordings"]},
        "review_cohort_exact_ci95": interval(exact_counts["review_cohort_exact"], exact_counts["review_cohort"]),
        "paired_review_cohort_improvement": rate(sum(sum(g) for g in families.values()), exact_counts["review_cohort"]),
        "paired_review_cohort_improvement_ci95": bootstrap(families),
    }, audit


def git_head(path):
    return subprocess.check_output(["git", "-C", path, "rev-parse", "HEAD"], text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=("validation", "test", "fresh"))
    for name in ("cases", "gold", "lines", "catalog", "scorer", "model", "date-catalog",
                 "context", "study", "output"):
        parser.add_argument(f"--{name}", type=Path, required=True)
    parser.add_argument("--audit", type=Path, help="Private source quotes and identifiers")
    parser.add_argument("--seal", type=Path)
    parser.add_argument("--package", action="append", default=[], metavar="NAME=DIRECTORY")
    args = parser.parse_args()
    app = Path(__file__).resolve().parents[2]
    # A research-only descendant has the same application source as the measured baseline.
    app_changes = subprocess.run(["git", "-C", app, "diff", "--quiet", CATALOG_COMMIT, "--",
                                  "internal", "go.mod", "go.sum"], check=False).returncode
    if app_changes or git_head(args.study) != STUDY_COMMIT:
        raise ValueError("application or study checkout differs from pinned commit")
    if args.output.exists() or (args.audit and args.audit.exists()):
        raise FileExistsError("choose new output paths")
    input_hashes = {name: sha(getattr(args, name)) for name in ("cases", "gold", "lines")}
    if args.phase == "fresh":
        if args.seal is None:
            raise ValueError("fresh inference requires the independent source/reference seal")
        seal = json.loads(args.seal.read_bytes())
        if seal["input_sha256"] != input_hashes or not seal["independent_source_review_complete"]:
            raise ValueError("fresh sources or references differ from the reviewed seal")
        from execution import bindings
        packages = {name: Path(path) for name, path in (arg.split("=", 1) for arg in args.package)}
        actual = bindings(Path(__file__).resolve().parent, packages, args.catalog, args.context,
                          args.scorer, args.model, args.date_catalog)
        frozen_path = Path(__file__).with_name("results") / "execution-freeze.json"
        if sha(frozen_path) != seal["execution_sha256"] or actual != json.loads(frozen_path.read_bytes()):
            raise ValueError("fresh inference execution differs from frozen candidate")
        input_hashes["seal"] = sha(args.seal)
    else:
        known = json.loads(Path(__file__).with_name("retrospective-inputs.json").read_bytes())
        if known[args.phase] != input_hashes:
            raise ValueError("retrospective source or references differ from pinned inputs")

    context = args.context / CONTEXT
    freeze_path = context / CONTEXT_FREEZE
    sha_identity(freeze_path, "21cbdc3ba6e82bd9dc8eb1921bc4a8dba435f3beddfbdbe85e26984ad4debe4f")
    freeze = json.loads(freeze_path.read_bytes())
    binding = freeze["binding"]
    for path, key in ((args.model, "model"), (args.scorer, "scorer"), (args.date_catalog, "date_catalog"),
                      (context / "candidate.py", "candidate"), (context / "score.py", "scoring")):
        sha_identity(path, binding[key + "_sha256"])

    sys.path[:0] = [str(args.study.resolve()), str(context.resolve())]
    import candidate
    from intervals import interval
    from score import local_scores, runtime_scores
    from experiments.parser.study_v2_runner import source_hash, source_lines

    cases = [case for case in rows(args.cases) if args.phase == "fresh" or case["split"] == args.phase]
    if not cases:
        raise ValueError("empty source membership")
    gold, labels, notes = verify_inputs(cases, rows(args.gold), rows(args.lines), source_hash, source_lines)

    baseline = run_catalog(args.catalog, cases)
    replay = run_catalog(args.catalog, cases)
    errors = [case_id for case_id, row in baseline.items() if "error" in row]
    matched = sum("error" not in row and row == replay[case_id] for case_id, row in baseline.items())
    equal_failures = sum(baseline[case_id] == replay[case_id] for case_id in errors)
    report = {
        "phase": args.phase,
        "pilot_source_sha256": sha(Path(__file__).with_name("pilot.py")),
        "input_sha256": input_hashes,
        "context_freeze_sha256": sha(freeze_path),
        "catalog_commit": CATALOG_COMMIT,
        "catalog_adapter_sha256": sha(Path(__file__).with_name("catalog") / "main.go"),
        "catalog_binary_sha256": sha(args.catalog),
        "evaluator_sha256": sha(Path(__file__)),
        "catalog_parity": {
            "successful_equal": matched,
            "shared_equal_failures": equal_failures,
            "underlying_application_failures": len(errors),
            "underlying_application_failures_by_recording_count": {
                str(count): sum(baseline[case_id]["error"] == f"expected one recording, got {count}"
                                for case_id in errors) for count in range(4)
            },
            "strict_parity_pass": matched == len(cases),
            "preview_only": True,
            "writer_invocations": 0,
        },
    }

    @lru_cache(maxsize=None)
    def parse_date(value):
        run = subprocess.run([args.date_catalog, "--date"], input=json.dumps(value) + "\n",
                             text=True, encoding="utf-8", capture_output=True, check=True)
        result = json.loads(run.stdout)
        return result["date"] if not result["review_reasons"] else ""

    scored = local_scores(cases, runtime_scores(cases, args.scorer, args.model))
    select = freeze["selected"]
    limit = select["header_limit"]

    def context_predict(case):
        choice = candidate.predict(case, scored[case["id"]], select["thresholds"], limit)
        fixed = baseline[case["id"]]
        return {"lines": [{"note": key[0], "line_index": key[1], "roles": [role]}
                          for key, role in choice.items()],
                "proposals": candidate.propose(case, choice, fixed, parse_date, limit)}

    from pilot import Predictor, control

    runners = {"current_rules": lambda case: {"lines": [], "proposals": []},
               "section_aware_classifier": context_predict,
               "deterministic_span_control": lambda case: control(notes[case["id"]])}
    packages = {}
    for arg in args.package:
        name, path = arg.split("=", 1)
        if name in runners:
            raise ValueError(f"duplicate arm {name}")
        directory = Path(path)
        predictor = Predictor(directory)
        files = [file for file in sorted(directory.iterdir()) if file.is_file()]
        packages[name] = {"file_sha256": {file.name: sha(file) for file in files},
                          "package_bytes": sum(file.stat().st_size for file in files),
                          "thresholds": predictor.config["thresholds"]}
        runners[name] = lambda case, p=predictor: p.predict(notes[case["id"]])
    report["packages"] = packages

    metrics, audit = {}, {}
    all_proposals = {}
    for name, predict in runners.items():
        assigned, proposals, failures = {}, {}, {}
        for case in cases:
            case_id = case["id"]
            try:
                prediction = predict(case)
                if prediction.get("error"):
                    raise RuntimeError(str(prediction["error"]))
                emissions, accepted = normalize_prediction(case_id, prediction, notes[case_id],
                                                           baseline[case_id], parse_date)
            except Exception as error:
                failures[case_id] = type(error).__name__
                continue
            assigned.update({(case_id, *key): roles for key, roles in emissions.items()})
            proposals[case_id] = accepted
        all_proposals[name] = proposals
        metrics[name], proposals_audit = score(cases, gold, labels, baseline, assigned, proposals,
                                               failures, interval, {})
        metrics[name]["prediction_errors_by_type"] = dict(Counter(failures.values()))
        audit[name] = {"prediction_failures": failures, "proposals": proposals_audit}
        if name not in ("current_rules", "section_aware_classifier", "deterministic_span_control"):
            competitors = ["section_aware_classifier", "deterministic_span_control"]
            if name != "head_only" and "head_only" in all_proposals:
                competitors.append("head_only")
            for competitor in competitors:
                difference, _ = score(cases, gold, labels, baseline, assigned, proposals,
                                      failures, interval, all_proposals[competitor])
                metrics[name][f"paired_review_cohort_vs_{competitor}"] = {
                    key: difference["paired_review_cohort_" + key] for key in ("improvement", "improvement_ci95")}
    report["arms"] = metrics
    with args.output.open("x") as stream:
        json.dump(report, stream, indent=2)
        stream.write("\n")
    if args.audit:
        with args.audit.open("x") as stream:
            json.dump({"catalog_errors": {case_id: baseline[case_id]["error"] for case_id in errors},
                       "arms": audit}, stream, indent=2, ensure_ascii=False)
            stream.write("\n")


if __name__ == "__main__":
    main()
