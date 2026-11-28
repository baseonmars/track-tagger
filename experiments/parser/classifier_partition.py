"""Freeze conservative classifier-only source exclusions without predictions."""
import argparse
import hashlib
import json
from collections import defaultdict,Counter
from pathlib import Path
from .runner import read_cases

def normalized(value):
    return value.strip().casefold() if isinstance(value,str) else ''

def partition(cases,uploaders,signature_ids):
    groups=defaultdict(list)
    for c in cases: groups[c['group']].append(c)
    if any(len({c['split'] for c in cs})!=1 for cs in groups.values()): raise ValueError('group crosses splits')
    identities={s:{uploaders.get(c['id'],'') for c in cases if c['split']==s}-{''} for s in ('train','validation','test')}
    sig_groups={c['group'] for c in cases if c['id'] in signature_ids};exclusions={}; test_unknown=[]
    for group,cs in groups.items():
        split=cs[0]['split'];ids=[c['id'] for c in cs]; us={uploaders.get(id,'') for id in ids}; reasons=set()
        if split=='test':
            test_unknown.extend(id for id in ids if not uploaders.get(id));continue
        if '' in us: reasons.add('missing_uploader')
        if split=='train' and us & (identities['validation']|identities['test']): reasons.add('uploader_in_validation_or_test')
        if split=='validation' and us & identities['test']: reasons.add('uploader_in_test')
        if group in sig_groups: reasons.add('verified_test_template_family')
        if reasons:
            for id in ids: exclusions[id]=sorted(reasons)
    return exclusions,test_unknown

def main():
    p=argparse.ArgumentParser();p.add_argument('--frozen',type=Path,required=True);p.add_argument('--manifest',type=Path,required=True);p.add_argument('--audit',type=Path,required=True);p.add_argument('--packages',type=Path,required=True);p.add_argument('--output',type=Path,required=True);args=p.parse_args()
    if args.output.exists(): raise SystemExit('partition already exists')
    cases=read_cases(args.frozen); manifests=read_cases(args.manifest);by_id={m['package_id']:m for m in manifests}
    if len(by_id)!=len(manifests) or set(by_id)!={c['id'] for c in cases}: raise ValueError('manifest and frozen source IDs differ')
    uploaders={};hashes={}
    for c in cases:
        id=c['id'];meta=args.packages/id/'archive-metadata.json'; data=meta.read_bytes()
        if hashlib.sha256(data).hexdigest()!=by_id[id]['archive_metadata_sha256']: raise ValueError('acquisition metadata hash mismatch')
        u=normalized(json.loads(data)['metadata'].get('uploader'));uploaders[id]=u;hashes[id]=hashlib.sha256(u.encode()).hexdigest() if u else None
    audit=json.loads(args.audit.read_text());match=audit['signature_overlap_diagnostic']['case_ids_by_split'];sig=set(sum((match[s] for s in ('train','validation','test')),[])) if match['test'] else set()
    exclusions,unknown=partition(cases,uploaders,sig)
    counts=Counter(c['split']+'_excluded' if c['id'] in exclusions else c['split']+'_retained' for c in cases)
    result={'policy_version':1,'source_sha256':hashlib.sha256(args.frozen.read_bytes()).hexdigest(),'acquisition_manifest_sha256':hashlib.sha256(args.manifest.read_bytes()).hexdigest(),'audit_sha256':hashlib.sha256(args.audit.read_bytes()).hexdigest(),'algorithm_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'exclusions':exclusions,'test_unknown_uploader_ids':sorted(unknown),'uploader_hash_by_case':hashes,'counts':dict(counts),'method':'Exclude whole train groups sharing an uploader with any validation or test case, validation groups sharing an uploader with any test case, train/validation groups with missing uploader, and train/validation groups matching the source-audited test template signature. Test inputs retained. This conservative metadata policy does not prove separation of all cross-uploader templates.'}
    args.output.parent.mkdir(parents=True,exist_ok=True);args.output.write_text(json.dumps(result,sort_keys=True,indent=2)+'\n');print(json.dumps({'counts':dict(counts),'test_unknown':len(unknown)},sort_keys=True))
if __name__=='__main__':main()
