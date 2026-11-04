"""Acquire fresh text-only packages with conservative source-family exclusions."""
import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
from ..path_guard import require_external
import random
import re

from ..acquire import acquire, atomic_json, balanced_candidates, decode_note, fetch, public_record, scalar
from ..hosted import canonical

def sha(value):
    return hashlib.sha256(canonical(value).encode()).hexdigest()

def text(package, root):
    return '\n'.join(decode_note((root/'packages'/package['package_id']/n['local_path']).read_bytes())[0] for n in package['notes'])

def signatures(value):
    """Technical/source-chain lines, excluding track and ordinary metadata headers.

    Exact normalized lines plus technical word shingles are conservative signals,
    not a proof of semantic independence or author identity.
    """
    result=set()
    for line in value.splitlines():
        line=re.sub(r'\s+',' ',line.casefold()).strip()
        if not re.search(r'(?:record|transfer|taper|source|master|compiled|compilation|microphone|soundcard|lineage|tracked|upload|seeded|\.com|\.org|->| > )',line):continue
        words=re.findall(r'[a-z]+',line)
        if len(words)<7:continue
        result.add('line:'+line)
        for i in range(len(words)-11):result.add('words:'+' '.join(words[i:i+12]))
    return result

def aliases(value):
    return {''.join(ch for ch in re.sub(r'^the\s+','',v.strip().casefold()) if ch.isalnum()) for v in re.split(r'[;|]',value)}-{''}

def uploader(package):
    return scalar(package['reference_metadata'].get('uploader')).strip().casefold()

def performance(package):
    date=package['date'][:10]
    return {a+'|'+date for a in aliases(package['artist'])} if re.fullmatch(r'\d{4}-\d{2}-\d{2}',date) else set()

def acquire_fresh(root, old_root, old_manifest, target=500, workers=6):
    root=require_external(root)
    root.mkdir(parents=True,exist_ok=True)
    old_rows=[json.loads(x) for x in old_manifest.read_text().splitlines() if x.strip()]
    old_ids={r['package_id'] for r in old_rows}; old_uploaders=set();old_performances=set();old_signatures=set()
    for id in sorted(old_ids):
        package=json.loads((old_root/'packages'/id/'package.json').read_text())
        old_uploaders.add(uploader(package));old_performances.update(performance(package));old_signatures.update(signatures(text(package,old_root)))
    docs=json.loads((old_root/'discovery.json').read_text())
    recent=root/'discovery-recent-text.json'
    priority=balanced_candidates(json.loads(recent.read_text()),100)if recent.exists()else []
    ordered=list({d['identifier']:d for d in priority+balanced_candidates(docs,100)}.values())
    ordered=[d for d in ordered if d['identifier'] not in old_ids]
    state_path=root/'acquisition-state.json'
    state=json.loads(state_path.read_text()) if state_path.exists() else {'attempted':{},'accepted':[]}
    chosen=[];seen_uploaders=set(old_uploaders);seen_performances=set(old_performances);seen_signatures=set(old_signatures);artists=Counter()
    for id in state['accepted']:
        p=acquire(id,root);chosen.append(p);seen_uploaders.add(uploader(p));seen_performances.update(performance(p));seen_signatures.update(signatures(text(p,root)));artists.update(aliases(p['artist']))
    def candidate(doc):
        id=doc['identifier']
        try:
            metadata=json.loads(fetch('https://archive.org/metadata/'+id))
            u=scalar(metadata.get('metadata',{}).get('uploader')).strip().casefold()
            if not u:return id,None,'missing_uploader'
            if u in old_uploaders:return id,None,'old_uploader'
            package=acquire(id,root)
            return id,package,None
        except Exception as exc:
            known={'missing original notes or original audio file listing':'missing_original_inventory','empty original note':'empty_original_note','binary note':'binary_original_note','oversize original note':'oversize_original_note','oversize original note package':'oversize_original_package'}
            return id,None,known.get(str(exc),type(exc).__name__)
    pending=[d for d in ordered if d['identifier'] not in state['attempted']]
    with ThreadPoolExecutor(max_workers=workers) as pool:
        for offset in range(0,len(pending),workers):
            if len(chosen)>=target:break
            for id,p,error in pool.map(candidate,pending[offset:offset+workers]):
                if p is not None:
                    sig=signatures(text(p,root));perf=performance(p);a=aliases(p['artist']);u=uploader(p)
                    if u in seen_uploaders:error='uploader_component'
                    elif perf & seen_performances:error='performance_component'
                    elif sig & seen_signatures:error='distinctive_technical_template_component'
                    elif any(artists[x]>=2 for x in a):error='artist_balance'
                    elif len(chosen)>=target:error='target_already_met'
                    else:
                        chosen.append(p);state['accepted'].append(id);seen_uploaders.add(u);seen_performances.update(perf);seen_signatures.update(sig);artists.update(a)
                state['attempted'][id]=error or 'accepted'
            atomic_json(state_path,state)
            print(canonical({'attempted':len(state['attempted']),'accepted':len(chosen),'target':target}),flush=True)
    (root/'acquisition-manifest.jsonl').write_text(''.join(canonical(public_record(p))+'\n' for p in chosen))
    audit={'target':target,'accepted':len(chosen),'attempted':len(state['attempted']),'reasons':dict(Counter(state['attempted'].values())),'old_manifest_sha256':hashlib.sha256(old_manifest.read_bytes()).hexdigest(),'distinct_uploaders':len({uploader(p) for p in chosen}),'old_uploader_overlap':len({uploader(p) for p in chosen}&old_uploaders),'policy':'At most one fresh package per uploader/performance/technical-signature component; reject known old1200 relatives from every fresh split. Technical signatures are heuristic, not semantic proof.','no_audio_downloads':True,'candidate_order':'Existing downloads-ranked decade discovery, seeded artist-balanced order; not random Archive sample.'}
    atomic_json(root/'acquisition-report.json',audit)
    return audit

def main():
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--old-root',type=Path,required=True);p.add_argument('--old-manifest',type=Path,required=True);p.add_argument('--target',type=int,default=500);p.add_argument('--workers',type=int,default=6);a=p.parse_args()
    print(canonical(acquire_fresh(a.root,a.old_root,a.old_manifest,a.target,a.workers)))

if __name__=='__main__':main()
