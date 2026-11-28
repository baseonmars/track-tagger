"""Assemble two-reader source adjudications in the sealed holdout order."""
import argparse
from collections import Counter
from hashlib import sha256
import json
from pathlib import Path

from inventory import indexed, load, require
from blind_sources import write_jsonl

# Earlier rows take precedence when the same provisional source had multiple reader pairs.
ADJUDICATION_FILES = (
    "adjudicated-early-eval.jsonl",
    "adjudicated-setup-eval.jsonl",
    "adjudicated-eval-encoder.jsonl",
    "reviews-reconciled-setup-encoder.jsonl",
    "cross-adjudicated-eval.jsonl",
    "reviews-final-encoder.jsonl",
    "adjudicated-b2-root.jsonl",
)


def named_proof(reader, name, expected=None):
    return {"reader": reader, "file": name, "sha256": expected}


def proof_specs(row, source, encoder_proofs):
    if "independent_proof" in row:
        return row["independent_proof"]
    if source == "adjudicated-setup-eval.jsonl":
        return [named_proof("setup", "reviews-setup.jsonl"), named_proof("eval", "independent-eval.jsonl")]
    if source == "cross-adjudicated-eval.jsonl":
        return [named_proof("root", "reviews-root.jsonl"), named_proof("eval", "cross-eval.jsonl")]
    manifest = encoder_proofs[source][row["id"]]
    primary_reader = "setup" if source == "reviews-reconciled-setup-encoder.jsonl" else "encoder"
    secondary_reader = "encoder" if primary_reader == "setup" else "root"
    return [
        named_proof(primary_reader, manifest["independent_primary"], manifest["independent_primary_sha256"]),
        named_proof(secondary_reader, manifest["second_read"], manifest.get("second_read_sha256")),
    ]


def original_bytes(path, expected):
    data = path.read_bytes()
    if expected is None or sha256(data).hexdigest() == expected:
        return data
    # An independent JSONL author may have appended more judgments after handoff.
    # Recover the exact earlier file prefix and check its original byte digest.
    if path.suffix == ".jsonl":
        prefix = bytearray()
        state = sha256()
        with path.open("rb") as source:
            for line in source:
                prefix.extend(line)
                state.update(line)
                if state.hexdigest() == expected:
                    return bytes(prefix)
    raise ValueError(f"could not recover pinned proof from {path}")


def assemble(inputs, source_dir, destination):
    require(
        not destination.resolve().is_relative_to(Path(__file__).resolve().parents[2]),
        "store private references outside this repository",
    )
    cases = indexed(inputs)
    encoder_proofs = {
        "reviews-reconciled-setup-encoder.jsonl": load(source_dir / "proof-reconciled-setup-encoder.json"),
        "reviews-final-encoder.jsonl": load(source_dir / "proof-final-encoder.json"),
    }
    rows = {}
    counts = Counter()
    evidence = {}
    for source in ADJUDICATION_FILES:
        for identifier, row in indexed(source_dir / source).items():
            if identifier not in cases or identifier in rows:
                continue
            proofs = []
            for spec in proof_specs(row, source, encoder_proofs):
                name = spec["file"]
                require(Path(name).name == name and name not in (".", ".."), "unsafe proof filename")
                key = name, spec["sha256"]
                if key not in evidence:
                    data = original_bytes(source_dir / name, spec["sha256"])
                    checksum = sha256(data).hexdigest()
                    packet_hashes = None
                    if name.endswith(".jsonl"):
                        packet_hashes = {
                            item["id"]: item["packet_sha256"]
                            for line in data.split(b"\n") if line.strip()
                            for item in (json.loads(line),)
                        }
                    evidence[key] = data, checksum, packet_hashes
                _, checksum, packet_hashes = evidence[key]
                if packet_hashes is not None:
                    require(packet_hashes.get(identifier) == row["packet_sha256"], f"{identifier} missing from {name}")
                proofs.append(named_proof(spec["reader"], f"{checksum[:12]}--{name}", checksum))
            require(len({proof["reader"] for proof in proofs}) >= 2, f"{identifier} lacks independent readings")
            rows[identifier] = row | {"independent_proof": proofs}
            counts[source] += 1

    destination.mkdir(parents=True, exist_ok=False)
    proof_dir = destination / "reference-proof"
    proof_dir.mkdir()
    for (name, _), (data, checksum, _) in evidence.items():
        path = proof_dir / f"{checksum[:12]}--{name}"
        if not path.exists():
            path.write_bytes(data)
    readings = destination / "adjudicated.jsonl"
    write_jsonl(readings, (rows[identifier] for identifier in cases if identifier in rows))
    print(json.dumps({"sources": len(cases), "adjudicated": len(rows), "contributions": dict(counts)}, sort_keys=True))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for argument in ("inputs", "source_dir", "destination"):
        parser.add_argument(argument, type=Path)
    assemble(**vars(parser.parse_args()))
