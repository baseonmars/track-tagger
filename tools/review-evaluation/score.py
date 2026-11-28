"""Score app-visible evidence against the already-exposed, sealed source references."""
import argparse
from collections import Counter
from hashlib import sha256
import json
import random
from math import ceil, sqrt
from pathlib import Path, PurePosixPath

FIELDS = ("artist", "date", "venue", "city")
VERIFIED = {"adjudicated", "agent-reviewed"}


def index(path, key=lambda row: row["id"]):
    result = {}
    with path.open(encoding="utf-8") as stream:
        for raw in stream:
            row = json.loads(raw)
            identifier = key(row)
            if identifier in result:
                raise ValueError(f"duplicate record in {path}: {identifier}")
            result[identifier] = row
    return result


def digest(path):
    return sha256(path.read_bytes()).hexdigest()


def clean(value):
    return " ".join(value.split())


def exact(metadata, reference):
    return metadata is not None and all(
        clean(metadata[field]) == clean(reference[field]) for field in FIELDS
    )


def wilson(correct, shown):
    if not shown:
        return None
    z = 1.959963984540054
    fraction = correct / shown
    scale = 1 + z * z / shown
    middle = (fraction + z * z / (2 * shown)) / scale
    radius = z * sqrt(fraction * (1 - fraction) / shown + z * z / (4 * shown * shown)) / scale
    return [max(0, middle - radius), min(1, middle + radius)]


def rate(numerator, denominator):
    return {
        "numerator": numerator,
        "denominator": denominator,
        "fraction": numerator / denominator if denominator else None,
        "wilson_95": wilson(numerator, denominator),
    }


def paired_interval(family_deltas):
    if not family_deltas:
        return None
    values = list(family_deltas.values())
    rng = random.Random(20261128)
    samples = []
    for _ in range(5000):
        sampled = rng.choices(values, k=len(values))
        samples.append(sum(sum(group) for group in sampled) / sum(len(group) for group in sampled))
    samples.sort()
    return [samples[125], samples[4874]]


def assess(identifier, prefix, proposal, labels, reference):
    field = proposal["field"].lower()
    name = str(PurePosixPath(prefix, proposal["note"]))
    label = labels.get((identifier, name, proposal["line"]))
    if label is None or label["line"][proposal["start"]:proposal["end"]] != proposal["quote"]:
        return "invalid_source_span"
    if label["role_disputed"] or label["label_status"] not in VERIFIED:
        return "unresolved_source_role"
    if any(label[flag] for flag in ("conflict", "mixed_scope", "uncertainty")):
        return "unsafe_source_scope"
    if field not in label["roles"]:
        return "wrong_source_role"
    if clean(proposal["value"]) != clean(reference[field]):
        return "wrong_recording_value"
    return "correct"


def policy_score(app, cases, gold, labels, prior):
    counts = Counter()
    role_counts = {field: Counter() for field in FIELDS}
    field_counts = {field: Counter() for field in FIELDS}
    shown_correct_lines = {field: set() for field in FIELDS}
    shown_lines = set()
    family_deltas = {}
    statuses = Counter()
    failures = Counter()
    wall_ms = []
    for identifier, case in cases.items():
        row, ref = app[identifier], gold[identifier]
        cohort = ref["review_required"]
        counts["source_review_cohort"] += cohort
        if cohort:
            family_deltas.setdefault(case["group"], []).append(0)
        if row.get("excluded"):
            failures[row["excluded"]] += 1
            counts["exclusions_matching_prior"] += bool(prior[identifier].get("error"))
            continue
        if "metadata" not in row:
            failures["app validation failed"] += 1
            continue
        counts["validated"] += 1
        counts["partial_source_membership"] += (
            row["scoped_note_count"] != len(case["notes"]) or row["scoped_audio_count"] != len(case["files"])
        )
        counts["eligible"] += row["eligible"]
        if row["eligible"]:
            wall_ms.append(row["request_wall_ns"] / 1e6)
        counts["request_errors"] += bool(row.get("error"))
        statuses[row["status"]] += 1
        counts["equal_metadata_plan"] += row["metadata_plan_equal"]
        counts["equal_browser_record"] += row["browser_record_equal"]
        counts["equal_fixture_bytes"] += row["fixture_bytes_equal"]
        counts["source_review_cases_ready"] += cohort and row["status"] == "ready"
        base = exact(row["metadata"], ref["metadata"])
        overlay = row["metadata"].copy()
        for proposal in row["proposals"] or ():
            role = proposal["field"].lower()
            outcome = assess(identifier, row["source_prefix"], proposal, labels, ref["metadata"])
            line_key = (identifier, str(PurePosixPath(row["source_prefix"], proposal["note"])), proposal["line"])
            shown_lines.add(line_key)
            if outcome == "correct":
                shown_correct_lines[role].add(line_key)
            role_counts[role][outcome] += 1
            field_counts[role]["shown_exact_value"] += clean(proposal["value"]) == clean(ref["metadata"][role])
            role_counts[role]["draft_action_available"] += role in ("artist", "date") or proposal.get("albumSafe", False)
            overlay[role] = proposal["value"]
        after = exact(overlay, ref["metadata"])
        counts["base_exact4"] += base
        counts["shown_overlay_exact4"] += after
        counts["cohort_base_exact4"] += cohort and base
        counts["cohort_shown_overlay_exact4"] += cohort and after
        counts["cohort_gains"] += cohort and after and not base
        counts["cohort_losses"] += cohort and base and not after
        if cohort:
            family_deltas[case["group"]][-1] = int(after) - int(base)
        for role in FIELDS:
            value = clean(ref["metadata"][role])
            originally_correct = clean(row["metadata"][role]) == value
            corrected = clean(overlay[role]) == value
            field_counts[role]["field_repairs"] += corrected and not originally_correct
            field_counts[role]["field_degradations"] += originally_correct and not corrected
            field_counts[role]["correctly_unknown_degradations"] += not value and originally_correct and not corrected
            if row["eligible"] and value and not originally_correct:
                field_counts[role]["eligible_known_repair_opportunities"] += 1
                field_counts[role]["eligible_known_repairs"] += corrected
        counts["proposal_recordings"] += bool(row["proposals"])
    safe_source_roles = Counter()
    background_lines = set()
    for key, label in labels.items():
        roles = set(label["roles"])
        if label["role_disputed"] or label["label_status"] not in VERIFIED:
            continue
        if roles and roles <= {"other", "technical_provenance", "blank"}:
            background_lines.add(key)
        if not any(label[flag] for flag in ("conflict", "mixed_scope", "uncertainty")):
            safe_source_roles.update(roles.intersection(FIELDS))

    role_metrics = {}
    joint_correct = joint_shown = 0
    for role, outcomes in role_counts.items():
        actions = outcomes.pop("draft_action_available", 0)
        correct = outcomes["correct"]
        shown = sum(outcomes.values())
        joint_correct += correct
        joint_shown += shown
        fields = field_counts[role]
        role_metrics[role] = {
            "correct": correct, "shown": shown, "draft_action_available": actions,
            "exact_value_precision": rate(fields["shown_exact_value"], shown),
            "joint_recall_over_safe_source_role_lines": rate(len(shown_correct_lines[role]), safe_source_roles[role]),
            "eligible_known_field_recovery_recall": rate(fields["eligible_known_repairs"], fields["eligible_known_repair_opportunities"]),
            "field_repairs": fields["field_repairs"],
            "field_degradations": fields["field_degradations"],
            "correctly_unknown_degradations": fields["correctly_unknown_degradations"],
            "eligible_recording_abstentions": counts["eligible"] - shown - counts["request_errors"],
            "joint_precision": correct / shown if shown else None,
            "wilson_95": wilson(correct, shown), "outcomes": dict(outcomes),
        }
    return {
        **dict(counts),
        "requested": len(cases),
        "statuses": dict(statuses),
        "exclusions": dict(failures),
        "shown_role_metrics": role_metrics,
        "overall_joint_correct": joint_correct,
        "overall_joint_shown": joint_shown,
        "overall_joint_precision": joint_correct / joint_shown if joint_shown else None,
        "overall_wilson_95": wilson(joint_correct, joint_shown),
        "overall_exact_value_precision": rate(sum(v["shown_exact_value"] for v in field_counts.values()), joint_shown),
        "field_repairs": sum(v["field_repairs"] for v in field_counts.values()),
        "field_degradations": sum(v["field_degradations"] for v in field_counts.values()),
        "correctly_unknown_degradations": sum(v["correctly_unknown_degradations"] for v in field_counts.values()),
        "eligible_known_field_recovery_recall": rate(
            sum(v["eligible_known_repairs"] for v in field_counts.values()),
            sum(v["eligible_known_repair_opportunities"] for v in field_counts.values()),
        ),
        "eligible_recording_abstentions": counts["eligible"] - counts["proposal_recordings"] - counts["request_errors"],
        "background_false_positive_lines": rate(len(shown_lines & background_lines), len(background_lines)),
        "cohort_exact4_baseline_wilson_95": wilson(counts["cohort_base_exact4"], counts["source_review_cohort"]),
        "cohort_exact4_overlay_wilson_95": wilson(counts["cohort_shown_overlay_exact4"], counts["source_review_cohort"]),
        "cohort_exact4_paired_family_bootstrap_95": paired_interval(family_deltas),
        "eligible_request_wall_p95_ms": sorted(wall_ms)[ceil(0.95 * len(wall_ms)) - 1] if wall_ms else None,
    }


def sealed_inputs(cases, seal_path):
    source = seal_path.parent
    seal = json.loads(seal_path.read_bytes())
    expected = {
        "cases": cases,
        "gold": source / "recording-gold.jsonl",
        "lines": source / "line-gold.jsonl",
    }
    for name, path in expected.items():
        if digest(path) != seal["input_sha256"][name]:
            raise ValueError(f"reference seal mismatch for {name}")
    return expected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for arg in ("cases", "seal", "prior", "us", "strict", "pack", "binary", "fixture", "prior_binary"):
        parser.add_argument(arg, type=Path)
    args = parser.parse_args()
    expected = sealed_inputs(args.cases, args.seal)
    cases, gold, prior, us, strict = (
        index(path) for path in (args.cases, expected["gold"], args.prior, args.us, args.strict)
    )
    if not (cases.keys() == gold.keys() == prior.keys() == us.keys() == strict.keys()):
        raise ValueError("the expected case inventories differ")
    if any(row["label_status"] not in VERIFIED for row in gold.values()):
        raise ValueError("study lacks reviewed recording references")
    labels = index(expected["lines"], lambda row: (row["id"], row["note"], row["line_index"]))
    policy_difference = Counter()
    for identifier in cases:
        left, right = us[identifier], strict[identifier]
        policy_difference["baseline_date_changed"] += left.get("metadata", {}).get("date") != right.get("metadata", {}).get("date")
        policy_difference["readiness_changed"] += left.get("status") != right.get("status")
        policy_difference["eligibility_changed"] += left["eligible"] != right["eligible"]
    result = {
        "source_kind": "real notes with generated audio fixtures",
        "cases_sha256": digest(args.cases),
        "seal_sha256": digest(args.seal),
        "model_pack_sha256": digest(args.pack),
        "app_replay_binary_sha256": digest(args.binary),
        "prior_catalog_binary_sha256": digest(args.prior_binary),
        "synthetic_flac_sha256": digest(args.fixture),
        "prior_exclusions": sum(bool(row.get("error")) for row in prior.values()),
        "policy_difference": dict(policy_difference),
        "us_assumption_on": policy_score(us, cases, gold, labels, prior),
        "us_assumption_off": policy_score(strict, cases, gold, labels, prior),
    }
    print(json.dumps(result, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
