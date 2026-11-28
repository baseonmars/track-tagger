"""Freeze external source/label inputs and export text-free reproducibility manifests."""
import argparse
import hashlib
import json
import re
from pathlib import Path
from .acquire import normalize_packages, group_packages
from .hosted import canonical
from .runner import freeze, read_cases

def digest(value):return hashlib.sha256(canonical(value).encode()).hexdigest()

def prepare(raw_root,source_manifest,output,public_output):
    cases=list(normalize_packages(Path(raw_root),source_manifest))
    for case in cases:
        text='\n'.join(n['text'] for n in case['notes'])
        signals={
            'multiple-original-notes':len(case['notes'])>1,
            'nested-original-audio-paths':any('/' in f for f in case['files']),
            'mixed-original-audio-formats':len({Path(f).suffix.lower() for f in case['files']})>1,
            'disc-set-heading-signal':bool(re.search(r'(?im)^\s*(?:disc|disk|cd|set)\s+(?:\d+|one|two|three|i{1,3})\b',text)),
            'duration-signal':bool(re.search(r'\b\d{1,2}:\d{2}\b',text)),
            'multiple-date-token-signal':len(set(re.findall(r'\b(?:19|20)\d{2}[-/.]\d{1,2}[-/.]\d{1,2}\b',text)))>1,
            'technical-section-signal':bool(re.search(r'(?im)^\s*(?:source|transfer|lineage|equipment)\s*:',text)),
            'parent-disc-path-signal':any(re.search(r'(?i)(?:disc|cd|set)\s*\d+/',f) for f in case['files'])}
        case['features']=sorted(set(case.get('features',[]))|{k for k,v in signals.items() if v})
    cases=group_packages(cases)
    ids={c['id'] for c in cases}
    selected={r['package_id'] for r in read_cases(source_manifest)}
    if len(ids)!=len(cases) or ids!=selected:raise ValueError('manifest package coverage mismatch')
    frozen=freeze(cases,output)
    public={k:v for k,v in frozen.items() if k!='cases'}
    public['source_manifest_sha256']=hashlib.sha256(Path(source_manifest).read_bytes()).hexdigest()
    public['cases']=[{'id':c['id'],'group':c['group'],'split':c['split'],'features':c.get('features',[]),
                      'artist':c['provenance'].get('artist'),'date':c['provenance'].get('date'),
                      'input_sha256':digest({'notes':c['notes'],'files':c['files']})} for c in frozen['cases']]
    Path(public_output).parent.mkdir(parents=True,exist_ok=True)
    if Path(public_output).exists():raise ValueError('public freeze exists')
    Path(public_output).write_text(canonical(public)+'\n')
    return public

def labels(frozen_source,reviewed,output,public_output=None):
    if Path(output).exists():raise ValueError('label freeze already exists')
    source=json.loads(Path(frozen_source).read_text())
    read_cases(frozen_source)  # Verify frozen content hash before accepting labels.
    rows=read_cases(reviewed)
    by_id={r['id']:r for r in rows}
    if len(by_id)!=len(rows):raise ValueError('duplicate reviewed label IDs')
    expected=[c for c in source['cases'] if c['split'] in ('validation','test')]
    expected_ids={c['id'] for c in expected}
    if set(by_id)-expected_ids:raise ValueError('unknown reviewed label IDs')
    result=[]
    for case in expected:
        row=by_id.get(case['id'])
        if row is None:row={**case,'label_status':'unreviewed','oracle':None}
        if digest({'notes':case['notes'],'files':case['files']})!=digest({'notes':row['notes'],'files':row['files']}):
            raise ValueError('reviewed source changed')
        if any(row.get(k)!=case.get(k) for k in ('id','group','split','features')):
            raise ValueError('reviewed partition metadata changed')
        result.append({**case,'oracle':row.get('oracle'),'label_status':row.get('label_status','unreviewed'),'annotation_review':row.get('annotation_review')})
    value={'labels_frozen':True,'source_sha256':hashlib.sha256(Path(frozen_source).read_bytes()).hexdigest(),
           'label_sha256':digest(result),'cases':result,'pilot_ids':source['pilot_ids'],'astra_ids':source['astra_ids'],
           'label_status_counts':{status:sum(r.get('label_status')==status for r in result) for status in sorted({r.get('label_status','unreviewed') for r in result})}}
    Path(output).write_text(canonical(value)+'\n')
    if public_output is not None:
        if Path(public_output).exists():raise ValueError('public label freeze already exists')
        public={k:v for k,v in value.items() if k!='cases'}
        public['cases']=[{'id':c['id'],'group':c['group'],'split':c['split'],'label_status':c['label_status'],'oracle_sha256':digest(c.get('oracle')),'reviewer_model':(c.get('annotation_review') or {}).get('reviewer_model')} for c in result]
        Path(public_output).parent.mkdir(parents=True,exist_ok=True)
        Path(public_output).write_text(canonical(public)+'\n')
    return {k:v for k,v in value.items() if k!='cases'}

def selected_labels(frozen_source,reviewed,output,selection='pilot_ids'):
    if Path(output).exists():raise ValueError('pilot labels already frozen')
    source=json.loads(Path(frozen_source).read_text());read_cases(frozen_source)
    ids={c['id'] for c in source['cases'] if c['split']=='test'} if selection=='test_ids' else set(source[selection]);expected=[c for c in source['cases'] if c['id'] in ids]
    rows={r['id']:r for r in read_cases(reviewed)}
    result=[]
    for case in expected:
        row=rows.get(case['id'])
        if row is None or row.get('label_status') not in ('agent-reviewed','adjudicated','disputed','unresolved') or not row.get('annotation_review'):
            raise ValueError('all fixed pilot IDs require completed source review before inference')
        if any(row.get(k)!=case.get(k) for k in ('id','group','split','features')) or digest({'notes':row['notes'],'files':row['files']})!=digest({'notes':case['notes'],'files':case['files']}):
            raise ValueError('reviewed pilot source/partition changed')
        result.append({**case,'oracle':row.get('oracle'),'label_status':row['label_status'],'annotation_review':row['annotation_review']})
    value={'labels_frozen':True,'selected_label_subset':selection,'source_sha256':hashlib.sha256(Path(frozen_source).read_bytes()).hexdigest(),'label_sha256':digest(result),'pilot_ids':source['pilot_ids'],'astra_ids':source['astra_ids'],'cases':result}
    Path(output).write_text(canonical(value)+'\n')
    return {'cases':len(result),'sha256':hashlib.sha256(Path(output).read_bytes()).hexdigest()}

def pilot_labels(frozen_source,reviewed,output):
    return selected_labels(frozen_source,reviewed,output,'pilot_ids')

def main():
    p=argparse.ArgumentParser();sub=p.add_subparsers(dest='command',required=True)
    a=sub.add_parser('sources');a.add_argument('raw');a.add_argument('manifest');a.add_argument('output');a.add_argument('public')
    b=sub.add_parser('labels');b.add_argument('sources');b.add_argument('reviewed');b.add_argument('output');b.add_argument('--public')
    args=p.parse_args()
    value=prepare(args.raw,args.manifest,args.output,args.public) if args.command=='sources' else labels(args.sources,args.reviewed,args.output,args.public)
    print(canonical({k:v for k,v in value.items() if k!='cases'}))
if __name__=='__main__':main()
