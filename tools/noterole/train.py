#!/usr/bin/env python3
"""Offline training: Go owns all feature semantics; runtime needs only Go+JSON.
Input JSONL covers every source line with roles (possibly multiple). Unknown role
labels must set role_disputed=true; factual uncertainty alone is not excluded.
"""
import argparse
import hashlib
import json
import math
import subprocess
from pathlib import Path

ROLES = ['artist','date','venue','city','title','heading','event','continuation','technical_provenance','other','blank']
VERSION = 'noterole-unicode-v1'

def train(rows, extractor, output, model_id):
    import numpy as np
    from scipy.sparse import csr_matrix
    from sklearn.linear_model import LogisticRegression
    if any(r.get('split') not in ('train','validation') for r in rows):
        raise ValueError('only train/validation rows allowed in training/calibration')
    groups = {}
    for row in rows:
        if not row.get('group') or not row.get('source_input_sha256'):
            raise ValueError('group and source input hash required')
        prior = groups.setdefault(row['group'], row['split'])
        if prior != row['split']:
            raise ValueError('group crosses train/validation boundary')
    selected = [r for r in rows if not r.get('role_disputed', False)]
    if any(not isinstance(r.get('roles'),list) or not r['roles'] or not set(r['roles']) <= set(ROLES) for r in selected):
        raise ValueError('explicit complete role targets required')
    if not selected or not any(r['split']=='train' for r in selected) or not any(r['split']=='validation' for r in selected):
        raise ValueError('training and validation required')
    payload=''.join(json.dumps({k:r[k] for k in ('line','prev','next')},ensure_ascii=False)+'\n' for r in selected)
    proc=subprocess.run([extractor],input=payload,text=True,capture_output=True,check=True)
    features=[json.loads(s) for s in proc.stdout.splitlines()]
    if len(features)!=len(selected) or any(f['feature_version']!=VERSION for f in features):
        raise ValueError('extractor contract mismatch')
    train_indices=[i for i,r in enumerate(selected) if r['split']=='train']
    val_indices=[i for i,r in enumerate(selected) if r['split']=='validation']
    # Frequency selection is training-only and deterministic. Never select using heldout text.
    counts={}
    for i in train_indices:
        for k,v in features[i]['features'].items():
            if len(k.encode('utf-8'))<=4096: counts[k]=counts.get(k,0)+v
    vocabulary=sorted(sorted(counts,key=lambda k:(-counts[k],k))[:30000])
    index={k:i for i,k in enumerate(vocabulary)}
    data=[];cols=[];ptr=[0]
    for f in features:
        for k,v in sorted(f['features'].items()):
            if k in index: cols.append(index[k]);data.append(v)
        ptr.append(len(data))
    x=csr_matrix((data,cols,ptr),shape=(len(selected),len(vocabulary)),dtype=np.float64)
    heads=[]
    for role in ROLES:
        y=np.array([int(role in r['roles']) for r in selected])
        if len(set(y[train_indices]))!=2:
            raise ValueError('each role needs positive and negative training examples: '+role)
        fit=LogisticRegression(max_iter=500,random_state=42,solver='liblinear').fit(x[train_indices],y[train_indices])
        p=fit.predict_proba(x[val_indices])[:,1]
        # Predeclared validation-only F1 threshold grid; ties favor conservative threshold.
        best=(-1,0.9)
        for threshold in [0.5,0.6,0.7,0.8,0.9]:
            pred=p>=threshold;truth=y[val_indices];tp=int(sum(pred & (truth==1)));fp=int(sum(pred & (truth==0)));fn=int(sum(~pred & (truth==1)))
            f1=2*tp/max(1,2*tp+fp+fn)
            best=max(best,(f1,threshold))
        heads.append(dict(role=role,weights=[0.0 if v==0 else float(v) for v in fit.coef_[0]],intercept=float(fit.intercept_[0]),threshold=best[1],margin=0.05))
    digest=hashlib.sha256(json.dumps(rows,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()
    artifact=dict(schema_version=1,feature_version=VERSION,model_id=model_id,training_sha256=digest,vocabulary=vocabulary,heads=heads)
    encoded=json.dumps(artifact,ensure_ascii=False,allow_nan=False,separators=(',',':')).encode()
    if len(encoded)>10*1024*1024: raise ValueError('artifact exceeds Go loader bound')
    Path(output).write_bytes(encoded+b'\n')
    # Fail before reporting success if runtime validation or scoring rejects export.
    try:
        subprocess.run([extractor,'--model',str(output)],input=json.dumps({k:selected[0][k] for k in ('line','prev','next')})+'\n',text=True,capture_output=True,check=True)
    except Exception:
        Path(output).unlink(missing_ok=True)
        raise
    return {'requested_lines':len(rows),'eligible_lines':len(selected),'train_lines':len(train_indices),'validation_lines':len(val_indices),'artifact_sha256':hashlib.sha256(encoded+b'\n').hexdigest()}

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('rows');p.add_argument('output');p.add_argument('--extractor',required=True);p.add_argument('--model-id',required=True);a=p.parse_args()
    print(json.dumps(train([json.loads(s) for s in Path(a.rows).read_text().splitlines() if s],a.extractor,a.output,a.model_id),sort_keys=True))
