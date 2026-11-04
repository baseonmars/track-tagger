"""Persisted session deadline, stage ceilings and independent annotation sequencing."""
import argparse
import concurrent.futures
import hashlib
import os
import json
from pathlib import Path
import time
from .annotations import draft, review, review_schema
from .budget import Ledger, BudgetExceeded
from .hosted import Adapter, canonical
from .runner import read_cases, run, PROMPT

MODELS = {
    'luna': {'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5},
    'sol': {'model':'gpt-6.1-sol','input_per_million':2,'output_per_million':10},
    'astra': {'model':'gpt-6-astra','input_per_million':10,'output_per_million':50}}

class StageAdapter:
    def __init__(self,adapter,ceiling,deadline):
        self.adapter,self.config=adapter,adapter.config
        self.start=adapter.ledger.summary()['charged_usd']
        self.ceiling,self.deadline=ceiling,deadline
        self.config['stage_usd']=ceiling
    def parse(self,*args):
        if time.time()>=self.deadline: raise BudgetExceeded('session reporting hour reached')
        return self.adapter.parse(*args)

def session(state):
    state=Path(state);state.mkdir(parents=True,exist_ok=True)
    path=state/'session.json'
    if not path.exists():path.write_text(canonical({'started_epoch':time.time(),'inference_deadline_epoch':time.time()+7*3600,'hard_deadline_epoch':time.time()+8*3600}))
    return json.loads(path.read_text())

def annotate_concurrently(cases,adapter,output,workers=4):
    """Four network workers, one checkpoint writer; sources/config cannot drift on resume."""
    output=Path(output)
    implementation={name:hashlib.sha256(Path(__file__).with_name(name).read_bytes()).hexdigest() for name in ('annotations.py','scoring.py','hosted.py','source_review.py')}
    from .hosted import SCHEMA
    fingerprint=hashlib.sha256(canonical({'fingerprint_version':2,'cases':cases,'config':adapter.config,'prompt':__import__('experiments.parser.annotations',fromlist=['ANNOTATION_PROMPT']).ANNOTATION_PROMPT,'schema':SCHEMA,'implementation_sha256':implementation}).encode()).hexdigest()
    manifest=output.with_suffix('.manifest.json')
    if manifest.exists() and json.loads(manifest.read_text())['fingerprint']!=fingerprint:
        raise ValueError('annotation inputs/config/prompt changed')
    if not manifest.exists():manifest.write_text(canonical({'fingerprint':fingerprint,'workers':min(workers,4)}))
    previous=read_cases(output) if output.exists() else []
    done={row['id'] for row in previous}
    pending=[c for c in cases if c['id'] not in done]
    def worker(case):
        try:return draft(case,adapter,None)
        except BudgetExceeded:return {'id':case['id'],'halted':True}
        except Exception as exc:
            diagnostic=getattr(exc,'result',{})
            return {'id':case['id'],'label_status':'annotation-error','error':type(exc).__name__,
                    'failure':diagnostic.get('failure'),'usage':diagnostic.get('usage'),
                    'latency_seconds':diagnostic.get('latency_seconds'),'actual_model':diagnostic.get('actual_model')}
    completed=0
    with concurrent.futures.ThreadPoolExecutor(max_workers=min(4,max(1,workers))) as pool:
        iterator=iter(pending)
        active={pool.submit(worker,c) for c in [next(iterator,None) for _ in range(min(4,len(pending)))] if c is not None}
        halted=False
        while active:
            finished,active=concurrent.futures.wait(active,return_when=concurrent.futures.FIRST_COMPLETED)
            for future in finished:
                row=future.result()
                if row.get('halted'):halted=True
                else:
                    with output.open('a') as stream:
                        stream.write(canonical(row)+'\n');stream.flush();os.fsync(stream.fileno())
                    completed+=1
                    if completed%25==0:print(canonical({'annotation_completed':len(done)+completed,'pending':len(cases)-len(done)-completed}),flush=True)
                if not halted:
                    case=next(iterator,None)
                    if case is not None:active.add(pool.submit(worker,case))
    return {'new_annotations':completed,'previous_annotations':len(done),'budget_or_deadline_halted':halted}

def main():
    p=argparse.ArgumentParser();p.add_argument('command',choices=['annotate','review','candidate','smoke']);p.add_argument('cases');p.add_argument('state');p.add_argument('--model',choices=MODELS,default='luna');p.add_argument('--stage-usd',type=float,default=1);p.add_argument('--phase',default='v1');p.add_argument('--split',default='validation');p.add_argument('--limit',type=int);p.add_argument('--freeze');p.add_argument('--mask',choices=['pilot','astra']);p.add_argument('--prompt-file');p.add_argument('--evidence-note-enum',action='store_true');p.add_argument('--cohort',choices=['common40','full200']);p.add_argument('--expansion-policy');p.add_argument('--budget-forecast');p.add_argument('--identifier-adapter');p.add_argument('--cache-phase');p.add_argument('--retry-transport-once',action='store_true');p.add_argument('--allocation-reason');p.add_argument('--env-file')
    a=p.parse_args();state=Path(a.state);clock=session(state);ledger=Ledger(state/'budget.sqlite')
    from .policy import freeze_policy
    freeze_policy(state/'selection-policy.json')
    config={**MODELS[a.model],'prices_verified':True,'output_token_policy':'min(16384,max(8192,2048+220*filecount))','identifier_executable':'/tmp/track-parser-adapter-v2'}
    if a.prompt_file or a.evidence_note_enum:
        if a.command not in ('candidate','smoke'):raise ValueError('candidate prompt/schema flags cannot change annotation jobs')
    prompt=Path(a.prompt_file).read_text() if a.prompt_file else PROMPT
    if a.evidence_note_enum:config['evidence_note_enum']=True
    if a.identifier_adapter:config['identifier_executable']=str(Path(a.identifier_adapter).resolve())
    if a.cache_phase:config['cache_phase_override']=a.cache_phase
    if a.retry_transport_once:config['retry_transport_once']=True
    if a.allocation_reason:ledger.allocate_stage((a.cache_phase or a.command+'-'+a.model+'-'+a.phase)+':'+config['model'],a.stage_usd,a.allocation_reason)
    if a.prompt_file:config['prompt_sha256']=hashlib.sha256(prompt.encode()).hexdigest()
    if a.command=='review':
        config.update(annotation_review=True,schema=review_schema())
    config_path=state/(a.model+'-config.json');config_path.write_text(canonical(config))
    adapter=StageAdapter(Adapter(config,ledger,state/'cache',**({'env_path':a.env_file} if a.env_file else {})),a.stage_usd,clock['inference_deadline_epoch'])
    if a.command=='candidate' and a.split=='test':
        label_manifest=json.loads(Path(a.cases).read_text())
        if label_manifest.get('labels_frozen') is not True:raise ValueError('test predictions require immutable frozen label artifact')
    cases=read_cases(a.cases)
    if a.mask:
        if not a.freeze:raise ValueError('frozen selection manifest required for pilot/Astra masks')
        manifest=json.loads(Path(a.freeze).read_text())
        ids=set(manifest['pilot_ids' if a.mask=='pilot' else 'astra_ids'])
        cases=[c for c in cases if c['id'] in ids]
    if a.command=='review':
        drafts={r['id']:r for r in read_cases(state/'annotations-luna.jsonl')}
        output=state/'reviewed-labels.jsonl'
        done={r['id'] for r in read_cases(output)} if output.exists() else set()
        for case in cases:
            if case.get('split') not in ('validation','test') or case['id'] in done or case['id'] not in drafts:continue
            try:review(case,drafts[case['id']],adapter,output)
            except BudgetExceeded:break
            except Exception:
                with (state/'annotation-errors.jsonl').open('a') as f:f.write(canonical({'id':case['id'],'model':a.model,'error':'review_failed'})+'\n')
    elif a.command=='annotate':
        # No predictions or prior labels are supplied to either model.
        cases=[c for c in cases if a.model=='luna' or c.get('split') in ('validation','test')]
        output=state/('annotations-'+a.model+'.jsonl')
        source_manifest=json.loads(Path(a.cases).read_text())
        pilot_ids=set(source_manifest.get('pilot_ids',[])) if isinstance(source_manifest,dict) else set()
        cases=sorted(cases,key=lambda c:(0 if c['id'] in pilot_ids else {'validation':1,'test':2,'train':3}.get(c.get('split'),4),c['id']))
        print(canonical(annotate_concurrently(cases,adapter,output)),flush=True)
    else:
        cases=[c for c in cases if c.get('split')==a.split]
        if a.command=='candidate' and a.split=='test':
            from .cohorts import test_cohort
            manifest=json.loads(Path(a.cases).read_text())
            cases=test_cohort(cases,manifest,config['model'],a.cohort,ledger,a.cases,prompt,(a.cache_phase or a.command+'-'+a.model+'-'+a.phase)+':'+config['model'],a.expansion_policy,a.budget_forecast)
            if a.model=='astra' and a.cohort=='full200':
                with ledger.connect() as db:adapter.config['stage_usd']=db.execute('SELECT cap FROM stages WHERE label=?',((a.cache_phase or a.command+'-'+a.model+'-'+a.phase)+':'+config['model'],)).fetchone()[0]/1e6
        if a.command=='smoke':
            if any(c.get('provenance',{}).get('kind') != 'synthetic' for c in cases):
                raise ValueError('smoke ground truth requires explicit synthetic provenance')
            cases=sorted(cases,key=lambda c:c['id'])[:a.limit or 3]
            # Synthetic truth is explicitly generated; source review is separate.
            cases=[{**c,'label_status':'synthetic'} for c in cases]
        elif a.limit:cases=cases[:a.limit]
        output=state/(a.command+'-'+a.model+'-'+a.phase+'.jsonl')
        print(canonical(run(cases,adapter,output,a.command+'-'+a.model+'-'+a.phase,prompt=prompt,hours=max(0,(clock['inference_deadline_epoch']-time.time())/3600))))
    print(canonical(ledger.summary()))

if __name__=='__main__':main()
