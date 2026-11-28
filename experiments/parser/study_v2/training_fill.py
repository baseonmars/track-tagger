"""Prefetch new recordings from old uploaders for training-only use."""
import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
from ..path_guard import require_external
from ..acquire import acquire, atomic_json
from ..hosted import canonical
from .corpus import performance, uploader

def prefetch(root,old_root,old_manifest,target=300,workers=2):
    root=require_external(root)
    old_ids={json.loads(l)['package_id']for l in old_manifest.read_text().splitlines()if l.strip()}
    old_performances=set()
    for id in old_ids:old_performances.update(performance(json.loads((old_root/'packages'/id/'package.json').read_text())))
    prior=json.loads((root/'acquisition-state.json').read_text())
    candidates=[id for id,reason in prior['attempted'].items()if reason=='old_uploader'and id not in old_ids]
    path=root/'training-fill-state.json';state=json.loads(path.read_text())if path.exists()else{'attempted':{},'accepted':[],'training_only':True}
    def worker(id):
        try:
            p=acquire(id,root)
            if performance(p)&old_performances:return id,None,'old_performance'
            return id,p,None
        except Exception as exc:
            reason='missing_original_inventory'if str(exc)=='missing original notes or original audio file listing'else type(exc).__name__
            return id,None,reason
    ids=[id for id in candidates if id not in state['attempted']]
    with ThreadPoolExecutor(max_workers=workers)as pool:
        for offset in range(0,len(ids),workers):
            if len(state['accepted'])>=target:break
            for id,p,error in pool.map(worker,ids[offset:offset+workers]):
                state['attempted'][id]=error or 'prefetched_training_only'
                if p:state['accepted'].append(id)
            atomic_json(path,state)
            print(canonical({'prefetched_training':len(state['accepted']),'attempted':len(state['attempted']),'target':target}),flush=True)
    return {'prefetched':len(state['accepted']),'reasons':dict(Counter(state['attempted'].values()))}

def main():
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--old-root',type=Path,required=True);p.add_argument('--old-manifest',type=Path,required=True);p.add_argument('--target',type=int,default=300);p.add_argument('--workers',type=int,default=2);a=p.parse_args();print(canonical(prefetch(a.root,a.old_root,a.old_manifest,a.target,a.workers)))

if __name__=='__main__':main()
