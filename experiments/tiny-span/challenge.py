"""Run hand-labeled, paired stress examples separately from recording-level evaluation."""
import argparse
import json
from pathlib import Path

from pilot import Predictor, control, source_lines

# Each pair shares a value and differs in whether it is attributable to the performance.
# These fictional sources are used only for stress testing after model selection.
PAIRS = [
    ("upload", "date", "2022-10-19", "Show date: 2022-10-19", "Upload date: 2022-10-19"),
    ("filename", "date", "2022-10-19", "2022-10-19", "2022-10-19_s01t02.flac"),
    ("historical", "date", "2022-10-19", "Date: 2022-10-19", "The group last played here on 2022-10-19."),
    ("personnel", "artist", "Oriel Stone", "Artist: Oriel Stone", "Personnel:\nOriel Stone - fiddle"),
    ("itinerary", "city", "Port Kestrel, ME", "City: Port Kestrel, ME", "Tour itinerary:\nPort Kestrel, ME"),
    ("combined", "city", "Port Kestrel, ME", "Oriel Ballroom  Port Kestrel, ME Setlist 10/19/22",
     "Previous engagement: Oriel Ballroom  Port Kestrel, ME Setlist 10/19/22"),
    ("conflict", "venue", "Oriel Ballroom", "Venue: Oriel Ballroom", "Venue: Oriel Ballroom\n\nCorrection: venue listed elsewhere as Gannet Hall."),
    ("conflicting notes", "venue", "Oriel Ballroom", "Venue: Oriel Ballroom", "Venue: Oriel Ballroom"),
    ("multiline venue", "venue", "Oriel Ballroom\nNorth Stage", "Venue: Oriel Ballroom\nNorth Stage",
     "Personnel:\nOriel Ballroom\nNorth Stage"),
]


def main(package, output, old_fresh):
    predictor = Predictor(package)
    result = {"synthetic": [], "previously_scored_example": {}}
    for kind, field, value, positive, incidental in PAIRS:
        for applicable, text in [(True, positive), (False, incidental)]:
            notes = {"show.txt": source_lines(text)}
            if kind == "conflicting notes" and not applicable:
                notes["second.txt"] = ["Venue: Gannet Hall"]
            start = text.index(value) if applicable else None
            expected = {"note":"show.txt","start":start,"end":start+len(value),"quote":value} if applicable else None
            row = {"family":kind,"current_evidence":expected,"field":field,"arms":{}}
            for arm, predict in (("control",control),("lora",predictor.predict)):
                emitted = [p for p in predict(notes)["proposals"] if p["field"] == field]
                correct = not emitted if expected is None else False
                if applicable and len(emitted) == 1:
                    p = emitted[0]
                    absolute = sum(1+len(s) for s in notes[p["note"]][:p["line_index"]])+p["start"]
                    correct = p["note"] == expected["note"] and absolute == start and p["quote"] == value
                row["arms"][arm] = {"correct":correct,"proposals":emitted}
            result["synthetic"].append(row)
    with old_fresh.open() as stream:
        case = next(c for c in map(json.loads,stream) if c["id"] == "PG2016-10-22.NMA")
    notes = {n["name"]:source_lines(n["text"]) for n in case["notes"]}
    predicted = predictor.predict(notes)
    result["previously_scored_example"] = {"id":case["id"],"diagnostic_only":True,
        "city_proposals": [p for p in predicted["proposals"] if p["field"]=="city"]}
    with output.open("x") as stream:
        json.dump(result,stream,indent=2,ensure_ascii=False)
        stream.write("\n")


if __name__ == "__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("package","output","old_fresh"):
        parser.add_argument(name,type=Path)
    main(**vars(parser.parse_args()))
