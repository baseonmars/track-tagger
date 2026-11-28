"""Bind the reviewer overlay, baseline and enabled neural packages before evaluation."""
from hashlib import sha256
import json
from pathlib import Path


def digest(path):
    return sha256(Path(path).read_bytes()).hexdigest()


def bindings(root, packages, catalog, context, scorer, model, date_catalog):
    return {
        "entrypoints": {name:digest(root / name) for name in
                        ("pilot.py","evaluate.py","catalog/main.go","execution.py")},
        "catalog_binary":digest(catalog),
        "prior_classifier":{name:digest(context / "experiments/context" / name)
                            for name in ("candidate.py","score.py","intervals.py","results/context-model-freeze.json")},
        "prior_artifacts":[digest(path) for path in (scorer,model,date_catalog)],
        "packages":{name:{p.name:digest(p) for p in sorted(directory.iterdir()) if p.is_file()}
                    for name,directory in packages.items()},
    }


if __name__ == "__main__":
    import argparse
    p=argparse.ArgumentParser(description=__doc__)
    for name in ("cache","context","catalog","output"):
        p.add_argument(name,type=Path)
    a=p.parse_args()
    data=a.cache / "v2"
    packages={name:a.cache / "tiny-span/run" / variant for name,variant in
              (("lora_adapted","lora"),("head_only","head"),("full_adapted","full"))}
    payload=bindings(Path(__file__).resolve().parent,packages,a.catalog,a.context,data/"noterole-features",
                     data/"classifier-model/model.json",data/"three-arm-build/catalog-fixed")
    with a.output.open("x") as stream:
        json.dump(payload,stream,sort_keys=True,indent=2)
        stream.write("\n")
