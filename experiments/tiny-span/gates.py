"""Calculate recording and suggestion gates against an independently sealed cohort."""
import argparse
import hashlib
import json
from math import sqrt
from pathlib import Path


def read(path):
    with path.open() as stream:
        return [json.loads(row) for row in stream]


def estimate(correct, total):
    if not total:
        return {"correct":correct, "total":total, "rate":None, "wilson95":None}
    p = correct / total
    z = 1.959963984540054
    center = p + z*z / (2 * total)
    width = z * sqrt(p*(1-p)/total + z*z/(4*total*total))
    divisor = 1 + z*z/total
    return {"correct":correct,"total":total,"rate":p,
            "wilson95":[max(0,(center-width)/divisor), min(1,(center+width)/divisor)]}


def summary(result, cases, gold):
    for key,path in (("cases",cases),("gold",gold)):
        if hashlib.sha256(path.read_bytes()).hexdigest() != result["input_sha256"][key]:
            raise ValueError("source/gold digest differs from evaluator inputs")
    refs={r["id"]:r for r in read(gold) if r["label_status"] in ("agent-reviewed","adjudicated")}
    requested=[c for c in read(cases) if result["phase"] == "fresh" or c["split"] == result["phase"]]
    complete=0
    resolved=0
    for case in requested:
        r=refs.get(case["id"])
        if not r or not isinstance(r.get("titles"),dict) or set(r.get("mappings",{}))!=set(case["files"]):
            continue
        complete+=1
        resolved+=all(m["number"] and m["disc"] and m["title"] for m in r["mappings"].values())
    out={"requested":len(requested),"complete_track_inventories":complete,"all_joins_resolved":resolved,"arms":{}}
    for name, arm in result["arms"].items():
        measures={}
        for label,counts in {"all":arm["proposal_assessment"], **arm["proposal_assessment_by_role"]}.items():
            matched=sum(n for k,n in counts.items() if k.startswith("matches_role"))
            support=sum(counts.values())
            measures[label]={"exact_value_and_current_role":estimate(matched,support),
                             "also_matching_explicit_offsets":counts.get("matches_role_value_and_annotated_offsets",0)}
        flags={"complete_adjudicated_references": complete == len(refs) and complete>=180,
               "reviewed_nonblank_coverage":arm["coverage"]["reviewed_nonblank_fraction"]>=.95,
               "per_role_precision_and_support":all(r["precision"] is not None and r["precision"]>=.95 and
                                                    r["emitting_recordings"]>=20 for r in arm["roles"].values()),
               "background_false_positive_rate":arm["background"]["rate"] is not None and arm["background"]["rate"]<=.01,
               "review_cohort_improvement":(arm["paired_review_cohort_improvement"] or 0)>=.05,
               "complete_candidate_outputs":not result["catalog_parity"]["underlying_application_failures"] and not arm["prediction_failures"],
               "strict_application_parity":result["catalog_parity"]["strict_parity_pass"],
               "all_shown_proposals_valid_source_slices":not arm["prediction_failures"],
               "all_shown_proposals_eligible_roles":not any(arm["proposal_assessment"].get(s,0) for s in
                   ("unknown_line_role","unsafe_source_scope","wrong_line_role","unknown_recording_reference")),
               "newly_accepted_recordings":0, "newly_accepted_material_errors":0}
        out["arms"][name]={"suggestions":measures,"gates":flags}
    return out


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("result","cases","gold","output"):
        parser.add_argument(name,type=Path)
    a=parser.parse_args()
    report=summary(json.loads(a.result.read_bytes()),a.cases,a.gold)
    with a.output.open("x") as stream:
        json.dump(report,stream,indent=2)
        stream.write("\n")
