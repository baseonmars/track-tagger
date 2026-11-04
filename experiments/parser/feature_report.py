"""Descriptive overlapping source-feature strata; no prediction-conditioned groups."""
import argparse
import hashlib
import json
from pathlib import Path
from .runner import read_cases
from .hosted import canonical


def summarize(predictions, labels, ids=None):
    cases=read_cases(labels);rows=read_cases(predictions)
    if len({r['id'] for r in rows})!=len(rows):raise ValueError('duplicate prediction IDs')
    if len({c['id'] for c in cases})!=len(cases):raise ValueError('duplicate source IDs')
    if ids is not None:cases=[c for c in cases if c['id'] in set(ids)]
    by_id={r['id']:r for r in rows};features=sorted({f for c in cases for f in c.get('features',[])})
    if any(not c.get('features') for c in cases):features.append('no-listed-source-feature')
    result={'method':'existing frozen source feature flags only; overlapping descriptive signals, not exclusive format or template families',
            'labels_sha256':hashlib.sha256(Path(labels).read_bytes()).hexdigest(),
            'predictions_sha256':hashlib.sha256(Path(predictions).read_bytes()).hexdigest(),'strata':{}}
    for feature in features:
        selected=[c for c in cases if (not c.get('features') if feature=='no-listed-source-feature' else feature in c.get('features',[]))]
        eligible=[c for c in selected if c.get('label_status') in ('agent-reviewed','adjudicated','synthetic') and c.get('oracle')]
        item={'requested':len(selected),'eligible':len(eligible),'requested_ids':[c['id'] for c in selected],
              'attempted':sum(c['id'] in by_id for c in selected),
              'errors':sum('error' in by_id.get(c['id'],{}) for c in selected),
              'unattempted':sum(c['id'] not in by_id for c in selected)}
        for stage in ('raw','validated'):
            item[stage]={'exact':sum(bool(by_id.get(c['id'],{}).get(stage+'_score',{}).get('exact')) and 'error'not in by_id.get(c['id'],{}) for c in eligible),
                         'ready':sum(stage in by_id.get(c['id'],{}) and not by_id[c['id']][stage].get('review',True) for c in eligible),
                         'false_ready':sum(bool(by_id.get(c['id'],{}).get(stage+'_score',{}).get('false_ready')) for c in eligible)}
        result['strata'][feature]=item
    return result


def main():
    p=argparse.ArgumentParser();p.add_argument('predictions');p.add_argument('labels');p.add_argument('--common40',action='store_true');a=p.parse_args();ids=None
    if a.common40:
        try:manifest=json.loads(Path(a.labels).read_text())
        except ValueError:manifest={}
        ids=manifest.get('astra_ids') or [c['id'] for c in read_cases(a.labels) if c.get('selected_common40')]
        if len(ids)!=40:raise ValueError('frozen common40 selection required')
    print(canonical(summarize(a.predictions,a.labels,ids)))
if __name__=='__main__':main()
