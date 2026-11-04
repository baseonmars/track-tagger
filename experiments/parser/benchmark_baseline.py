"""Measure fresh one-case adapter processes and one persistent JSONL batch."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import time
from .runner import read_cases

def main():
    p=argparse.ArgumentParser();p.add_argument('--adapter',type=Path,required=True);p.add_argument('--cases',type=Path,required=True);p.add_argument('--id',required=True);p.add_argument('--output',type=Path,required=True);p.add_argument('--batch',type=int,default=30);args=p.parse_args()
    if args.output.exists(): raise SystemExit('refusing to overwrite result')
    found=[c for c in read_cases(args.cases) if c['id']==args.id]
    if len(found)!=1 or args.batch<1: raise SystemExit('expected one case and positive batch size')
    # Only source input is sent to the adapter; the JSONL data is not written as a benchmark artifact.
    c=found[0]; data=json.dumps({'id':c['id'],'notes':c['notes'],'files':c['files']},ensure_ascii=False)+'\n'; results=[]
    for count in [1,1,1,args.batch]:
        before=time.monotonic();run=subprocess.run(['/usr/bin/time','-lp',str(args.adapter)],input=data*count,text=True,capture_output=True,timeout=120,check=True);elapsed=time.monotonic()-before
        if len(run.stdout.splitlines())!=count: raise RuntimeError('adapter output count mismatch')
        cpu={}
        for metric in ('user','sys'):
            m=re.search(r'^'+metric+r'\s+([0-9.]+)',run.stderr,re.M)
            if m:cpu[metric+'_seconds']=float(m.group(1))
        m=re.search(r'^\s*(\d+)\s+maximum resident set size',run.stderr,re.M)
        if m:cpu['max_rss_bytes']=int(m.group(1))
        results.append({'cases':count,'wall_seconds':elapsed,'wall_per_case_seconds':elapsed/count,**cpu})
    args.output.parent.mkdir(parents=True,exist_ok=True);args.output.write_text(json.dumps({'adapter_sha256':hashlib.sha256(args.adapter.read_bytes()).hexdigest(),'adapter_size_bytes':args.adapter.stat().st_size,'case_id':args.id,'case_sha256':hashlib.sha256(data.encode()).hexdigest(),'samples':results,'note':'three fresh one-case processes; final sample is one batch in one persistent process. No per-item warm latency is claimed.'},indent=2,sort_keys=True)+'\n')
if __name__=='__main__':main()
