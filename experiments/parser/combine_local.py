"""Combine immutable common and remaining checkpoints without inference."""
import argparse
import hashlib
import json
from pathlib import Path
from .hosted import canonical
from .runner import read_cases

def main():
    p=argparse.ArgumentParser();p.add_argument('common',type=Path);p.add_argument('remaining',type=Path);p.add_argument('frozen',type=Path);p.add_argument('output',type=Path);a=p.parse_args()
    if a.output.exists(): raise ValueError('output exists')
    common=read_cases(a.common);remaining=read_cases(a.remaining);cases=read_cases(a.frozen);ids=[r['id'] for r in common+remaining]
    if len(common)!=40 or len(ids)!=len(set(ids)) or set(ids)!={c['id'] for c in cases}: raise ValueError('incomplete or overlapping checkpoints')
    a.output.write_text(''.join(canonical(row)+'\n' for row in common+remaining))
    m={'requested_ids':[c['id'] for c in cases],'eligible_label_ids':[c['id'] for c in cases if c.get('label_status') in ('adjudicated','agent-reviewed') and c.get('oracle')], 'source_checkpoint_sha256':[hashlib.sha256(p.read_bytes()).hexdigest() for p in (a.common,a.remaining)],'frozen_sha256':hashlib.sha256(a.frozen.read_bytes()).hexdigest()}
    a.output.with_suffix(a.output.suffix+'.manifest.json').write_text(canonical(m)+'\n')
if __name__=='__main__':main()
