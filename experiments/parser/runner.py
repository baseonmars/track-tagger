"""Checkpointed experiment CLI. Every output stays under the selected external state."""
import argparse
import hashlib
import json
from pathlib import Path
import random
import time
from .budget import Ledger, BudgetExceeded
from .hosted import Adapter, canonical
from .scoring import FIELDS, reconcile, score, validate, evidence_score

PROMPT = '''Extract concert notes conservatively. Return only facts explicitly supported by notes.
Use ISO dates with source precision (YYYY, YYYY-MM, or YYYY-MM-DD) only when unambiguous, otherwise empty string; exclude transfer dates. Preserve exact song text.
Match files only using explicit disc/track identifiers; never use list position. Track-only numbering defaults to disc 1 unless an explicit parent Disc/Set heading establishes another disc. Missing disc 1 alone is not ambiguous. Recognize Set One and Disc 1 headings. Preserve identifier numbering, title punctuation, and segue text. Remove only clearly delimited duration metadata. Missing, conflicting,
unsupported, duplicate or ambiguous identifiers require review. Unmapped files use zero disc/number
and empty title. Titles use canonical positive decimal disc:track IDs without leading zeroes (for example 1:1). Return every schema field; absent metadata is empty. Cite exact note quotes in evidence with field paths artist/date/venue/city or titles.DISC:TRACK.'''

def read_cases(path):
    text = Path(path).read_text()
    try:
        value=json.loads(text)
        if isinstance(value,dict) and 'cases' in value:
            expected=value.get('case_sha256',value.get('label_sha256'))
            if expected and hashlib.sha256(canonical(value['cases']).encode()).hexdigest()!=expected:
                raise ValueError('frozen case/label content hash mismatch')
            if value.get('labels_frozen') is True:
                frozen_hash=hashlib.sha256(Path(path).read_bytes()).hexdigest()
                return [{**c,'_labels_frozen_sha256':frozen_hash} for c in value['cases']]
            return value['cases']
        return value if isinstance(value,list) else [value]
    except json.JSONDecodeError:
        return [json.loads(x) for x in text.splitlines() if x.strip()]

def oracle(case):
    return case.get('oracle') or {k:case[k] for k in FIELDS if k in case}

def strata(case):
    provenance=case.get('provenance',{})
    date=str(provenance.get('date',''))
    decade=date[:3]+'0' if date[:4].isdigit() else 'unknown'
    return {'artist:'+str(provenance.get('artist',case.get('group','unknown'))),
            'group:'+str(case.get('group','unknown')),'decade:'+decade} | {'feature:'+f for f in case.get('features',[])}

def select_subset(cases,count,seed=20261103):
    remaining=sorted(cases,key=lambda c:hashlib.sha256((str(seed)+c['id']).encode()).hexdigest())
    seen={};selected=[]
    while remaining and len(selected)<count:
        chosen=max(remaining,key=lambda c:sum(1/(1+seen.get(k,0)) for k in strata(c)))
        selected.append(chosen);remaining.remove(chosen)
        for k in strata(chosen):seen[k]=seen.get(k,0)+1
    return [c['id'] for c in selected],seen

def freeze(cases, out, seed=20261103, counts=(800,200,200)):
    if Path(out).exists():
        raise ValueError('freeze target exists; never overwrite frozen splits')
    groups = {}
    for c in cases:
        # Acquisition supplies merged group IDs for alternate recordings/templates.
        if not c.get('group'):
            raise ValueError('group required before freezing')
        groups.setdefault(c['group'], []).append(c)
    keys = sorted(groups)
    random.Random(seed).shuffle(keys)
    if sum(len(v) for v in groups.values()) < sum(counts):
        raise ValueError('insufficient packages for requested split')
    # Whole groups go to the most underfilled partition; report actual quotas.
    assigned = {name: [] for name in ('train','validation','test')}
    targets = dict(zip(assigned, counts))
    for key in sorted(keys, key=lambda k: -len(groups[k])):
        split = max(assigned, key=lambda name: (targets[name]-len(assigned[name])) / max(targets[name],1))
        assigned[split].extend({**c, 'split':split} for c in groups[key])
    result = [c for name in assigned for c in assigned[name]]
    pilot,pilot_strata=select_subset([c for c in result if c['split']=='validation'],30,seed)
    astra,astra_strata=select_subset([c for c in result if c['split']=='test'],40,seed)
    manifest={'seed':seed,'actual_counts':{name:len(v) for name,v in assigned.items()},'case_sha256':hashlib.sha256(canonical(result).encode()).hexdigest(),
              'pilot_ids':pilot,'pilot_strata':pilot_strata,
              'astra_ids':astra,'astra_strata':astra_strata, 'cases':result}
    Path(out).write_text(canonical(manifest)+'\n')
    return manifest

def implementation_hash(adapter):
    digest=hashlib.sha256()
    for path in sorted(Path(__file__).parent.glob('*.py')):
        digest.update(path.name.encode());digest.update(path.read_bytes())
    config=getattr(adapter,'config',{})
    for key in ('identifier_executable','executable'):
        if key in config and Path(config[key]).is_file():digest.update(Path(config[key]).read_bytes())
    return digest.hexdigest()

def run(cases, adapter, output, phase, prompt=PROMPT, hours=7):
    from .guards import candidate_guard
    for case in cases:candidate_guard(case,phase,getattr(adapter,'config',{}).get('kind')=='catalog-baseline')
    output=Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    fingerprint=hashlib.sha256(canonical({'cases':cases,'prompt':prompt,'phase':phase,'config':getattr(adapter,'config',{}),'implementation_sha256':implementation_hash(adapter)}).encode()).hexdigest()
    lock=output.with_suffix(output.suffix+'.manifest.json')
    if lock.exists() and json.loads(lock.read_text())['fingerprint'] != fingerprint:
        raise ValueError('run fingerprint changed; use a new output phase')
    if not lock.exists():
        lock.write_text(canonical({'fingerprint':fingerprint,'requested_ids':[c['id'] for c in cases],'eligible_label_ids':[c['id'] for c in cases if c.get('label_status') in ('adjudicated','agent-reviewed','synthetic') and bool(oracle(c))],'deadline_epoch':time.time()+min(hours,7)*3600}))
    done={r['id'] for r in read_cases(output)} if output.exists() else set()
    deadline=json.loads(lock.read_text())['deadline_epoch']
    for case in cases:
        if case['id'] in done:
            continue
        if time.time() >= deadline:
            break
        result={}
        try:
            result=adapter.parse(case,prompt,phase)
            raw=validate(result['output'])
            is_baseline=getattr(adapter,'config',{}).get('kind')=='catalog-baseline' or result.get('hybrid_used_fallback') is False
            if is_baseline:
                # Catalog output already passed production safeguards; preserve its readiness/mapping semantics.
                validated={**raw}
            else:
                identifiers=None
                identifier_executable=getattr(adapter,'config',{}).get('identifier_executable')
                if identifier_executable:
                    from .adapters import catalog_identifiers
                    identifiers=catalog_identifiers(identifier_executable,case['files'])
                validated=reconcile(raw,case['files'],identifiers)
            evidence=evidence_score(raw,case['notes'])
            if not is_baseline and (evidence['invalid_quotes'] or evidence['unsupported_populated_fields']):validated['review']=True
            row={'id':case['id'],'phase':phase,'raw':raw,'validated':validated,
                 'latency_seconds':result['latency_seconds']+result.get('baseline_latency_seconds',0),'inference_latency_seconds':result['latency_seconds'],'baseline_latency_seconds':result.get('baseline_latency_seconds',0),'usage':result['usage'],'cache_hit':result['cache_hit'],'features':case.get('features',[]),'actual_model':result.get('actual_model'),'cost_usd':result.get('cost_usd'),'cost_method':result.get('cost_method'),'settings':result.get('settings'),'adapter_provenance':result.get('provenance'),'resources':result.get('resources'), 'hybrid_used_fallback':result.get('hybrid_used_fallback')}
            # Only independently adjudicated real labels or explicit synthetic oracles qualify.
            eligible=case.get('label_status') in ('adjudicated','agent-reviewed','synthetic') and bool(oracle(case))
            row['accuracy_eligible']=eligible
            row['evidence_score']=evidence_score(raw,case['notes'])
            if eligible:
                row['raw_score']=score(raw,oracle(case),case['files'])
                row['validated_score']=score(validated,oracle(case),case['files'])
        except BudgetExceeded:
            break
        except Exception as exc:
            diagnostic=getattr(exc,'result',result)
            if not diagnostic:diagnostic={'latency_seconds':getattr(exc,'latency_seconds',None),'usage':getattr(exc,'usage',{})}
            row={'id':case['id'],'phase':phase,'error':type(exc).__name__,'resources':diagnostic.get('resources',getattr(exc,'resources',None)),'features':case.get('features',[]),'failure':diagnostic.get('failure',{'kind':'local_schema_or_adapter'}),'latency_seconds':diagnostic.get('latency_seconds'),'usage':diagnostic.get('usage',{}),'actual_model':diagnostic.get('actual_model'),'cost_usd':diagnostic.get('cost_usd'),'cost_method':diagnostic.get('cost_method'),'settings':diagnostic.get('settings'),'accuracy_eligible':case.get('label_status') in ('adjudicated','agent-reviewed','synthetic') and bool(oracle(case))}
            if row['accuracy_eligible']:
                failed=score({'review':True},oracle(case),case['files'])
                failed['exact']=False
                row.update(raw_score=failed,validated_score=failed)
        with output.open('a') as stream:
            stream.write(canonical(row)+'\n')
            stream.flush()
    return report(output)

def report(path):
    rows=read_cases(path) if Path(path).exists() else []
    eligible=[r for r in rows if r.get('accuracy_eligible') and 'raw_score' in r]
    manifest_path=Path(path).with_suffix(Path(path).suffix+'.manifest.json')
    manifest=json.loads(manifest_path.read_text()) if manifest_path.exists() else {}
    requested_ids=manifest.get('requested_ids',[r['id'] for r in rows])
    result={'requested':len(requested_ids),'unattempted':len(set(requested_ids)-{r['id'] for r in rows}),'eligible_labels_requested':len(manifest.get('eligible_label_ids',eligible)), 'completed':len(rows),'errors':sum('error' in r for r in rows),'accuracy_eligible':len(eligible),'valid_outputs':sum('error' not in r for r in rows)}
    for stage in ('raw','validated'):
        scores=[r[stage+'_score'] for r in eligible]
        result[stage]={key:sum(int(s[key]) for s in scores) for key in ('exact','correct_titles','expected_titles','correct_mappings','expected_mappings','wrong_mappings','incorrect_accepted_mappings','false_ready','review','invented_info','missed_ambiguity')}
    result['format_strata']={feature:{'cases':sum(feature in r.get('features',[]) for r in eligible),'exact':sum(feature in r.get('features',[]) and r['validated_score']['exact'] for r in eligible)} for feature in sorted({f for r in eligible for f in r.get('features',[])})}
    result['metadata_fields']={field:{'correct':sum(r['validated_score'].get('metadata_fields',{}).get(field,False) for r in eligible),'scored':sum(field in r['validated_score'].get('metadata_fields',{}) for r in eligible)} for field in ('artist','date','venue','city')}
    latency=sorted(r['latency_seconds'] for r in rows if isinstance(r.get('latency_seconds'),(int,float)))
    result['latency_seconds']={'p50':latency[len(latency)//2] if latency else None,'p95':latency[min(len(latency)-1,int(len(latency)*.95))] if latency else None,'observations':len(latency)}
    result['reported_usage_cost_usd']=sum(r.get('cost_usd') or 0 for r in rows)
    result['cost_method_counts']={method:sum(r.get('cost_method')==method for r in rows) for method in {r.get('cost_method') for r in rows} if method}
    result['usage']={k:sum(r.get('usage',{}).get(k,0) for r in rows) for k in ('input_tokens','output_tokens')}
    result['exact_wilson_95']=wilson(result['validated']['exact'],len(eligible))
    return result

def wilson(successes,total):
    if not total:return None
    z=1.95996398454;p=successes/total;den=1+z*z/total
    center=(p+z*z/(2*total))/den
    half=z*((p*(1-p)/total+z*z/(4*total*total))**.5)/den
    return [center-half,center+half]

def preflight_identifiers(config):
    """Published model configs require their declared catalog identifier helper."""
    executable=config.get('identifier_executable')
    if config.get('identifier_adapter_source') and not executable:
        raise ValueError('build the declared identifier adapter and pass --identifier-adapter')
    if executable and (not Path(executable).is_file() or not __import__('os').access(executable,__import__('os').X_OK)):
        raise ValueError('identifier adapter must be an existing executable')


def main():
    p=argparse.ArgumentParser()
    sub=p.add_subparsers(dest='command',required=True)
    f=sub.add_parser('freeze');f.add_argument('cases');f.add_argument('output')
    f.add_argument('--counts',nargs=3,type=int,default=[800,200,200])
    r=sub.add_parser('run');r.add_argument('cases');r.add_argument('config');r.add_argument('state');r.add_argument('--phase',required=True);r.add_argument('--split',choices=['train','validation','test'],required=True);r.add_argument('--hours',type=float,default=7);r.add_argument('--mask',choices=['pilot','astra']);r.add_argument('--cohort',choices=['common40','full200']);r.add_argument('--expansion-policy');r.add_argument('--budget-forecast');r.add_argument('--prompt-file');r.add_argument('--identifier-adapter');r.add_argument('--env-file')
    q=sub.add_parser('report');q.add_argument('results')
    args=p.parse_args()
    if args.command=='freeze':
        m=freeze(read_cases(args.cases),args.output,counts=args.counts);print(canonical({k:v for k,v in m.items() if k!='cases'}))
    elif args.command=='report':print(canonical(report(args.results)))
    else:
        state=Path(args.state);state.mkdir(parents=True,exist_ok=True)
        cases=read_cases(args.cases)
        cases=[c for c in cases if not args.split or c.get('split')==args.split]
        if args.mask:
            manifest=json.loads(Path(args.cases).read_text())
            selected=set(manifest['pilot_ids' if args.mask=='pilot' else 'astra_ids'])
            cases=[c for c in cases if c['id'] in selected]
        config=json.loads(Path(args.config).read_text())
        prompt=Path(args.prompt_file).read_text() if args.prompt_file else PROMPT
        if config.get('prompt_sha256') and hashlib.sha256(prompt.encode()).hexdigest()!=config['prompt_sha256']:raise ValueError('configured prompt hash differs')
        if args.identifier_adapter:config['identifier_executable']=str(Path(args.identifier_adapter).resolve())
        preflight_identifiers(config)
        ledger=Ledger(state/'budget.sqlite')
        from .orchestrate import StageAdapter
        session_path=state/'session.json'
        if not session_path.exists():raise ValueError('persisted session required; initialize with orchestrate')
        clock=json.loads(session_path.read_text())
        if args.split=='test':
            frozen=json.loads(Path(args.cases).read_text())
            if frozen.get('labels_frozen') is not True:raise ValueError('test predictions require frozen labels')
            from .cohorts import test_cohort
            cases=test_cohort(cases,frozen,config['model'],args.cohort,ledger,args.cases,prompt,config.get('cache_phase_override',args.phase)+':'+config['model'],args.expansion_policy,args.budget_forecast)
            if config['model']=='gpt-6-astra' and args.cohort=='full200':
                with ledger.connect() as db:config['stage_usd']=db.execute('SELECT cap FROM stages WHERE label=?',(config.get('cache_phase_override',args.phase)+':'+config['model'],)).fetchone()[0]/1e6
        adapter=StageAdapter(Adapter(config,ledger,state/'cache',**({'env_path':args.env_file} if args.env_file else {})),config.get('stage_usd',1),clock['inference_deadline_epoch'])
        hours=min(args.hours,max(0,(clock['inference_deadline_epoch']-time.time())/3600))
        print(canonical(run(cases,adapter,state/(args.phase+'.jsonl'),args.phase,prompt=prompt,hours=hours)))
        print(canonical(ledger.summary()))

if __name__=='__main__':main()
