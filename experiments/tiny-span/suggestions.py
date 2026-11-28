"""Score shown source suggestions and field-level changes against the sealed fresh references."""
import argparse
from collections import Counter
import json
from pathlib import Path

from evaluate import FIELDS, REVIEWED, assess_proposal, indexed, rows, run_catalog, sha
from gates import estimate


MATCH = "matches_role"
EXACT_OFFSET = "matches_role_value_and_annotated_offsets"
NO_OFFSETS = "matches_role_and_value_no_annotated_offsets"


def normalized(value):
    return " ".join(value.split())


def matches(proposed, reference):
    return bool(normalized(reference)) and normalized(proposed) == normalized(reference)


def baseline_counts(baseline, references, field):
    counts = Counter()
    for case_id, scan in baseline.items():
        expected = references.get(case_id, {}).get(field, "")
        if not normalized(expected):
            counts["unknown_reference"] += 1
            continue
        counts["known_reference"] += 1
        if "error" in scan:
            counts["known_behind_catalog_failure"] += 1
            continue
        metadata = scan["metadata"]
        counts["known_successful_scan"] += 1
        if matches(metadata.get(field, ""), expected):
            counts["baseline_correct"] += 1
        else:
            counts["baseline_wrong_or_missing"] += 1
            counts["baseline_missing"] += not normalized(metadata.get(field, ""))
            counts["baseline_wrong_or_missing_review_eligible"] += scan.get("ready") is False
    return counts


def assess_arm(audit_arm, metrics, baseline, references, labels):
    proposals = indexed(audit_arm["proposals"], lambda item: (item["id"], item["field"]))
    if len(proposals) != metrics["proposal_count"]:
        raise ValueError("private suggestion count differs from evaluator")
    failures = audit_arm["prediction_failures"]
    if len(failures) != metrics["prediction_failures"] or failures.keys() - baseline.keys():
        raise ValueError("private prediction failures differ from evaluator")
    shown = {field: Counter() for field in FIELDS}
    statuses = {field: Counter() for field in FIELDS}
    failure_support = {field: Counter() for field in FIELDS}
    for case_id in failures:
        scan = baseline[case_id]
        if "error" in scan:
            continue
        for field, counts in failure_support.items():
            expected = references.get(case_id, {}).get(field, "")
            if normalized(expected) and not matches(scan["metadata"].get(field, ""), expected):
                counts["wrong_or_missing_behind_prediction_failure"] += 1
                counts["review_eligible_behind_prediction_failure"] += scan.get("ready") is False
    for (case_id, field), proposal in proposals.items():
        if case_id in failures:
            raise ValueError("proposal attached to failed prediction")
        scan = baseline[case_id]
        if "error" in scan or scan.get("ready") is not False:
            raise ValueError("proposal attached to ineligible catalog output")
        label = labels[(case_id, proposal["note"], proposal["line_index"])]
        if (label["source_input_sha256"] != proposal["source_input_sha256"] or
                not 0 <= proposal["start"] < proposal["end"] <= len(label["line"]) or
                label["line"][proposal["start"]:proposal["end"]] != proposal["quote"]):
            raise ValueError("proposal lacks an exact source slice")
        reference = references.get(case_id)
        expected = (reference or {}).get(field, "")
        status = (assess_proposal(proposal, label, reference)
                  if reference is not None else "unknown_recording_reference")
        if status != proposal["status"]:
            raise ValueError("private proposal assessment differs from sealed sources")
        statuses[field][status] += 1
        before = scan["metadata"].get(field, "")
        after = proposal["value"]
        if normalized(before) == normalized(after):
            raise ValueError("unchanged field was shown as a suggestion")
        counts = shown[field]
        counts["shown"] += 1
        counts["unknown_reference_shown"] += not normalized(expected)
        counts["exact_value"] += matches(after, expected)
        counts["exact_value_and_role"] += status.startswith(MATCH)
        counts["exact_offsets"] += status == EXACT_OFFSET
        counts["matching_role_value_without_offset_reference"] += status == NO_OFFSETS
        if normalized(expected):
            if matches(before, expected):
                counts["baseline_correct_shown"] += 1
                counts["baseline_correct_degraded"] += not matches(after, expected)
            else:
                counts["field_repaired"] += matches(after, expected)
                counts["field_repaired_with_current_role"] += status.startswith(MATCH)
    if statuses != metrics["proposal_assessment_by_role"]:
        raise ValueError("private assessments by field differ from evaluator")
    support = {field: baseline_counts(baseline, references, field) for field in FIELDS}
    shown["all"] = sum(shown.values(), Counter())
    support["all"] = sum(support.values(), Counter())
    failure_support["all"] = sum(failure_support.values(), Counter())
    return {field: field_report(shown[field], support[field], failure_support[field])
            for field in ("all", *FIELDS)}


def field_report(shown, base, failures):
    total = shown["shown"]
    with_offsets = shown["exact_value_and_role"] - shown["matching_role_value_without_offset_reference"]
    wrong = base["baseline_wrong_or_missing"]
    eligible = base["baseline_wrong_or_missing_review_eligible"]
    return {
        "shown_suggestions": total,
        "shown_with_unknown_reference": shown["unknown_reference_shown"],
        "exact_value_among_shown": estimate(shown["exact_value"], total),
        "joint_current_role_and_exact_value_among_shown": estimate(shown["exact_value_and_role"], total),
        "joint_exact_annotated_offsets_among_shown": estimate(shown["exact_offsets"], total),
        "joint_exact_offsets_when_role_value_match_and_annotated": estimate(shown["exact_offsets"], with_offsets),
        "matching_role_value_without_offset_reference": shown["matching_role_value_without_offset_reference"],
        "reference_unknown_fields": base["unknown_reference"],
        "known_fields_behind_catalog_failure": base["known_behind_catalog_failure"],
        "known_fields_on_successful_scans": base["known_successful_scan"],
        "baseline_missing_known_on_successful_scans": base["baseline_missing"],
        "baseline_incorrect_nonempty_known_on_successful_scans": wrong - base["baseline_missing"],
        "wrong_or_missing_fields_behind_prediction_failure": failures["wrong_or_missing_behind_prediction_failure"],
        "review_eligible_fields_behind_prediction_failure": failures["review_eligible_behind_prediction_failure"],
        "exact_field_repair_recall_successful_scans": estimate(shown["field_repaired"], wrong),
        "exact_field_repair_recall_review_eligible": estimate(shown["field_repaired"], eligible),
        "joint_field_repair_recall_review_eligible": estimate(shown["field_repaired_with_current_role"], eligible),
        "degradation_among_correct_baseline_fields": estimate(shown["baseline_correct_degraded"],
                                                            base["baseline_correct"]),
        "degradation_among_shown_overwrites_of_correct_fields": estimate(shown["baseline_correct_degraded"],
                                                                       shown["baseline_correct_shown"]),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("result", "audit", "cases", "gold", "lines", "catalog", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    result = json.loads(args.result.read_bytes())
    hashes = {key: sha(getattr(args, key)) for key in ("cases", "gold", "lines")}
    if (result["phase"] != "fresh" or not result["input_sha256"].get("seal") or
            any(result["input_sha256"][key] != digest for key, digest in hashes.items()) or
            result["catalog_binary_sha256"] != sha(args.catalog)):
        raise ValueError("inputs differ from the sealed evaluation")
    audit = json.loads(args.audit.read_bytes())
    cases = rows(args.cases)
    gold = indexed(rows(args.gold), lambda item: item["id"])
    labels = indexed(rows(args.lines), lambda item: (item["id"], item["note"], item["line_index"]))
    baseline = run_catalog(args.catalog, cases)
    errors = {key: value["error"] for key, value in baseline.items() if "error" in value}
    if (errors != audit["catalog_errors"] or len(errors) !=
            result["catalog_parity"]["underlying_application_failures"]):
        raise ValueError("catalog replay failures differ from sealed evaluation")
    if gold.keys() != baseline.keys() or audit["arms"].keys() != result["arms"].keys():
        raise ValueError("cohort or arm membership differs")
    references = {k: r["metadata"] for k, r in gold.items() if r["label_status"] in REVIEWED}
    report = {
        "evaluation_sha256": sha(args.result),
        "private_audit_sha256": sha(args.audit),
        "recordings": len(cases),
        "recordings_with_reviewed_reference": len(references),
        "successful_catalog_scans": len(cases) - len(errors),
        "failed_catalog_scans": len(errors),
        "failed_catalog_scans_with_reviewed_reference": len(errors.keys() & references.keys()),
        "confidence_method": "95% Wilson intervals, descriptive and unadjusted for recording-family dependence",
        "arms": {
            arm: assess_arm(audit_arm, result["arms"][arm], baseline, references, labels)
            for arm, audit_arm in audit["arms"].items()
        },
    }
    with args.output.open("x") as stream:
        json.dump(report, stream, indent=2)
        stream.write("\n")


if __name__ == "__main__":
    main()
