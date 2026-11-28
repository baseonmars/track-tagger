"""Seal separate recording/line reference components before candidate inference."""
import argparse
import copy
from collections import Counter
import hashlib
import json
from pathlib import Path
from ..path_guard import require_external
from ..acquire import atomic_json
from ..hosted import canonical, SCHEMA
from ..source_review import validate_review
from .labels import flatten,lines,source_hash

def digest(value):return hashlib.sha256(canonical(value).encode()).hexdigest()

def unknown_lines(case,reason):
    rows=[]
    for note in case['notes']:
        text=lines(note['text'])
        for index,line in enumerate(text):
            blank=not line.strip()
            rows.append({'id':case['id'],'group':case['group'],'split':case['split'],'note':note['name'],'line_index':index,'line':line,'prev':text[index-1]if index else'','next':text[index+1]if index+1<len(text)else'','roles':['blank']if blank else[],'label_status':'agent-reviewed'if blank else'unresolved','source_input_sha256':source_hash(case),'uncertainty':False,'conflict':False,'mixed_scope':False,'role_disputed':not blank,'spans':[],'label_origin':'deterministic-source-whitespace'if blank else'annotation-unavailable','reference_issue':reason})
    return rows

def blank_convention(case,annotation):
    """Whitespace-only lines are mechanically known, never invented negatives."""
    value=copy.deepcopy(annotation);source={n['name']:lines(n['text'])for n in case['notes']};segments=[];seen=set();changes=[]
    for segment in value['segments']:
        note=segment['note'];start=segment['start_line'];end=segment['end_line']
        if note not in source or type(start)is not int or type(end)is not int or not 0<=start<end<=len(source[note]):raise ValueError('invalid source interval')
        for i in range(start,end):
            if(note,i)in seen:raise ValueError('duplicate source coverage')
            seen.add((note,i));s={**segment,'start_line':i,'end_line':i+1,'spans':[sp for sp in segment['spans']if sp['line_index']==i]}
            if not source[note][i].strip():
                if s['roles']!=['blank']or s['spans']:changes.append({'note':note,'line_index':i,'action':'source-whitespace-blank-convention'})
                s.update(roles=['blank'],uncertainty=False,conflict=False,mixed_scope=False,role_disputed=False,spans=[])
            segments.append(s)
    for note,text in source.items():
        for i,line in enumerate(text):
            if(note,i)not in seen and not line.strip():
                segments.append({'note':note,'start_line':i,'end_line':i+1,'roles':['blank'],'uncertainty':False,'conflict':False,'mixed_scope':False,'role_disputed':False,'spans':[]});changes.append({'note':note,'line_index':i,'action':'explicit-source-whitespace-blank-completion'})
    value['segments']=segments
    return value,changes

def canonical_oracle(annotation):
    oracle={k:copy.deepcopy(annotation[k])for k in SCHEMA['required']};known_ids={}
    for name,mapping in oracle['mappings'].items():
        if not mapping['title']and(mapping['disc']or mapping['number']):
            known_ids[name]={'disc':mapping['disc'],'number':mapping['number']};mapping.update(disc=0,number=0)
    return oracle,known_ids

def promote(cases,review_inputs,review_rows):
    source={c['id']:c for c in cases};inputs={c['id']:c for c in review_inputs};reviews={r['id']:r for r in review_rows}
    if len(source)!=len(cases)or len(inputs)!=len(review_inputs)or len(reviews)!=len(review_rows)or set(reviews)-set(source)or set(inputs)!=set(source):raise ValueError('duplicate/foreign/missing source review membership')
    for id,case in source.items():
        reviewed=inputs[id]
        if (case['group'],case['split'],source_hash(case))!=(reviewed['group'],reviewed['split'],source_hash(reviewed)):raise ValueError('source review inputs changed frozen membership or text')
    recording=[];line_rows=[];audit=[]
    for id,case in sorted(source.items()):
        row=reviews.get(id);annotation=(row or{}).get('annotation');draft=inputs[id].get('annotation_draft')or{};has_author=bool(draft.get('segments'));line_issue=record_issue=None;derived=[];oracle=None;known_ids={}
        if row and row.get('source_input_sha256')!=source_hash(case):raise ValueError('source review hash changed')
        if not annotation or not has_author:
            line_issue=record_issue='missing-independent-source-pass'if annotation else'no-source-review-output'
        elif annotation.get('verdict')not in ('accepted','corrected'):
            line_issue=record_issue='source-review-disputed'
        else:
            try:
                view,derived=blank_convention(case,annotation);flat=flatten(case,view,'agent-reviewed')
                for r in flat:r['label_origin']='independent-source-review'
                if annotation['verdict']=='accepted':
                    original,_=blank_convention(case,draft);prior=flatten(case,original,'draft')
                    signature=lambda rows:[(r['note'],r['line_index'],sorted(r['roles']),r['uncertainty'],r['conflict'],r['mixed_scope'],r['role_disputed'])for r in rows]
                    if signature(flat)!=signature(prior):raise ValueError('accepted verdict changed source line labels')
            except (ValueError,KeyError,TypeError)as exc:line_issue=type(exc).__name__+':'+str(exc)
            try:
                oracle,known_ids=canonical_oracle(annotation)
                issues=validate_review(case,{}, {'verdict':'corrected','oracle':oracle,'reasons':[]})
                if issues:raise ValueError(','.join(issues))
                if annotation['verdict']=='accepted':
                    prior,_=canonical_oracle(draft)
                    if any(oracle[k]!=prior[k]for k in ('artist','date','venue','city','titles','mappings','review')):raise ValueError('accepted verdict changed recording labels')
            except (ValueError,KeyError,TypeError)as exc:record_issue=type(exc).__name__+':'+str(exc)
        if line_issue:flat=unknown_lines(case,line_issue)
        line_rows.extend(flat)
        label={'id':id,'source_input_sha256':source_hash(case),'label_status':'agent-reviewed'if not record_issue else'unresolved','metadata':{k:oracle[k]for k in ('artist','date','venue','city')}if not record_issue else{k:''for k in ('artist','date','venue','city')},'review_required':bool(oracle['review'])if not record_issue else True,'source_review_sha256':digest(row),'source_author_response_sha256':inputs[id].get('source_author_response_sha256'),'human_certified':False}
        if not record_issue:label.update(titles=oracle['titles'],mappings=oracle['mappings'])
        recording.append(label);audit.append({'id':id,'recording_reference_issue':record_issue,'line_reference_issue':line_issue,'derived_blank_conventions':derived,'known_file_ids_separate_from_unknown_join':known_ids,'original_review_verdict':annotation.get('verdict')if annotation else None,'source_author_had_output':has_author,'source_review_status':row.get('label_status')if row else'missing-request','candidate_predictions_used':False})
    return recording,line_rows,audit

def freeze(cases_path,inputs_path,reviews_path,output):
    output=require_external(output)
    if output.exists():raise ValueError('gold output already exists')
    value=json.loads(cases_path.read_text());cases=value['cases'];inputs=json.loads(inputs_path.read_text())['cases'];reviews=[json.loads(l)for l in reviews_path.read_text().split('\n')if l.strip()]
    recording,line_rows,audit=promote(cases,inputs,reviews)
    output.mkdir(parents=True)
    for name,rows in [('recording-gold.jsonl',recording),('line-gold.jsonl',line_rows),('source-review-audit.jsonl',audit)]:
        (output/name).write_text(''.join(canonical(r)+'\n'for r in rows))
    counts=Counter(r['label_status']for r in recording);nonblank=[r for r in line_rows if r['line'].strip()];known=sum(r['label_status']=='agent-reviewed'and not r['role_disputed']for r in nonblank)
    report={'source_cases':len(cases),'recording_status_counts':dict(counts),'lines':len(line_rows),'nonblank_lines':len(nonblank),'reviewed_nonblank_lines':known,'reviewed_nonblank_fraction':known/max(1,len(nonblank)),'recording_gold_sha256':digest(recording),'line_gold_sha256':digest(line_rows),'source_review_artifact_sha256':hashlib.sha256(reviews_path.read_bytes()).hexdigest(),'source_inputs_sha256':hashlib.sha256(cases_path.read_bytes()).hexdigest(),'gold_frozen_before_candidate_predictions':True,'human_certified':False,'independent_contexts_not_independent_model_families':True}
    atomic_json(output/'gold-freeze.json',report);return report

def main():
    p=argparse.ArgumentParser();p.add_argument('cases',type=Path);p.add_argument('review_inputs',type=Path);p.add_argument('reviews',type=Path);p.add_argument('output',type=Path);a=p.parse_args();print(canonical(freeze(a.cases,a.review_inputs,a.reviews,a.output)))

if __name__=='__main__':main()
