"""Rescore saved outputs without inference or mutation of raw/gold artifacts."""
import argparse
import hashlib
import json
from pathlib import Path
from .hosted import canonical
from .runner import read_cases, oracle, report
from .scorer_v3 import score, SCORER_VERSION


def rescore(results,labels,output):
    results,output=Path(results),Path(output)
    if output.exists():raise ValueError('rescored output already exists')
    label_rows=read_cases(labels);prediction_rows=read_cases(results)
    if len({c.get('id') for c in label_rows})!=len(label_rows) or any(not isinstance(c.get('notes'),list) or not isinstance(c.get('files'),list) for c in label_rows):
        raise ValueError('labels must contain unique source case records with notes and files')
    if len({r.get('id') for r in prediction_rows})!=len(prediction_rows) or any(not r.get('phase') or not (('raw' in r and 'validated' in r) or 'error' in r) for r in prediction_rows):
        raise ValueError('results must contain unique prediction checkpoint rows')
    cases={c['id']:c for c in label_rows}
    rows=[]
    for old in prediction_rows:
        case=cases.get(old['id'])
        if case is None:raise ValueError('prediction ID absent from frozen labels')
        row={**old,'score_version':SCORER_VERSION,'original_scored_row_sha256':hashlib.sha256(canonical(old).encode()).hexdigest()}
        if old.get('accuracy_eligible'):
            for stage in ('raw','validated'):
                value=row.get(stage,{'review':True})
                scored=score(value,oracle(case),case['files'])
                if 'error' in row:scored['exact']=False
                row[stage+'_score']=scored
        rows.append(row)
    output.parent.mkdir(parents=True,exist_ok=True)
    output.write_text(''.join(canonical(r)+'\n' for r in rows))
    original_manifest=results.with_suffix(results.suffix+'.manifest.json')
    manifest=json.loads(original_manifest.read_text()) if original_manifest.exists() else {}
    manifest.update(score_version=SCORER_VERSION,original_results_sha256=hashlib.sha256(results.read_bytes()).hexdigest(),
                    frozen_labels_sha256=hashlib.sha256(Path(labels).read_bytes()).hexdigest(),
                    scorer_sha256=hashlib.sha256(Path(__file__).with_name('scorer_v3.py').read_bytes()).hexdigest())
    output.with_suffix(output.suffix+'.manifest.json').write_text(canonical(manifest)+'\n')
    return report(output)

def main():
    p=argparse.ArgumentParser();p.add_argument('results');p.add_argument('labels');p.add_argument('output');a=p.parse_args()
    print(canonical(rescore(a.results,a.labels,a.output)))
if __name__=='__main__':main()
