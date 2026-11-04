"""Resumable independent full-line source annotation using the hardened adapter."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
import sqlite3
from pathlib import Path
import time
from ..budget import Ledger, BudgetExceeded
from ..hosted import Adapter, canonical, request_schema
from ..orchestrate import session, MODELS
from ..path_guard import require_external
from .labels import PROMPT, schema, flatten, normalized_case, source_hash, lines

ALLOCATIONS={'annotation-v2-pilot-luna:gpt-6-luna':.5,'annotation-v2-pilot-sol:gpt-6.1-sol':4.5,'annotation-v2-training:gpt-6.1-sol':10,'annotation-v2-independent-author:gpt-6.1-sol':11.5,'annotation-v2-independent-review:gpt-6.1-sol':13.5,'annotation-v2-adjudication:gpt-6-astra':5}

class StudyLedger(Ledger):
    def reserve(self,label,ceiling,stage_cap=None):
        import re
        aggregate=re.sub(r'-r[2-9](?=:)', '', label)
        if aggregate not in ALLOCATIONS or stage_cap!=ALLOCATIONS[aggregate]:raise ValueError('unallocated or changed study stage')
        return super().reserve(aggregate,ceiling,stage_cap)

def initialize(root):
    root=require_external(root)
    root.mkdir(parents=True,exist_ok=True)
    database=root/'budget.sqlite'
    if database.exists():
        with sqlite3.connect(database.as_uri()+'?mode=ro',uri=True)as db:
            amendment=db.execute('SELECT count(*)FROM sqlite_master WHERE name="budget_amendments"').fetchone()[0]
            cap=db.execute('SELECT cap FROM config WHERE id=1').fetchone()
            if amendment or cap!=(50_000_000,):raise ValueError('refuse prior or amended ledger without modifying it')
    ledger=StudyLedger(root/'budget.sqlite',50)
    with ledger.connect() as db:
        if db.execute('SELECT count(*) FROM sqlite_master WHERE name="budget_amendments"').fetchone()[0]:raise ValueError('study ledger cannot contain prior budget amendment')
    if ledger.summary()['cap_usd']!=50:raise ValueError('study requires its own incremental50 ledger')
    for label,cap in ALLOCATIONS.items():
        with ledger.connect() as db:row=db.execute('SELECT cap FROM stages WHERE label=?',(label,)).fetchone()
        if row is None:ledger.allocate_stage(label,cap,'approved incremental50 study allocation; shared45 allocated after explicit2 independent-review reserve release;5 overall reserve')
        elif row[0]!=int(cap*1e6):raise ValueError('stage allocation drift')
    return ledger,session(root)

def config(model,phase,max_output=8192,review=False,line_only=False):
    import re
    c={**MODELS[model],'prices_verified':True,'schema':schema(line_only),'max_output_tokens':max_output,'service_tier':'default','line_annotation_note_enum':True,'evidence_note_enum':not line_only,'source_gold_identifiers':not line_only,'line_annotation_task':'line-only' if line_only else 'joint'}
    label=re.sub(r'-r[2-9]$', '', phase)+':'+c['model']
    if label not in ALLOCATIONS:raise ValueError('unknown phase/model allocation')
    c['stage_usd']=ALLOCATIONS[label]
    if review:
        c['line_source_review']=True
        c['schema']['properties']['verdict']={'type':'string','enum':['accepted','corrected','disputed']}
        c['schema']['required'].append('verdict')
    return c

def indexed_case(case):
    case=normalized_case(case)
    return {**case,'notes':[{**n,'text':'\n'.join(f"{i:06d}|{line}" for i,line in enumerate(lines(n['text'])))} for n in case['notes']]}

def prompt_for(c):
    if c.get('line_source_review'):
        return PROMPT+' Independently review the supplied annotation_draft against COMPLETE source, treating every draft value as untrusted. Candidate parser/model predictions are never present. Correct omissions, invalid spans/IDs and unsupported facts. Return the complete annotation with verdict accepted only if unchanged and source-supported, corrected for a corrected complete source-supported label, or disputed only if the expected source label itself cannot be established. Recording ambiguity is not annotation disagreement: represent supported roles/unknown mappings/review=true rather than dispute the whole label. Factual uncertainty retains known role. Source-author failures do not establish a label; author from complete source and mark corrected, with provenance retained separately.'
    if c.get('line_annotation_task')=='line-only':
        base=PROMPT.split(' Recording gold:')[0]
        return base.replace('Alongside segments, independently author complete recording gold metadata, titles, mappings and source evidence; this is gold labeling, never a candidate prediction.','Return titles=[] and mappings=[] compatibility members. Only full-line roles/flags/spans are being source-authored in this training phase.')
    return PROMPT

def payload_for(case,c):
    case=indexed_case(case);safe={'notes':[{'name':n['name'],'text':n['text']}for n in case['notes']],'files':case['files']}
    if c.get('line_source_review'):safe['annotation_draft']=case['annotation_draft']
    return {'model':c['model'],'service_tier':'default','input':[{'role':'system','content':prompt_for(c)},{'role':'user','content':canonical(safe)}],'reasoning':{'effort':'low'},'max_output_tokens':c['max_output_tokens'],'text':{'format':{'type':'json_schema','name':'concert_notes','strict':True,'schema':request_schema(c,case)}}}

def forecast(cases,c):
    rows=[]
    for case in cases:
        payload=payload_for(case,c)
        bound=len(canonical(payload).encode())+32768
        rows.append({'id':case['id'],'reserved_usd':(bound*c['input_per_million']*2.5+c['max_output_tokens']*c['output_per_million'])/1e6,'upper_input_tokens':bound})
    return {'cases':len(rows),'total_reserved_worst_case_usd':sum(r['reserved_usd'] for r in rows),'maximum_single_reservation_usd':max((r['reserved_usd'] for r in rows),default=0),'rows':rows,'settings':{k:v for k,v in c.items() if k!='schema'},'schema_sha256':hashlib.sha256(canonical(c['schema']).encode()).hexdigest(),'prompt_sha256':hashlib.sha256(PROMPT.encode()).hexdigest()}

def run(cases,root,model,phase,workers=2,max_output=8192,limit=None,review=False,line_only=False):
    if not 1<=workers<=4:raise ValueError('workers must be1..4')
    if len({x['id'] for x in cases})!=len(cases):raise ValueError('duplicate source case IDs')
    heldout=[x for x in cases if x['split']in ('validation','test')]
    if heldout:
        source_root=Path(root).parent
        try:
            approval=json.loads((source_root/'source-audit-approval.json').read_text())
            frozen=json.loads((source_root/'frozen-corpus.json').read_text())
        except FileNotFoundError:raise ValueError('heldout annotations require frozen independently audited source membership')from None
        digest=frozen['manifest_sha256'];members={x['id']:x for x in frozen['cases']}
        if approval.get('approved')is not True or approval.get('source_manifest_sha256')!=digest:raise ValueError('source audit approval mismatch')
        for x in heldout:
            expected=members.get(x['id'])
            if not expected or (x['group'],x['split'],source_hash(x),x.get('_source_manifest_sha256'))!=(expected['group'],expected['split'],source_hash(expected),digest):raise ValueError('heldout source membership changed')
    ledger,clock=initialize(root);c=config(model,phase,max_output,review=review,line_only=line_only)
    output=Path(root)/(phase+'.jsonl');manifest=output.with_suffix('.manifest.json')
    fingerprint=hashlib.sha256(canonical({'cases':[{'id':x['id'],'group':x['group'],'split':x['split'],'hash':source_hash(x),'draft_sha256':hashlib.sha256(canonical(x.get('annotation_draft')).encode()).hexdigest()} for x in cases],'config':c,'prompt':prompt_for(c),'implementation':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'labels_implementation':hashlib.sha256(Path(__file__).with_name('labels.py').read_bytes()).hexdigest(),'hosted_implementation':hashlib.sha256(Path(__file__).parents[1].joinpath('hosted.py').read_bytes()).hexdigest()}).encode()).hexdigest()
    if manifest.exists() and json.loads(manifest.read_text())['fingerprint']!=fingerprint:raise ValueError('annotation phase changed')
    if not manifest.exists():manifest.write_text(canonical({'fingerprint':fingerprint,'forecast':forecast(cases,c),'independent_source_only':True,'candidate_predictions_used':False})+'\n')
    snapshot=Path(root)/'phase-provenance'/phase
    if not snapshot.exists():
        snapshot.mkdir(parents=True)
        (snapshot/'prompt.txt').write_text(prompt_for(c))
        (snapshot/'config.json').write_text(canonical(c)+'\n')
        for name,path in [('run_labels.py',Path(__file__)),('labels.py',Path(__file__).with_name('labels.py')),('hosted.py',Path(__file__).parents[1]/'hosted.py')]:
            (snapshot/name).write_bytes(path.read_bytes())
    prior=[json.loads(line)['id'] for line in output.read_text().splitlines()] if output.exists() else []
    if len(set(prior))!=len(prior) or set(prior)-{x['id']for x in cases}:raise ValueError('duplicate or unknown checkpoint IDs')
    done=set(prior)
    adapter=Adapter(c,ledger,Path(root)/'response-cache')
    def worker(case):
        if time.time()>=clock['inference_deadline_epoch']:return {'id':case['id'],'halted':'deadline'}
        result={}
        try:
            result=adapter.parse(indexed_case(case),prompt_for(c),phase)
            flattened=flatten(case,result['output'],'draft' if model=='luna' else 'agent-reviewed')
            from ..source_review import validate_review
            oracle={k:v for k,v in result['output'].items() if k not in ('segments','verdict')}
            issues=validate_review(normalized_case(case),{}, {'verdict':'corrected','oracle':oracle,'reasons':[]})
            if issues and not line_only:raise ValueError('recording_gold_validation:'+','.join(issues))
            status='source-reviewed'if review else 'source-authored'if model=='sol'else 'draft'
            if result['output'].get('verdict')=='disputed':status='disputed'
            return {'id':case['id'],'source_input_sha256':source_hash(case),'label_status':status,'annotation':result['output'],'line_count':len(flattened),'cost_usd':result.get('cost_usd'),'usage':result.get('usage'),'actual_model':result.get('actual_model'),'requested_model':c['model'],'latency_seconds':result.get('latency_seconds'),'cache_hit':result.get('cache_hit'),'source_review_pass':review,'prior_author_had_output':bool(case.get('annotation_draft',{}).get('segments')) if review else None}
        except BudgetExceeded:return {'id':case['id'],'halted':'budget_or_circuit'}
        except Exception as exc:
            diagnostic=getattr(exc,'result',{}) or result
            failure=diagnostic.get('failure') or {'kind':'source_line_validation','code':str(exc) if (str(exc).startswith('recording_gold_validation:') or str(exc) in ('invalid line interval','invalid roles','invalid flag','invalid exact source span','duplicate line coverage','blank role mismatch','multirole line missing exact spans','multirole spans omit a role','incomplete full-note coverage')) else 'unclassified'}
            return {'id':case['id'],'source_input_sha256':source_hash(case),'label_status':'annotation-error','error':type(exc).__name__,'failure':failure,'cost_usd':diagnostic.get('cost_usd'),'usage':diagnostic.get('usage'),'annotation':result.get('output')}
    with ThreadPoolExecutor(max_workers=min(4,workers)) as pool,output.open('a') as stream:
        pending=[x for x in cases if x['id']not in done]
        if limit is not None:pending=pending[:limit]
        for offset in range(0,len(pending),min(4,workers)):
            batch=list(pool.map(worker,pending[offset:offset+min(4,workers)]))
            for row in batch:
                if 'halted'not in row:stream.write(canonical(row)+'\n');stream.flush();os.fsync(stream.fileno())
                print(canonical({'id':row['id'],'status':row.get('label_status',row.get('halted')),'cost_usd':row.get('cost_usd'),'ledger':ledger.summary()}),flush=True)
            if any('halted'in r for r in batch):break

def main():
    p=argparse.ArgumentParser();p.add_argument('cases',type=Path);p.add_argument('root',type=Path);p.add_argument('--model',choices=MODELS,required=True);p.add_argument('--phase',required=True);p.add_argument('--workers',type=int,default=2);p.add_argument('--max-output',type=int,default=8192);p.add_argument('--forecast-only',action='store_true');p.add_argument('--limit',type=int);p.add_argument('--review',action='store_true');p.add_argument('--line-only',action='store_true');a=p.parse_args()
    value=json.loads(a.cases.read_text());cases=value['cases'] if isinstance(value,dict) else value
    if a.forecast_only:print(canonical(forecast(cases,config(a.model,a.phase,a.max_output,review=a.review,line_only=a.line_only))))
    else:run(cases,a.root,a.model,a.phase,a.workers,a.max_output,a.limit,a.review,a.line_only)

if __name__=='__main__':main()
