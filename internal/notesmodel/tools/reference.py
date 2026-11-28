"""Export exact line-span reference predictions for private native parity tests."""
import argparse
import json
from pathlib import Path
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("pilot", "packages", "cases", "output"):
        parser.add_argument(name, type=Path)
    args = parser.parse_args()
    sys.path.insert(0, str(args.pilot))
    from pilot import Predictor, source_lines

    models = [Predictor(args.packages / variant) for variant in ("lora", "full")]
    with args.cases.open() as source, args.output.open("x") as output:
        for line in source:
            case = json.loads(line)
            notes = {note["name"]: source_lines(note["text"]) for note in case["notes"]}
            variants = []
            for model in models:
                variants.append([
                    dict(field=span["field"], note=span["note"], line=span["line_index"],
                         start=span["start"], end=span["end"])
                    for span in model.scored(notes)
                    if span["score"] >= model.config["thresholds"][span["field"]]
                ])
            output.write(json.dumps(dict(id=case["id"], notes=notes, variants=variants),
                                    ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
