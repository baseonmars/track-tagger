"""Private source-audit packets; never changes frozen gold or primary scores."""
import argparse
import hashlib
from pathlib import Path
from .runner import read_cases
from .hosted import canonical

def packet(predictions,labels,output,model):
    output=Path(output).resolve()
    if any((p/'.git').exists() for p in [output.parent,*output.parents]):raise ValueError('source audit with original evidence must remain external')
    cases={c['id']:c for c in read_cases(labels)};selected=[]
    for row in read_cases(predictions):
        case=cases[row['id']];eligible=row.get('accuracy_eligible',False);value=row.get('validated');flags=[]
        if value is None:continue
        score=row.get('validated_score',{})
        if eligible:
            if score.get('false_ready'):flags.append('false-ready-reference-expects-review')
            if score.get('incorrect_accepted_mappings'):flags.append('strict-accepted-mapping-difference')
            if not value.get('review',True) and not score.get('exact'):flags.append('accepted-nonexact-proxy')
        elif not value.get('review',True):flags.append('acceptance-with-unresolved-reference')
        if not flags:continue
        selected.append({'id':row['id'],'requested_model':model,'flags':flags,'reference_status':case.get('label_status'),
                         'validated_output':value,'frozen_reference':case.get('oracle'),'primary_metrics':score,
                         'source_case_file':str(Path(labels).resolve()),'source_package_path':case.get('provenance',{}).get('package_path'),
                         'note_names':[n['name'] for n in case['notes']],'files':case['files'],
                         'frozen_gold_unchanged':True,'source_case_input_sha256':hashlib.sha256(canonical({'notes':case['notes'],'files':case['files']}).encode()).hexdigest()})
    output.parent.mkdir(parents=True,exist_ok=True);output.write_text(''.join(canonical(r)+'\n' for r in selected))
    return {'cases':len(selected),'ids':[r['id'] for r in selected],'sha256':hashlib.sha256(output.read_bytes()).hexdigest()}

def main():
    p=argparse.ArgumentParser();p.add_argument('predictions');p.add_argument('labels');p.add_argument('external_output');p.add_argument('--model',required=True);a=p.parse_args();print(canonical(packet(a.predictions,a.labels,a.external_output,a.model)))
if __name__=='__main__':main()
