"""Summarize the separately recorded independent judgments before related-source evaluation."""
import argparse
from collections import Counter
from pathlib import Path
import hashlib
import json

def read(path):
    with path.open(encoding="utf-8") as stream:
        rows = [json.loads(line) for line in stream if line.strip()]
    index = {row["id"]: row for row in rows}
    if len(rows) != len(index):
        raise ValueError(f"duplicate recording ID in {path.name}")
    return index
def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("cohort", "primary", "secondary", "adjudications"):
        parser.add_argument(name, type=Path)
    args = parser.parse_args()
    cases = read(args.cohort)
    a = read(args.primary)
    b = read(args.secondary)
    c = read(args.adjudications)
    common=sorted(cases.keys() & a.keys() & b.keys() & c.keys())
    fixed={"metadata_values":Counter(dict.fromkeys(("artist","date","venue","city"),0)),
           "metadata_evidence":Counter(dict.fromkeys(("artist","date","venue","city"),0)),
           "scope_flags":Counter(dict.fromkeys(("mixed_scope","uncertainty","conflict"),0))}
    counts=Counter()
    changes=Counter()
    for case_id in common:
        original,reading,joint=a[case_id],b[case_id],c[case_id]
        assert original["packet_sha256"]==reading["packet_sha256"]==joint["packet_sha256"]
        for role in ("artist","date","venue","city"):
            fixed["metadata_values"][role]+=original["metadata"][role]!=reading["metadata"][role]
            fixed["metadata_evidence"][role]+=original["evidence"].get(role)!=reading["evidence"].get(role)
            changes["primary_"+role]+=original["metadata"][role]!=joint["metadata"][role]
        for key in ("review_required","titles","mappings"):
            counts["recordings_different_"+key]+=original[key]!=reading[key]
            changes["primary_"+key]+=original[key]!=joint[key]
        assert len(original["lines"]) == len(reading["lines"]) == len(joint["lines"])
        counts["lines"]+=len(original["lines"])
        for aa,bb in zip(original["lines"],reading["lines"]):
            roles_a,roles_b=set(aa["roles"]),set(bb["roles"])
            counts["line_role_disagreements"]+=roles_a!=roles_b
            counts["metadata_role_disagreements"]+=bool((roles_a^roles_b) & set(fixed["metadata_values"]))
            for flag in ("mixed_scope","uncertainty","conflict"):
                fixed["scope_flags"][flag]+=bool(aa.get(flag))!=bool(bb.get(flag))
    report={"considered_recordings":len(common),"cohort_sha256":sha(args.cohort),
            "method":"Independent primary and second readings of original notes and supplied filenames. Cross-review sample uses the preassigned ID hash bucket and retained final-v3 membership.",
            "uses_candidate_predictions":False,"pre_adjudication_disagreements":{**fixed,**counts},
            "adjudication_changes_from_primary":dict(changes)}
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
