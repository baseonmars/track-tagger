"""Fit and compare tiny offline BERT span heads using only v2 train/validation."""
import argparse
from collections import Counter, defaultdict
import datetime
from importlib.metadata import version
import json
import os
from pathlib import Path
import random
import re
import shutil

import numpy as np
import onnx
from onnx import helper, numpy_helper
import torch
from torch import nn
import torch.nn.functional as F
from tokenizers import BertWordPieceTokenizer
from transformers import BertModel

from pilot import (FIELDS, HEADER_LIMIT, LABEL, ALIASES, Predictor, control, encode,
                   header, normalize_date, sha256, source_lines)

SEED = 21691
BASE_REVISION = "30b0a37ccaaa32f332884b96992754e246e48c5f"
BASE_FILES = {
    "config.json": "508e1f01aae55d73355cbdd82609be2f43ba5a0d3428837adbe56cf8391f8b39",
    "model.safetensors": "7fb69ad9f6866d8983183c930e33828f326470bf6ad8bbb2ad4ed957a92e9414",
    "vocab.txt": "07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3",
}
VARIANTS = ("head", "full", "lora")
THRESHOLDS = (0, 1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20)
BACKGROUND = {"other", "technical_provenance", "blank"}


def rows(path):
    with path.open() as file:
        return [json.loads(line) for line in file if line.strip()]


def dump(path, value):
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + "\n")


def synthetic(count, stress=False):
    """Generate counterfactual spans and decoys from hand-written fictional values."""
    rng = random.Random(SEED + int(stress))
    banks = (
        ("Cloudglass Trio", "Marble Comets", "Sweet Violet Band", "Red Poplar Assembly",
         "Cuatro Faroles", "James & Liora", "Écho Mécanique", "Rising Lake Quartet"),
        ("Riverline Theater", "Mooring Hall", "The Blue Vault", "Juniper Pavilion",
         "Le Petit Rivage", "Stage 3", "The Atlas Room", "Neon Bell Ballroom"),
        ("Moonside, RI", "Amber Falls, WI", "Port Lenox, WA", "South Bayou, LA",
         "Nuevo Puerto, México", "St. Céleste, QC", "Ashmere, UK", "Quartz City, NV"),
    )
    subset = slice(4, None) if stress else slice(0, 4)
    for i in range(count):
        artist, venue, city = (rng.choice(bank[subset]) for bank in banks)
        day = datetime.date(rng.randint(1961, 2026), rng.randint(1, 12), rng.randint(1, 28))
        date = rng.choice((day.isoformat(), day.strftime("%B %d, %Y"), day.strftime("%m/%d/%Y")))
        values = dict(zip(FIELDS, (artist, date, venue, city)))
        counterfactual = dict(values)
        counterfactual["artist"] = rng.choice([v for v in banks[0][subset] if v != artist])
        layouts = (
            ("{artist}", "{date}", "{venue}", "{city}"),
            ("{artist} / {date}", "{venue} - {city}"),
            ("Artist: {artist}", "Date: {date}", "Venue: {venue}", "City: {city}"),
            ("{artist}", "{venue}, {city}, {date}"),
            ("{date}", "{artist}", "{city}", "{venue}"),
            ("{artist}", "Venue: {venue} | City: {city}", "{date}"),
        ) if not stress else (
            ("{venue} • {city} / {date}", "{artist}"),
            ("{date} — {artist}", "{venue} @ {city}"),
            ("{artist}", "{date}", "{venue} :: {city}"),
            ("{artist} | {date} | {venue} | {city}",),
        )
        layout = rng.choice(layouts)
        lines = [line.format(**values) for line in layout]
        gold = [{role: (offset, offset + len(value)) for role, value in values.items()
                 if (offset := line.find(value)) >= 0} for line in lines]
        negative_sections = (
            ("Previously: {artist} at {venue}", "{city} probably?"),
            ("Source: {artist} 24/96", "Transfer: {venue}; {city}"),
            ("guest appearance by {artist}", "mixed by {artist} at {venue}"),
            ("01. {artist}", "02. {venue}", "03. {city}"),
            ("Other versions mention {venue}", "Notes: {artist} / {date}"),
        )
        distractors = [line.format(**counterfactual) for line in rng.choice(negative_sections)]
        if i % 5 == 0:
            lines = distractors + lines
            gold = [{} for _ in distractors] + gold
        elif i % 5 == 1:
            lines = distractors
            gold = [{} for _ in distractors]
        else:
            lines += distractors
            gold += [{} for _ in distractors]
        yield f"synthetic-{i}", {"synthetic.txt": lines}, {
            ("synthetic.txt", line_index): spans for line_index, spans in enumerate(gold)
        }


def weak_refs(row):
    if row["role_disputed"]:
        return None
    if row["uncertainty"] or row["mixed_scope"] or row["conflict"]:
        return {}
    line = row["line"]
    span_lists = defaultdict(list)
    for span in row["spans"]:
        for role in set(span["roles"]) & set(FIELDS):
            if line[span["start"]:span["end"]] == span["quote"]:
                span_lists[role].append((span["start"], span["end"]))
    if len(row["roles"]) == 1 and (role := row["roles"][0]) in FIELDS and role not in span_lists:
        start = len(line) - len(line.lstrip())
        end = len(line.rstrip())
        label = LABEL.match(line, start)
        if label and ALIASES.get(label[1].lower(), label[1].lower()) == role:
            start = label.end()
        if start < end and (role != "date" or normalize_date(line[start:end])):
            span_lists[role].append((start, end))
    return {role: spans[0] for role, spans in span_lists.items() if len(spans) == 1}


def labels(tokenizer, lines, index, spans):
    result = encode(tokenizer, lines, index)
    if not result:
        return None
    ids, offsets = result
    starts = {s: i for i, (s, _) in reversed(list(enumerate(offsets[1:], 1)))}
    ends = {e: i for i, (_, e) in enumerate(offsets[1:], 1)}
    targets = []
    for role in FIELDS:
        if role not in spans:
            targets.extend((0, 0))
        else:
            start, end = spans[role]
            first, last = starts.get(start, -100), ends.get(end, -100)
            targets.extend((first, last) if 0 < first <= last else (-100, -100))
    return ids, len(offsets), targets


def training(tokenizer, sources, train_lines, count):
    by_record = defaultdict(dict)
    for row in train_lines:
        by_record[row["id"]][(row["note"], row["line_index"])] = row
    real = []
    synthetic_rows = []
    for target, recordings in (
        (real, ((case["id"], case["notes"], None) for case in sources.values())),
        (synthetic_rows, synthetic(count)),
    ):
        for identifier, notes, spans in recordings:
            for note, index, line in header(notes):
                if spans is not None:
                    target_spans = spans[(note, index)]
                else:
                    row = by_record[identifier].get((note, index))
                    if not row or row["line"] != line:
                        continue
                    target_spans = weak_refs(row)
                if target_spans is None:
                    continue
                result = labels(tokenizer, notes[note], index, target_spans)
                if result:
                    target.append(result)
    return real, synthetic_rows


class LowRank(nn.Module):
    def __init__(self, linear):
        super().__init__()
        self.linear = linear
        self.down = nn.Parameter(torch.empty(4, linear.in_features))
        self.up = nn.Parameter(torch.zeros(linear.out_features, 4))
        nn.init.normal_(self.down, std=0.02)

    def forward(self, value):
        return self.linear(value) + F.linear(F.linear(value, self.down), self.up) * 2

    def merge(self):
        with torch.no_grad():
            self.linear.weight.add_(self.up @ self.down * 2)
        return self.linear


class SpanModel(nn.Module):
    def __init__(self, base, variant):
        super().__init__()
        self.encoder = BertModel.from_pretrained(base, add_pooling_layer=False,
                                                 attn_implementation="eager", local_files_only=True)
        self.head = nn.Linear(self.encoder.config.hidden_size, 2 * len(FIELDS))
        self.variant = variant
        if variant != "full":
            self.encoder.requires_grad_(False)
        if variant == "lora":
            for layer in self.encoder.encoder.layer:
                attention = layer.attention.self
                attention.query = LowRank(attention.query)
                attention.value = LowRank(attention.value)

    def forward(self, ids, mask):
        return self.head(self.encoder(input_ids=ids, attention_mask=mask).last_hidden_state)

    def merge(self):
        if self.variant == "lora":
            for layer in self.encoder.encoder.layer:
                attention = layer.attention.self
                attention.query = attention.query.merge()
                attention.value = attention.value.merge()


def fit(base, variant, samples, epochs):
    torch.manual_seed(SEED)
    randomizer = random.Random(SEED)
    model = SpanModel(base, variant)
    parameters = [parameter for parameter in model.parameters() if parameter.requires_grad]
    optimizer = torch.optim.AdamW(parameters, lr=0.00045 if variant == "full" else 0.003)
    losses = []
    for epoch in range(epochs):
        shuffled = list(samples)
        randomizer.shuffle(shuffled)
        batches = [shuffled[i:i + 32] for i in range(0, len(shuffled), 32)]
        floating_loss = 0.0
        model.train()
        if variant == "head":
            model.encoder.eval()
        for batch in batches:
            width = max(len(row[0]) for row in batch)
            ids = torch.zeros((len(batch), width), dtype=torch.long)
            mask = torch.zeros_like(ids)
            eligible = torch.zeros_like(ids, dtype=torch.bool)
            for position, (tokens, count, _) in enumerate(batch):
                ids[position, :len(tokens)] = torch.tensor(tokens)
                mask[position, :len(tokens)] = 1
                eligible[position, :count] = True
            target = torch.tensor([row[2] for row in batch])
            logits = model(ids, mask).transpose(1, 2).masked_fill(~eligible[:, None, :], -10000)
            loss = F.cross_entropy(logits.flatten(0, 1), target.flatten(), reduction="none")
            weights = torch.where(target.flatten() == 0, 0.35, 1.0)
            loss = (loss * weights).sum() / weights.sum()
            optimizer.zero_grad(set_to_none=True)
            loss.backward()
            torch.nn.utils.clip_grad_norm_(parameters, 1.0)
            optimizer.step()
            floating_loss += float(loss.detach())
        losses.append(round(floating_loss / len(batches), 6))
        print(variant, epoch + 1, losses[-1], flush=True)
    model.merge()
    return model.eval(), losses, sum(parameter.numel() for parameter in parameters)


def quantize_embeddings(src, dest):
    """Gather int8 word vectors before dequantizing only the selected rows."""
    model = onnx.load(src)
    names = {initializer.name for initializer in model.graph.initializer
             if tuple(initializer.dims) == (30522, 128)}
    if len(names) != 1:
        raise ValueError("expected a single BERT word embedding")
    name = names.pop()
    weight = next(tensor for tensor in model.graph.initializer if tensor.name == name)
    values = numpy_helper.to_array(weight)
    scale = np.maximum(np.max(np.abs(values), axis=1, keepdims=True) / 127, 1e-9)
    quantized = np.rint(values / scale).clip(-127, 127).astype(np.int8)
    model.graph.initializer.remove(weight)
    model.graph.initializer.extend((numpy_helper.from_array(quantized, name),
                                    numpy_helper.from_array(scale, "word_row_scale")))
    nodes = list(model.graph.node)
    gather = [node for node in nodes if node.op_type == "Gather" and node.input[0] == name]
    if len(gather) != 1:
        raise ValueError("expected a single lookup of the word embedding")
    gather = gather[0]
    index = nodes.index(gather)
    output = gather.output[0]
    gather.output[0] = "word_i8"
    nodes[index + 1:index + 1] = [
        helper.make_node("Gather", ["word_row_scale", gather.input[1]], ["word_scale"]),
        helper.make_node("Cast", ["word_i8"], ["word_fp32"], to=onnx.TensorProto.FLOAT),
        helper.make_node("Mul", ["word_fp32", "word_scale"], [output]),
    ]
    del model.graph.node[:]
    model.graph.node.extend(nodes)
    onnx.checker.check_model(model)
    onnx.save(model, dest)


def export(model, base, package):
    package.mkdir()
    ids = torch.ones((2, 24), dtype=torch.long)
    mask = torch.ones_like(ids)
    original = package.parent / (package.name + "-fp32.onnx")
    torch.onnx.export(model, (ids, mask), original, input_names=["ids", "mask"],
                      output_names=["logits"], opset_version=17, dynamo=False,
                      dynamic_axes={"ids": {0: "batch", 1: "sequence"},
                                    "mask": {0: "batch", 1: "sequence"},
                                    "logits": {0: "batch", 1: "sequence"}})
    quantize_embeddings(original, package / "model.onnx")
    shutil.copyfile(base / "vocab.txt", package / "vocab.txt")
    return original


def case_sources(v2, train_lines, validation_lines):
    sources = {"train": {}, "validation": {}}
    require = {"train": train_lines, "validation": validation_lines}
    for split, labels in require.items():
        if any(row["split"] != split for row in labels):
            raise ValueError(f"invalid label split: {split}")
    with (v2 / "replayed-cases.jsonl").open() as file:
        for line in file:
            data = json.loads(line)
            if data["split"] not in sources:
                continue
            data["notes"] = {note["name"]: source_lines(note["text"]) for note in data["notes"]}
            sources[data["split"]][data["id"]] = data
    t, v = sources.values()
    if t.keys() & v.keys() or {x["group"] for x in t.values()} & {x["group"] for x in v.values()}:
        raise ValueError("overlapping train and validation groups")
    for split, labels in require.items():
        if any(row["id"] not in sources[split] or
               row["group"] != sources[split][row["id"]]["group"] for row in labels):
            raise ValueError(f"label does not match source group: {split}")
    return t, v


def assessment(identifier, spans, label_rows, reference, emissions):
    collected = []
    for row in emissions:
        key = row["note"], row["line_index"]
        role = row["field"]
        label = label_rows.get(key)
        output = dict(row, id=identifier)
        if label is not None:
            eligible = not any(label[k] for k in ("role_disputed", "uncertainty", "mixed_scope", "conflict"))
            reviewed = label["label_status"] in {"agent-reviewed", "adjudicated"}
            role_correct = eligible and role in label["roles"]
            exact = (row["value"] == reference.get(role) and role_correct
                     and (role not in spans.get(key, {}) or
                          spans[key][role] == (row["start"], row["end"])))
            output.update(correct=bool(exact and reviewed), role_correct=bool(role_correct and reviewed),
                          background=bool(reviewed and not label["role_disputed"] and
                                          set(label["roles"]) <= BACKGROUND))
        elif reference is None:
            expected = spans.get(key, {})
            exact = expected.get(role) == (row["start"], row["end"])
            output.update(correct=exact, role_correct=role in expected, background=not expected)
        else:
            output.update(correct=False, role_correct=False, background=False)
        collected.append(output)
    return collected


def measurements(predictions, thresholds, counts, background_count):
    by_role = {}
    for role in FIELDS:
        emits = [r for r in predictions if r["field"] == role and r["score"] >= thresholds.get(role, float("inf"))]
        correct = sum(row["correct"] for row in emits)
        by_role[role] = dict(emitted=len(emits), correct=correct, role_correct=sum(row["role_correct"] for row in emits),
                             recordings=len({row["id"] for row in emits}),
                             precision=correct / len(emits) if emits else None,
                             supported_references=counts[role],
                             recall=correct / counts[role] if counts[role] else None)
    background_hits = {(r["id"], r["note"], r["line_index"]) for r in predictions
                       if r["background"] and r["score"] >= thresholds.get(r["field"], float("inf"))}
    return dict(roles=by_role, background_fp=len(background_hits), background=background_count)


def calibrate(predictions, counts, background):
    thresholds = {}
    for role in FIELDS:
        for cutoff in THRESHOLDS:
            arm = measurements(predictions, {**thresholds, role: cutoff}, counts, background)
            score = arm["roles"][role]
            if score["recordings"] >= 5 and score["precision"] >= .95 and arm["background_fp"] <= .01 * background:
                thresholds[role] = cutoff
                break
    return thresholds


def validation_metrics(f, validation, label_rows, recording_rows, calibrator=None):
    labels = defaultdict(dict)
    for row in label_rows:
        labels[row["id"]][(row["note"], row["line_index"])] = row
    recordings = {row["id"]: row for row in recording_rows}
    counts = Counter()
    background = 0
    predictions = []
    for identifier, source in validation.items():
        note_labels = labels[identifier]
        reference = recordings.get(identifier, {}).get("metadata", {})
        for label in note_labels.values():
            if label["role_disputed"] or label["label_status"] not in {"agent-reviewed", "adjudicated"}:
                continue
            background += set(label["roles"]) <= BACKGROUND
            if not any(label[k] for k in ("uncertainty", "mixed_scope", "conflict")):
                counts.update(role for role in label["roles"] if reference.get(role))
        spans = {key: weak_refs(label) for key, label in note_labels.items() if label["spans"] and
                 weak_refs(label)}
        predictions.extend(assessment(identifier, spans, note_labels, reference,
                                      f(source["notes"])))
    thresholds = calibrate(predictions, counts, background) if calibrator else {}
    return thresholds, {
        "recordings": len(validation), "groups": len({case["group"] for case in validation.values()}),
        "reviewed_lines": sum(len(note_rows) for note_rows in labels.values()),
        "at_zero": measurements(predictions, dict.fromkeys(FIELDS, 0), counts, background),
        "calibrated": measurements(predictions, thresholds, counts, background),
    }


def stress_metrics(f, thresholds):
    predictions = []
    gold_counts = Counter()
    backgrounds = 0
    for identifier, notes, spans in synthetic(240, stress=True):
        for refs in spans.values():
            gold_counts.update(refs.keys())
            backgrounds += not refs
        predictions.extend(assessment(identifier, spans, {}, None, f(notes)))
    return {"cases": 240, "at_zero": measurements(predictions, dict.fromkeys(FIELDS, 0), gold_counts, backgrounds),
            "calibrated": measurements(predictions, thresholds, gold_counts, backgrounds)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--v2", type=Path, required=True)
    parser.add_argument("--base", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--epochs", type=int, default=5)
    parser.add_argument("--synthetic", type=int, default=480)
    parser.add_argument("--threads", type=int, default=2)
    parser.add_argument("--random-head", action="store_true",
                        help="evaluate the seeded, untrained head without fitting any parameters")
    args = parser.parse_args()
    if args.epochs < 1 or args.threads < 1 or args.synthetic < 1:
        parser.error("epochs, threads, and synthetic count must be positive")
    os.environ["TOKENIZERS_PARALLELISM"] = "false"
    torch.set_num_threads(args.threads)
    torch.set_num_interop_threads(1)
    torch.use_deterministic_algorithms(True)
    for name, digest in BASE_FILES.items():
        if sha256(args.base / name) != digest:
            raise ValueError(f"unexpected pretrained model artifact: {name}")
    training_path = args.v2 / "training-lines.jsonl"
    validation_path = args.v2 / "validation-gold" / "line-gold.jsonl"
    validation_recordings_path = validation_path.with_name("recording-gold.jsonl")
    training_lines = rows(training_path)
    validation_lines = rows(validation_path)
    recording_references = rows(validation_recordings_path)
    train, valid = case_sources(args.v2, training_lines, validation_lines)
    tokenizer = BertWordPieceTokenizer(str(args.base / "vocab.txt"), lowercase=True)
    real, generated = training(tokenizer, train, training_lines, args.synthetic)
    training_labels = Counter(role for _, _, targets in real
                              for role, target in zip(FIELDS, targets[::2]) if target > 0)
    args.out.mkdir()
    result = dict(base_revision=BASE_REVISION, base_files=BASE_FILES, seed=SEED, epochs=args.epochs,
                  synthetic_recordings=args.synthetic, threads=args.threads, train_recordings=len(train),
                  train_groups=len({x["group"] for x in train.values()}), real_samples=len(real),
                  synthetic_samples=len(generated), real_positive_spans=dict(training_labels),
                  input_sha256={path.name: sha256(path) for path in
                                (training_path, validation_path, validation_recordings_path)},
                  runtime={name: version(name) for name in
                           ("torch", "transformers", "numpy", "onnx", "onnxruntime", "tokenizers")}, variants={})
    for variant in ("random",) if args.random_head else VARIANTS:
        if args.random_head:
            torch.manual_seed(SEED)
            model = SpanModel(args.base, "head").eval()
            losses, trainable_parameters = [], 0
        else:
            model, losses, trainable_parameters = fit(args.base, variant, real + generated, args.epochs)
        package = args.out / variant
        original = export(model, args.base, package)
        policy = dict(variant=variant, base_revision=BASE_REVISION, thresholds={},
                      onnx_word_embeddings="int8 per row, selected rows dequantized to float32",
                      header_limit=HEADER_LIMIT,
                      files={name: sha256(package / name) for name in ("model.onnx", "vocab.txt")},
                      predictor_sha256=sha256(Path(__file__).with_name("pilot.py")))
        dump(package / "policy.json", policy)
        predictor = Predictor(package)
        thresholds, validation = validation_metrics(predictor.scored, valid, validation_lines,
                                                    recording_references, calibrator=True)
        policy["thresholds"] = thresholds
        dump(package / "policy.json", policy)
        result["variants"][variant] = dict(
            validation=validation, stress=stress_metrics(predictor.scored, thresholds),
            thresholds=thresholds, losses=losses, trainable_parameters=trainable_parameters,
            total_parameters=sum(p.numel() for p in model.parameters()),
            package_bytes=sum(p.stat().st_size for p in package.iterdir()), fp32_graph_bytes=original.stat().st_size,
            policy_sha256=sha256(package / "policy.json"),
        )
        dump(args.out / "pilot-results.json", result)
    det = lambda notes: control(notes)["emissions"]
    result["control"] = validation_metrics(det, valid, validation_lines, recording_references)[1]["at_zero"]
    result["control_stress"] = stress_metrics(det, dict.fromkeys(FIELDS, 0))["at_zero"]
    dump(args.out / "pilot-results.json", result)


if __name__ == "__main__":
    main()
