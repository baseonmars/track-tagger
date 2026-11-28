"""Rehydrate only committed original-note URLs and verify normalized study hashes."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import re
import urllib.parse
from ..acquire import decode_note,fetch,MAX_NOTE,safe_relative,atomic_json
from ..hosted import canonical
from ..path_guard import require_external
from .labels import normalize,source_hash

def reconstruct(record,part,root,digest):
    root=require_external(root)
    id=record['package_id']
    if not re.fullmatch(r'[A-Za-z0-9_.-]+',id)or id in ('.','..'):raise ValueError('unsafe source ID')
    notes=[]
    for note in record['notes']:
        name=safe_relative(note['path']);url=urllib.parse.urlparse(note['source_url'])
        if url.scheme!='https'or url.hostname!='archive.org'or urllib.parse.unquote(url.path)!='/download/'+id+'/'+name or url.query or url.fragment or Path(name).suffix.lower()not in {'.txt','.text','.nfo','.md'}:raise ValueError('manifest entry is not an original note URL')
        path=require_external(root/'packages'/id/'notes'/name)
        if not path.is_relative_to(root):raise ValueError('note cache escaped replay root')
        data=path.read_bytes()if path.exists()else fetch(note['source_url'],MAX_NOTE)
        if len(data)!=note['size']or hashlib.sha256(data).hexdigest()!=note['sha256']:raise ValueError('original note bytes unavailable or changed')
        if not path.exists():path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(data)
        value,encoding=decode_note(data);notes.append({'name':name,'text':normalize(value),'encoding':'utf-8','source_encoding':encoding})
    files=[f['path']for f in record['audio_files']]
    if files!=part['files']:raise ValueError('original file inventory differs')
    case={'id':id,'group':part['group'],'split':part['split'],'notes':notes,'files':files,'features':part.get('features',[]),'_source_manifest_sha256':digest}
    if source_hash(case)!=part['source_input_sha256']:raise ValueError('normalized source input hash differs')
    return case

def replay(acquisition,manifest,root,workers=4):
    root=require_external(root);root.mkdir(parents=True,exist_ok=True)
    if not 1<=workers<=8:raise ValueError('workers must be1..8')
    value=json.loads(manifest.read_text());digest=value.pop('manifest_sha256')
    if hashlib.sha256(canonical(value).encode()).hexdigest()!=digest:raise ValueError('source split manifest changed')
    records=[json.loads(l)for l in acquisition.read_text().split('\n')if l.strip()];parts={r['id']:r for r in value['cases']}
    if len(parts)!=len(value['cases'])or len({r['package_id']for r in records})!=len(records)or{r['package_id']for r in records}!=set(parts):raise ValueError('acquisition and partition membership differ')
    def worker(record):
        try:return reconstruct(record,parts[record['package_id']],root,digest),None
        except Exception as exc:return None,{'id':record['package_id'],'error_type':type(exc).__name__}
    cases=[];errors=[]
    with ThreadPoolExecutor(max_workers=workers)as pool:
        for case,error in pool.map(worker,records):
            if case:cases.append(case)
            if error:errors.append(error)
    cases.sort(key=lambda c:c['id']);output=root/'replayed-cases.jsonl';output.write_text(''.join(canonical(c)+'\n'for c in cases))
    report={'requested':len(records),'reconstructed':len(cases),'failures':errors,'all_source_input_hashes_verified':not errors,'source_manifest_sha256':digest,'acquisition_manifest_sha256':hashlib.sha256(acquisition.read_bytes()).hexdigest(),'audio_downloads':0,'discovery_requests':0}
    atomic_json(root/'replay-report.json',report);return report

def main():
    p=argparse.ArgumentParser();p.add_argument('acquisition',type=Path);p.add_argument('manifest',type=Path);p.add_argument('root',type=Path);p.add_argument('--workers',type=int,default=4);a=p.parse_args();result=replay(a.acquisition,a.manifest,a.root,a.workers);print(canonical(result))
    if result['failures']:raise SystemExit(1)

if __name__=='__main__':main()
