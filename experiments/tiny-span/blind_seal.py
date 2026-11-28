"""Validate independent source readings and freeze their original-note evidence."""
import argparse
import hashlib
import json
from pathlib import Path

from blind_author import line_index, packet


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def read(path):
    with path.open(encoding="utf-8") as stream:
        return [json.loads(line) for line in stream]


def save(path, rows):
    with path.open("x") as stream:
        for row in rows:
            stream.write(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n")


def seal(inputs, readings, proof_dir, output, study):
    import sys
    sys.path.insert(0, str(study))
    from experiments.parser.study_v2.labels import source_hash, ROLES
    cases = read(inputs)
    reviews = {row["id"]: row for row in read(readings)}
    output.mkdir(exist_ok=False)
    recordings, labels = [], []
    proof_hashes = {}
    used = set()
    for case in cases:
        cid = case["id"]
        evidence = line_index(case)
        ref = reviews.get(cid)
        identity = {"id": cid, "source_input_sha256": source_hash(case)}
        rec = dict(identity, label_status="unresolved")
        if ref:
            used.add(cid)
            packet_hash = hashlib.sha256(packet(case).encode()).hexdigest()
            if ref["packet_sha256"] != packet_hash or not ref["adjudication"]:
                raise ValueError(f"{cid}: evidence differs or adjudication missing")
            proofs = ref["independent_proof"]
            if len({p["reader"] for p in proofs}) < 2:
                raise ValueError(f"{cid}: two independent source readings required")
            for proof in proofs:
                path = proof_dir / proof["file"]
                if path.resolve().parent != proof_dir.resolve():
                    raise ValueError("unexpected review path")
                proof_hashes[path.name] = digest(path)
                if proof_hashes[path.name] != proof["sha256"]:
                    raise ValueError("independent review artifact changed")
            if set(ref["metadata"]) != {"artist", "date", "venue", "city"} or type(ref["review_required"]) is not bool:
                raise ValueError("incomplete recording decision")
            rec["field_evidence"] = {}
            for role, entry in ref["evidence"].items():
                if role not in {"artist", "date", "venue", "city"}:
                    raise ValueError("unexpected field")
                n, quote = entry
                if not quote or not (0 <= n < len(evidence)) or quote not in evidence[n][2]:
                    raise ValueError(f"{cid}: metadata evidence mismatch {entry}")
                name, index, text = evidence[n]
                start = text.index(quote)
                rec["field_evidence"][role] = dict(note=name, line_index=index, quote=quote,
                                                    start=start, end=start + len(quote))
            if set(ref["evidence"]) != {k for k,v in ref["metadata"].items() if v}:
                raise ValueError("missing field evidence")
            if set(ref["mappings"]) != set(case["files"]):
                raise ValueError("missing reviewed file")
            rec.update({k: ref[k] for k in ("metadata", "review_required", "titles", "mappings",
                                             "adjudication", "evidence")})
            for path, join in ref["mappings"].items():
                key = f"{join['disc']}:{join['number']}"
                if join["title"]:
                    if min(join["disc"], join["number"]) < 1 or ref["titles"].get(key) != join["title"]:
                        raise ValueError(f"{cid}: title/ID mismatch for {path}")
                elif join["disc"] or join["number"]:
                    raise ValueError(f"{cid}: partly populated unknown join")
            rec["label_status"] = "adjudicated"
        recordings.append(rec)
        for n, (name, index, text) in enumerate(evidence):
            item = ref["lines"][n] if ref else {}
            roles = item.get("roles", [] if text.strip() else ["blank"])
            if not set(roles) <= set(ROLES) or (roles and ("blank" in roles) != (not text.strip())):
                raise ValueError(f"{cid}: bad role assignment {n}")
            for span in item.get("spans", []):
                if not 0 <= span["start"] < span["end"] <= len(text) or text[span["start"]:span["end"]] != span["quote"]:
                    raise ValueError(f"{cid}: line evidence mismatch")
            labels.append(dict(identity, group=case["group"], note=name, line_index=index, line=text, roles=roles, spans=item.get("spans", []),
                               role_disputed=not bool(roles), label_status="adjudicated" if roles else "unresolved",
                               **{flag: bool(item.get(flag)) for flag in ("mixed_scope","uncertainty","conflict")}))
        if ref and len(ref["lines"]) != len(evidence):
            raise ValueError("review coverage length changed")
    save(output / "recording-gold.jsonl", recordings)
    save(output / "line-gold.jsonl", labels)
    freeze = {"inputs": digest(inputs), "source_readings": digest(readings), "source_proof_hashes": proof_hashes,
              "recordings": len(recordings), "adjudicated":len(used),
              "reviewed_nonblank": sum(bool(r["line"].strip()) and not r["role_disputed"] for r in labels),
              "nonblank":sum(bool(r["line"].strip()) for r in labels),
              "review_cohort": [r["id"] for r in recordings if r.get("review_required")],
              "gold": {p.name:digest(p) for p in output.glob("*.jsonl")}}
    freeze["input_sha256"] = {"cases":digest(inputs),"gold":freeze["gold"]["recording-gold.jsonl"],
                             "lines":freeze["gold"]["line-gold.jsonl"]}
    freeze["independent_source_review_complete"] = len(used) == len(cases)
    freeze["execution_sha256"] = digest(Path(__file__).with_name("results") / "execution-freeze.json")
    (output / "seal.json").write_text(json.dumps(freeze,indent=2)+"\n")


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    for arg in ("inputs","readings","proof_dir","output","study"):
        p.add_argument(arg,type=Path)
    seal(**vars(p.parse_args()))
