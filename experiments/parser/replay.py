"""Reconstruct frozen inputs from listed note URLs; never discover items or fetch audio."""
import argparse
import concurrent.futures
import hashlib
import json
from pathlib import Path
import urllib.parse
from .acquire import decode_note, fetch, MAX_NOTE, safe_relative
from .hosted import canonical
from .runner import read_cases


def sha(data):return hashlib.sha256(data).hexdigest()

def reconstruct(record,partition,root):
    notes=[]
    for note in record['notes']:
        url=note['source_url'];parsed=urllib.parse.urlparse(url)
        if parsed.scheme!='https' or parsed.hostname!='archive.org' or not parsed.path.startswith('/download/'):
            raise ValueError('listed note URL is not an Archive download')
        if Path(note['path']).suffix.lower() not in {'.txt','.nfo','.md','.text'}:
            raise ValueError('manifest entry is not a supported original note')
        path=root/'packages'/record['package_id']/'notes'/safe_relative(note['path'])
        data=path.read_bytes() if path.exists() else fetch(url,MAX_NOTE)
        if sha(data)!=note['sha256'] or len(data)!=note['size']:
            raise ValueError('original note bytes unavailable or hash/size changed')
        if not path.exists():
            path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(data)
        text,encoding=decode_note(data)
        notes.append({'name':note['path'],'text':text,'encoding':'utf-8','source_encoding':encoding})
    files=[entry['path'] for entry in record['audio_files']]
    actual=sha(canonical({'notes':notes,'files':files}).encode())
    if actual!=partition['input_sha256']:raise ValueError('canonical candidate input hash mismatch')
    return {'id':record['package_id'],'group':partition['group'],'split':partition['split'],
            'features':partition.get('features',[]),'notes':notes,'files':files,
            'provenance':{'replayed_from_manifest':True,'source_input_sha256':actual,'artist':partition.get('artist'),'date':partition.get('date')}}

def replay(acquisition_manifest,split_manifest,root,workers=4):
    root=Path(root).resolve()
    if any((parent/'.git').exists() for parent in [root,*root.parents]):raise ValueError('original notes require an external non-repository root')
    if not 1<=workers<=16:raise ValueError('workers must be between 1 and 16')
    root.mkdir(parents=True,exist_ok=True)
    records=read_cases(acquisition_manifest);partitions=json.loads(Path(split_manifest).read_text())
    parts={c['id']:c for c in partitions['cases']}
    if len(parts)!=len(partitions['cases']) or {r['package_id'] for r in records}!=set(parts):raise ValueError('acquisition/split manifest IDs differ')
    expected=partitions.get('source_manifest_sha256')
    if expected and expected!=sha(Path(acquisition_manifest).read_bytes()):raise ValueError('acquisition manifest hash differs from frozen selection')
    cases=[];failures=[]
    def worker(record):
        try:return reconstruct(record,parts[record['package_id']],root),None
        except Exception as exc:return None,{'id':record['package_id'],'error_type':type(exc).__name__,'reason':'listed source note unavailable, changed, or undecodable; inspect the manifest ID and original hash'}
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        for case,error in pool.map(worker,records):
            if case:cases.append(case)
            if error:failures.append(error)
    cases.sort(key=lambda c:c['id'])
    output=root/'replayed-cases.jsonl';output.write_text(''.join(canonical(c)+'\n' for c in cases))
    result={'requested':len(records),'reconstructed':len(cases),'failures':failures,'all_input_hashes_verified':not failures,
            'audio_downloads':0,'discovery_requests':0,'source_manifest_sha256':sha(Path(acquisition_manifest).read_bytes()),'split_manifest_sha256':sha(Path(split_manifest).read_bytes())}
    (root/'replay-report.json').write_text(canonical(result)+'\n')
    return result

def main():
    p=argparse.ArgumentParser();p.add_argument('acquisition_manifest');p.add_argument('split_manifest');p.add_argument('external_root');p.add_argument('--workers',type=int,default=4);args=p.parse_args()
    result=replay(args.acquisition_manifest,args.split_manifest,args.external_root,args.workers)
    print(canonical(result))
    if result['failures']:raise SystemExit(1)
if __name__=='__main__':main()
