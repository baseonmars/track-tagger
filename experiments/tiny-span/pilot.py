"""Offline, extractive BERT pilot and its source-only control."""
import datetime
import hashlib
import json
from pathlib import Path
import re

FIELDS = ("artist", "date", "venue", "city")
HEADER_LIMIT = 10
MAX_FOCUS = 40
MAX_CONTEXT = 10
MAX_SPAN = 24
LABEL = re.compile(r"\b(artist|performer|band|concert date|date|venue|city)\s*:\s*", re.I)
ALIASES = {"band": "artist", "performer": "artist", "concert date": "date"}
SECTION = re.compile(
    r"^(?:set\s+(?:\d+|[ivx]+|one|two)\b|(?:disc|disk|cd)\s+\d+\b"
    r"|\d{1,3}\s*[.)_-]\s*\S|d\d+t\d+|(?:source|lineage|track\s*list|set\s*list|"
    r"notes|credits|personnel|musicians|recording|transfer|taper|checksums?)\s*:)", re.I
)
UNSAFE = re.compile(
    r"\b(?:last time|previous(?:ly)?|formerly|guest|featur\w*|taper|lineage|transfer\w*"
    r"|upload\w*|seeded|source|mastered|recorded by|taped by|mixed by|another show"
    r"|probably|possibly|perhaps|unsure|uncertain|unknown|or maybe)\b"
    r"|\bw/|[?]|\.(?:flac|shn|wav|mp3|txt|nfo|md5|ffp)\b", re.I
)
NUMERIC_DATE = re.compile(r"^(\d{1,4})([-/.])(\d{1,2})\2(\d{1,4})$")
WEEKDAY = re.compile(r"^(?:(?:Mon(?:day)?|Tue(?:sday)?|Wed(?:nesday)?|Thu(?:rsday)?|"
                     r"Fri(?:day)?|Sat(?:urday)?|Sun(?:day)?),?\s+)", re.I)


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def normalize_date(quote):
    value = WEEKDAY.sub("", quote.strip())
    match = NUMERIC_DATE.fullmatch(value)
    if match:
        first, _, center, last = match.groups()
        if len(first) == 4:
            year, month, day = int(first), int(center), int(last)
        elif len(last) == 4:
            year, month, day = int(last), int(first), int(center)
        else:
            return ""
        try:
            return datetime.date(year, month, day).isoformat() if 1900 <= year <= 2099 else ""
        except ValueError:
            return ""
    value = re.sub(r"(?<=\d)(?:st|nd|rd|th)\b", "", value, flags=re.I).replace(",", " ")
    for template in ("%B %d %Y", "%b %d %Y", "%d %B %Y", "%d %b %Y"):
        try:
            date = datetime.datetime.strptime(value, template).date()
        except ValueError:
            continue
        if 1900 <= date.year <= 2099:
            return date.isoformat()
    return ""


def source_lines(text):
    text = text.removeprefix("\ufeff").replace("\r\n", "\n").replace("\r", "\n")
    if not text:
        return []
    rows = text.split("\n")
    return rows[:-1] if text.endswith("\n") else rows


def header(notes):
    for note, lines in sorted(notes.items()):
        count = 0
        for index, line in enumerate(lines):
            value = line.strip()
            if not value:
                if count:
                    break
                continue
            count += 1
            if count > HEADER_LIMIT or (SECTION.match(value) and not normalize_date(value)):
                break
            if not UNSAFE.search(value):
                yield note, index, line


def encode(tokenizer, lines, index):
    focus = tokenizer.encode(lines[index], add_special_tokens=False)
    if not focus.ids or len(focus.ids) > MAX_FOCUS:
        return None
    prev = tokenizer.encode(lines[index - 1] if index else "", add_special_tokens=False)
    following = tokenizer.encode(lines[index + 1] if index + 1 < len(lines) else "",
                                 add_special_tokens=False)
    cls = tokenizer.token_to_id("[CLS]")
    sep = tokenizer.token_to_id("[SEP]")
    ids = [cls, *focus.ids, sep, *prev.ids[-MAX_CONTEXT:], sep, *following.ids[:MAX_CONTEXT], sep]
    return ids, [(0, 0), *focus.offsets]


def make_proposal(note, index, line, role, start, end, score):
    quote = line[start:end]
    if not quote or not any(char.isalnum() for char in quote) or LABEL.search(quote):
        return None
    declarations = list(LABEL.finditer(line, 0, start))
    if declarations:
        declared = declarations[-1].group(1).lower()
        if ALIASES.get(declared, declared) != role:
            return None
    if re.match(r"^[\s:|,;]", quote) or re.search(r"[|;,\s]$", quote):
        return None
    value = normalize_date(quote) if role == "date" else quote
    return dict(field=role, note=note, line_index=index, start=start, end=end,
                quote=quote, value=value, score=score) if value else None


def assemble(notes, emissions):
    roles = {}
    choices = {field: {} for field in FIELDS}
    for emission in emissions:
        role = emission["field"]
        key = (emission["note"], emission["line_index"])
        roles.setdefault(key, set()).add(role)
        existing = choices[role].get(emission["value"])
        if not existing or existing["score"] < emission["score"]:
            choices[role][emission["value"]] = emission
    return {
        "lines": [
            {"note": note, "line_index": index, "roles": sorted(roles.get((note, index), []))}
            for note, lines in sorted(notes.items()) for index in range(len(lines))
        ],
        "proposals": [next(iter(values.values())) for values in choices.values() if len(values) == 1],
        "emissions": emissions,
    }


def control(notes):
    emissions = []
    for note, index, line in header(notes):
        labels = list(LABEL.finditer(line))
        for position, label in enumerate(labels):
            role = label[1].lower()
            end = labels[position + 1].start() if position + 1 < len(labels) else len(line)
            value = line[label.end():end].strip(" \t;|")
            if value:
                start = line.index(value, label.end(), end)
                proposal = make_proposal(note, index, line, ALIASES.get(role, role),
                                         start, start + len(value), 1.0)
                if proposal:
                    emissions.append(proposal)
        if not labels and normalize_date(line):
            start = len(line) - len(line.lstrip())
            proposal = make_proposal(note, index, line, "date", start, len(line.rstrip()), 1.0)
            if proposal:
                emissions.append(proposal)
    return assemble(notes, emissions)


class Predictor:
    def __init__(self, package, threads=1):
        import onnxruntime as ort
        from tokenizers import BertWordPieceTokenizer

        package = Path(package)
        self.config = json.loads((package / "policy.json").read_text())
        for name, digest in self.config["files"].items():
            if sha256(package / name) != digest:
                raise ValueError(f"package hash mismatch: {name}")
        self.tokenizer = BertWordPieceTokenizer(str(package / "vocab.txt"), lowercase=True)
        options = ort.SessionOptions()
        options.intra_op_num_threads = threads
        options.inter_op_num_threads = 1
        self.session = ort.InferenceSession(str(package / "model.onnx"), sess_options=options,
                                            providers=["CPUExecutionProvider"])

    def scored(self, notes):
        import numpy as np

        locations = []
        for note, index, line in header(notes):
            encoded = encode(self.tokenizer, notes[note], index)
            if encoded:
                locations.append((note, index, line, encoded))
        if not locations:
            return []
        ids = np.zeros((len(locations), max(len(row[3][0]) for row in locations)), dtype=np.int64)
        mask = np.zeros_like(ids)
        for position, (_, _, _, (tokens, _)) in enumerate(locations):
            ids[position, :len(tokens)] = tokens
            mask[position, :len(tokens)] = 1
        (logits,) = self.session.run(None, {"ids": ids, "mask": mask})
        emissions = []
        for (note, index, line, (_, offsets)), line_scores in zip(locations, logits):
            count = len(offsets) - 1
            first, last = np.triu_indices(count)
            allowed = last - first < MAX_SPAN
            first, last = first[allowed] + 1, last[allowed] + 1
            for i, role in enumerate(FIELDS):
                s, e = line_scores[:, 2 * i:2 * i + 2].T
                scores = s[first] + e[last] - s[0] - e[0]
                best = int(scores.argmax())
                proposal = make_proposal(note, index, line, role, offsets[first[best]][0],
                                         offsets[last[best]][1], round(float(scores[best]), 6))
                if proposal:
                    emissions.append(proposal)
        return emissions

    def predict(self, notes):
        thresholds = self.config["thresholds"]
        emissions = [row for row in self.scored(notes)
                     if row["field"] in thresholds and row["score"] >= thresholds[row["field"]]]
        return assemble(notes, emissions)
