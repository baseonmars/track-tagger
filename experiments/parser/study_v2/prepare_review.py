"""Bind independent source-author artifacts to complete source review inputs."""
import argparse
import hashlib
import json
from pathlib import Path
from ..acquire import atomic_json
from ..hosted import canonical
from .run_labels import config,payload_for
from .labels import source_hash,flatten

def prepare(cases,root,author_phase,model='sol',max_output=8192):
    c=config(model,author_phase,max_output);rows=[];audit=[]
    for case in cases:
        body=canonical(payload_for(case,c)).encode()
        key=hashlib.sha256(author_phase.encode()+b'\0'+body).hexdigest()
        path=root/'response-cache'/(key+'.json')
        if not path.exists():raise ValueError('source author response missing for '+case['id'])
        response=json.loads(path.read_text());draft=response.get('output')
        strict_valid=False;issue=None
        if draft:
            try:flatten(case,draft);strict_valid=True
            except ValueError as exc:issue=str(exc)
        rows.append({**case,'annotation_draft':draft or {'source_author_status':'failed_without_output','failure':response.get('failure')},'source_author_response_sha256':hashlib.sha256(path.read_bytes()).hexdigest()})
        audit.append({'id':case['id'],'source_input_sha256':source_hash(case),'source_author_response_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'author_has_output':bool(draft),'strict_line_reaudit_valid':strict_valid,'strict_line_reaudit_issue':issue,'source_author_phase':author_phase})
    return rows,audit

def main():
    p=argparse.ArgumentParser();p.add_argument('cases',type=Path);p.add_argument('root',type=Path);p.add_argument('author_phase');p.add_argument('output',type=Path);p.add_argument('--model',default='sol');a=p.parse_args();value=json.loads(a.cases.read_text());cases=value['cases']if isinstance(value,dict)else value
    rows,audit=prepare(cases,a.root,a.author_phase,a.model);atomic_json(a.output,{'cases':rows,'source_author_audit':audit});print(canonical({'cases':len(rows),'author_outputs':sum(r['author_has_output']for r in audit),'strict_line_valid':sum(r['strict_line_reaudit_valid']for r in audit)}))

if __name__=='__main__':main()
