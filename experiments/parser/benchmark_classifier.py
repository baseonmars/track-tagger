"""Measure a frozen classifier on fixed source-cited test lines, without fitting."""
import argparse
import hashlib
import json
from pathlib import Path
import resource
import time
from .classifier_refs import rehydrate,sha_file,verify_public_source
from .runner import read_cases

def main():
    p=argparse.ArgumentParser();p.add_argument('--frozen',type=Path,required=True);p.add_argument('--source-manifest',type=Path,required=True);p.add_argument('--partition',type=Path,required=True);p.add_argument('--refs',type=Path,required=True);p.add_argument('--refs-manifest',type=Path,required=True);p.add_argument('--model',type=Path,required=True);p.add_argument('--training-freeze',type=Path,required=True);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
    if a.output.exists():raise ValueError('output exists')
    manifest=json.loads(a.refs_manifest.read_text());freeze=json.loads(a.training_freeze.read_text());part=json.loads(a.partition.read_text());cases=read_cases(a.frozen);verify_public_source(cases,json.loads(a.source_manifest.read_text()))
    if manifest.get('stage')!='test' or manifest.get('refs_sha256')!=sha_file(a.refs) or manifest.get('source_manifest_sha256')!=sha_file(a.source_manifest) or manifest.get('partition_sha256')!=sha_file(a.partition) or manifest.get('model_sha256')!=sha_file(a.model) or freeze.get('model_sha256')!=sha_file(a.model) or manifest.get('training_refs_sha256')!=freeze.get('train_refs_sha256'):raise ValueError('frozen benchmark identity mismatch')
    refs=read_cases(a.refs)
    if any(r['split']!='test' for r in refs):raise ValueError('requires test-only references')
    rows=rehydrate(refs,cases,part,refs)
    import joblib
    from scipy.sparse import hstack
    model=joblib.load(a.model);vecs=model['vectorizers'];predictor=model['model'];samples=[]
    for i in range(3):
        start=time.perf_counter();cpu=time.process_time();xs=[(r['line'],r['line'],'previous '+r['prev']+' next '+r['next']) for r in rows];x=hstack([v.transform([z[j] for z in xs]) for j,v in enumerate(vecs)]).tocsr();pred=list(predictor.predict(x));samples.append({'call':i+1,'wall_seconds':time.perf_counter()-start,'process_cpu_seconds':time.process_time()-cpu,'prediction_sha256':hashlib.sha256(json.dumps(pred,ensure_ascii=False).encode()).hexdigest()})
    a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(json.dumps({'lines':len(rows),'model_sha256':sha_file(a.model),'refs_sha256':sha_file(a.refs),'samples':samples,'peak_rss_platform_units':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,'note':'transform plus prediction on fixed cited positive lines, excludes model load and source rehydration; not end-to-end parser time'},indent=2,sort_keys=True)+'\n')
if __name__=='__main__':main()
