"""Build the native inference pack from frozen Tiny BERT ONNX exports."""
import argparse
import gzip
from hashlib import sha256
import json
from pathlib import Path
import struct

import numpy as np
import onnx
from onnx.numpy_helper import to_array

MAGIC = b"TTBERT01"
FIELDS = ("artist", "date", "venue", "city")
EMBEDDING = "encoder.embeddings.word_embeddings.weight"
DENSE = ("attention/self/query", "attention/self/key", "attention/self/value",
         "attention/output/dense", "intermediate/dense", "output/dense")
EXPECTED = {
    "lora": "214311b9fb1ea2a6b601bc3473b4e9efd34acad07ec7bdba5b6dcda8ad12f098",
    "full": "f384135eab1f3828d49dedf326cef95c05109e42624016efba18f40795d62ed0",
}
VOCABULARY_SHA = "07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3"


def loaded(root, name):
    path = root / name / "model.onnx"
    if sha256(path.read_bytes()).hexdigest() != EXPECTED[name]:
        raise ValueError(f"non-frozen model: {name}")
    policy = root / name / "policy.json"
    graph = onnx.load(path).graph
    return (json.loads(policy.read_bytes()),
            {data.name: to_array(data) for data in graph.initializer},
            {node.name: node for node in graph.node})


def parameters(model):
    policy, data, nodes = model
    yield struct.pack("<4f", *(policy["thresholds"][role] for role in FIELDS))

    def floats(value):
        return np.asarray(value, dtype="<f4", order="C").tobytes()

    def param(name):
        return floats(data[name])

    def linear(path):
        node = nodes[path + "/MatMul"]
        yield floats(data[node.input[1]].T)
        yield param(path.strip("/").replace("/", ".") + ".bias")

    yield param("word_row_scale")
    yield param("encoder.embeddings.position_embeddings.weight")
    yield floats(data["encoder.embeddings.token_type_embeddings.weight"][0])
    yield param("encoder.embeddings.LayerNorm.weight")
    yield param("encoder.embeddings.LayerNorm.bias")
    for layer in range(2):
        path = f"/encoder/encoder/layer.{layer}/"
        for name in DENSE:
            yield from linear(path + name)
        for name in ("attention.output", "output"):
            yield param(f"encoder.encoder.layer.{layer}.{name}.LayerNorm.weight")
            yield param(f"encoder.encoder.layer.{layer}.{name}.LayerNorm.bias")
    yield from linear("/head")


def main():
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("frozen_packages", type=Path)
    cli.add_argument("output", type=Path)
    args = cli.parse_args()
    lora = loaded(args.frozen_packages, "lora")
    full = loaded(args.frozen_packages, "full")
    vocab = (args.frozen_packages / "lora" / "vocab.txt").read_bytes()
    if sha256(vocab).hexdigest() != VOCABULARY_SHA:
        raise ValueError("non-frozen vocabulary")
    base = lora[1][EMBEDDING]
    delta = full[1][EMBEDDING].astype(np.int16) - base.astype(np.int16)
    # Check this generation's delta stays lossless before narrowing.
    if delta.min() < -128 or delta.max() > 127:
        raise ValueError("embedding delta outside int8")
    with args.output.open("xb") as stream:
        with gzip.GzipFile(filename="", fileobj=stream, mode="wb", mtime=0) as pack:
            pack.write(MAGIC)
            pack.write(struct.pack("<I", len(vocab)))
            pack.write(vocab)
            pack.write(base.tobytes())
            pack.write(delta.astype(np.int8).tobytes())
            for model in (lora, full):
                for block in parameters(model):
                    pack.write(block)
    print(sha256(args.output.read_bytes()).hexdigest(), args.output.stat().st_size)


if __name__ == "__main__":
    main()
