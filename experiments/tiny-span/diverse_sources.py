"""Sample public etree discovery across deposit years, prioritizing unseen performers."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from hashlib import sha256
import json
from pathlib import Path
import sys
from urllib.parse import urlencode


def run(study, cache, destination):
    sys.path.insert(0, str(study.resolve()))
    from experiments.parser.acquire import acquire, fetch, scalar
    from experiments.parser.study_v2.corpus import aliases
    destination.mkdir(exist_ok=False)
    known_ids, known_artists = set(), set()
    for root in (cache, cache / "v2"):
        for p in (root / "packages").glob("*/package.json"):
            record = json.loads(p.read_bytes())
            known_ids.add(record["package_id"])
            known_artists.update(aliases(record["artist"]))

    def discover(year):
        query = {"q": f'collection:etree AND format:"Text" AND addeddate:[{year}-01-01 TO {year}-12-31]',
                 "fl[]": ["identifier","creator"], "sort[]": ["downloads asc", "identifier asc"],
                 "rows": 1800, "output":"json"}
        raw = fetch("https://archive.org/advancedsearch.php?" + urlencode(query, doseq=True))
        (destination / f"discovery-{year}.json").write_bytes(raw)
        return json.loads(raw)["response"]["docs"]

    with ThreadPoolExecutor(4) as pool:
        discovered = [row for year in pool.map(discover, range(2003, 2027)) for row in year]
    ordered = sorted(discovered, key=lambda r: sha256(("rare-v1:"+r["identifier"]).encode()).digest())
    selected = []
    for row in ordered:
        names = aliases(scalar(row.get("creator")))
        if not names or row["identifier"] in known_ids or set(names) & known_artists:
            continue
        selected.append(row["identifier"])
        known_artists.update(names)
        known_ids.add(row["identifier"])
        if len(selected) >= 1000:
            break
    (destination / "queue.json").write_text(json.dumps(selected))
    def download(identifier):
        try:
            acquire(identifier,destination)
            return {"id":identifier,"status":"complete"}
        except (ValueError,OSError,KeyError) as exc:
            return {"id":identifier,"status":"failed","reason":str(exc)}
    with (destination / "outcomes.jsonl").open("x") as out, ThreadPoolExecutor(12) as pool:
        for result in pool.map(download,selected):
            out.write(json.dumps(result)+"\n")
            out.flush()


if __name__ == "__main__":
    p=argparse.ArgumentParser(description=__doc__)
    for name in ("study","cache","destination"):
        p.add_argument(name,type=Path)
    run(**vars(p.parse_args()))
