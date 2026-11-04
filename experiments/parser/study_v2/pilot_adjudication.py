"""Apply the coordinating agent's source-reviewed TRAINING-pilot corrections.

These named corrections precede fresh held-out labels and predictions. Original
provider responses/checkpoints are immutable; output is a separate audit.
"""
import copy
import hashlib
import json
from pathlib import Path
from ..acquire import atomic_json
from ..hosted import canonical
from ..source_review import validate_review
from .labels import flatten,lines

def adjudicate(root):
    packet=json.loads((root/'pilot20-source-adjudication-packet.json').read_text())
    rows=[]
    for case in packet['cases']:
        if case['split']!='train':raise ValueError('these corrections are training-pilot only')
        before=case['review_attempt'].get('annotation');value=copy.deepcopy(before)
        id=case['id'];changes=[];known_ids={}
        if id in ('InfamousStringdusters2010-10-15','Ween1997-11-27.sbd.remaster.flac16'):
            for path,mapping in value['mappings'].items():
                if not mapping['title']and(mapping['disc']or mapping['number']):
                    known_ids[path]={'disc':mapping['disc'],'number':mapping['number']}
                    mapping.update(disc=0,number=0)
            value['review']=True;changes.append('Canonical unknown JOIN is0/0/empty; source-known fileIDs retained separately. Schema limitation, not invented source facts.')
        elif id=='sp2013-04-13.flac16':
            value['date']='';value['titles']={};value['mappings']={path:{'disc':0,'number':0,'title':''}for path in case['files']};value['review']=True
            value['evidence']=[e for e in value['evidence']if e['field']not in ('date',)and not e['field'].startswith('titles.')]
            value['unresolved']=list(value['unresolved'])+['Conflicting2012/2013 source date; disc assignment not established; checksum-only filename is not actual inventory.']
            changes.append('Remove foreign checksum-only mapping; no inferred discIDs; preserve unassigned title-role lines; conflicting source date remains unknown/review.')
        elif id=='tod2005-02-27.flac16':
            covered={(s['note'],i)for s in value['segments']for i in range(s['start_line'],s['end_line'])}
            for note in case['notes']:
                for i,line in enumerate(lines(note['text'])):
                    if(note['name'],i)not in covered:
                        if line.strip():raise ValueError('source audit only authorized explicit blank completion')
                        value['segments'].append({'note':note['name'],'start_line':i,'end_line':i+1,'roles':['blank'],'uncertainty':False,'conflict':False,'mixed_scope':False,'role_disputed':False,'spans':[]})
            changes.append('Explicitly source-reviewed missing terminal blank line; no arbitrary nonblank completion.')
        elif id=='BrettDennen2013-11-11.flac24':
            value={'artist':'','date':'','venue':'','city':'','titles':{},'mappings':{p:{'disc':0,'number':0,'title':''}for p in case['files']},'review':True,'unresolved':['RTF disguised as .txt; unsupported text representation requires review. Provider source-author/review failures remain recorded.'],'evidence':[],'segments':[]}
            for note in case['notes']:
                for i,line in enumerate(lines(note['text'])):
                    value['segments'].append({'note':note['name'],'start_line':i,'end_line':i+1,'roles':['blank']if not line.strip()else [],'uncertainty':bool(line.strip()),'conflict':False,'mixed_scope':False,'role_disputed':bool(line.strip()),'spans':[]})
            changes.append('UnsupportedRTF case retained for review; nonblank role truth disputed, not mislabeled other/background.')
        else:raise ValueError('unreviewed training-pilot ID')
        value.pop('verdict',None)
        line_rows=flatten(case,value,'agent-adjudicated')
        oracle={k:v for k,v in value.items()if k!='segments'}
        issues=validate_review(case,{}, {'verdict':'corrected','oracle':oracle,'reasons':changes})
        if issues:raise ValueError('adjudication still invalid:'+','.join(issues))
        rows.append({'id':id,'source_input_sha256':case['source_input_sha256'],'label_status':'agent-adjudicated','annotation':value,'line_count':len(line_rows),'known_file_id_evidence':known_ids,'changes':changes,'reviewer':'coordinating Astra agent source-only audit','human_certified':False,'original_review_sha256':hashlib.sha256(canonical(case['review_attempt']).encode()).hexdigest(),'candidate_predictions_used':False,'training_only':True})
    atomic_json(root/'pilot20-source-adjudication.json',{'cases':rows,'original_provider_failures_retained':True})
    return rows

if __name__=='__main__':
    import sys
    print(canonical({'adjudicated_training_cases':len(adjudicate(Path(sys.argv[1])))}))
