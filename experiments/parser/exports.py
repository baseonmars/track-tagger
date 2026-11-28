"""Public structured metadata exports; original note text and evidence quotes stay external."""
import hashlib
import json
import re
from pathlib import Path
from .hosted import canonical,usage_cost
from .runner import read_cases


def sha(value):return hashlib.sha256(canonical(value).encode()).hexdigest()

def redact_strings(value):
    redactions=[]
    def clean(item,path):
        if isinstance(item,str):
            if len(item)>512 or '\n' in item or '\r' in item:
                digest=hashlib.sha256(item.encode()).hexdigest()
                redactions.append({'path':path,'sha256':digest,'characters':len(item),'reason':'long-or-multiline-string'})
                return '[redacted sha256:'+digest+']'
            return item
        if isinstance(item,list):return [clean(v,path+'['+str(i)+']') for i,v in enumerate(item)]
        if isinstance(item,dict):
            result={}
            for key,v in item.items():
                safe_key=key
                if isinstance(key,str) and (len(key)>512 or '\n' in key or '\r' in key):
                    safe_key='[redacted-key sha256:'+hashlib.sha256(key.encode()).hexdigest()+']'
                    redactions.append({'path':path+'.<key>','sha256':hashlib.sha256(key.encode()).hexdigest(),'characters':len(key),'reason':'long-or-multiline-key'})
                result[safe_key]=clean(v,path+'.'+str(safe_key))
            return result
        return item
    result=clean(value,'row')
    if redactions:result['public_export_redactions']=redactions
    return result

def dequote(oracle,notes=None):
    if not isinstance(oracle,dict):return None
    out={k:oracle[k] for k in ('artist','date','venue','city','titles','mappings','review') if k in oracle}
    out['unresolved']=[x for x in oracle.get('unresolved',[]) if re.fullmatch(r'[1-9]\d*:[1-9]\d*',x)]
    out['unresolved_full_sha256']=sha(oracle.get('unresolved',[]))
    sources={n['name']:n['text'] for n in (notes or [])}
    references=[]
    for item in oracle.get('evidence',[]):
        quote=item['quote'];text=sources.get(item['note'],'');offsets=[];start=0
        while quote:
            index=text.find(quote,start)
            if index<0:break
            offsets.append({'start_line':text.count('\n',0,index)+1,'end_line':text.count('\n',0,index+len(quote))+1})
            start=index+len(quote)
        references.append({'field':item['field'],'note':item['note'],'quote_sha256':hashlib.sha256(quote.encode()).hexdigest(),'line_offsets':offsets})
    out['evidence_references']=references
    return out


def gold(frozen_labels,output):
    cases=read_cases(frozen_labels)
    rows=[]
    try:selection=__import__('json').loads(Path(frozen_labels).read_text())
    except ValueError:selection={}
    for case in cases:
        row={k:case[k] for k in ('id','split','group','features','label_status')}
        row.update(files=case['files'],selected_common40=case['id'] in selection.get('astra_ids',[]),selected_pilot30=case['id'] in selection.get('pilot_ids',[]))
        row.update(source_input_sha256=sha({'notes':case['notes'],'files':case['files']}),
                   oracle=dequote(case.get('oracle'),case['notes']),
                   full_oracle_sha256=sha(case.get('oracle')))
        audit=case.get('annotation_review') or {}
        row['annotation_review']={k:audit[k] for k in ('reviewer_model','requested_model','prompt_sha256','config_sha256','source_sha256','draft_sha256','response_sha256','not_human_certified','verdict') if k in audit}
        rows.append(redact_strings(row))
    Path(output).write_text(''.join(canonical(r)+'\n' for r in rows))
    return {'cases':len(rows),'sha256':hashlib.sha256(Path(output).read_bytes()).hexdigest()}


def predictions(results,output,requested_model=None):
    rows=[]
    for original in read_cases(results):
        allowed={'id','phase','latency_seconds','inference_latency_seconds','baseline_latency_seconds','usage','cache_hit','features','actual_model','settings','hybrid_used_fallback','accuracy_eligible','evidence_score','raw_score','validated_score','error','failure','simulated_routing','score_version','cost_usd','cost_method','resources','requested_model','request_attempt','accounting_version','cache_record_sha256','cache_cost_recomputed_without_settlement','original_reported_cost_usd'}
        row={k:v for k,v in original.items() if k in allowed}
        if isinstance(row.get('failure'),dict):
            failure=row['failure'];safe={'kind','http_status','error_code','error_type','status','refusal','incomplete_reason','rate_limit_headers'}
            row['failure']={k:v for k,v in failure.items() if k in safe}
            if failure.get('raw_response_path'):row['failure']['raw_response_external']=True
        if requested_model:row['requested_model']=requested_model
        model=requested_model or row.get('actual_model','')
        rates={'gpt-6-luna':(.1,.5),'gpt-6.1-sol':(2,10),'gpt-6-astra':(10,50)}
        matched=next((name for name in rates if model and model.startswith(name)),None)
        if matched and row.get('usage'):
            try:
                cost,method=usage_cost(row['usage'],{'model':matched,'input_per_million':rates[matched][0],'output_per_million':rates[matched][1]})
                row.update(original_reported_cost_usd=row.get('cost_usd'),cost_usd=float(cost),cost_method=method,accounting_version=2)
            except ValueError:row['cost_method']='unresolved-usage-retain-ledger-ceiling'
        for key in ('raw','validated'):
            if key in original:row[key]=dequote(original[key])
        row['full_prediction_sha256']=sha(original)
        rows.append(redact_strings(row))
    Path(output).write_text(''.join(canonical(r)+'\n' for r in rows))
    manifest_path=Path(results).with_suffix(Path(results).suffix+'.manifest.json')
    if manifest_path.exists():
        original=__import__('json').loads(manifest_path.read_text())
        allowed={'fingerprint','requested_ids','eligible_label_ids','deadline_epoch','score_version','original_results_sha256','frozen_labels_sha256','scorer_sha256'}
        manifest={k:v for k,v in original.items() if k in allowed}
        manifest.update(public_prediction_sha256=hashlib.sha256(Path(output).read_bytes()).hexdigest(),stored_per_case_scores_preserved=True,redacted_case_ids=[r['id'] for r in rows if r.get('public_export_redactions')])
        Path(output).with_suffix(Path(output).suffix+'.manifest.json').write_text(canonical(manifest)+'\n')
    return {'cases':len(rows),'sha256':hashlib.sha256(Path(output).read_bytes()).hexdigest()}
