"""Post-freeze diagnostics only: never changes models, labels or primary scores."""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import subprocess
from .labels import ROLES
from ..path_guard import require_external

def read_rows(path):
    # JSONL is delimited by physical LF, not Unicode separators inside strings.
    with Path(path).open(encoding='utf-8')as stream:return [json.loads(s)for s in stream if s.strip()]
def key(r):return r['id'],r['note'],r['line_index']
def diagnose(gold,predictions,enabled):
    if len({key(r)for r in gold})!=len(gold)or len({key(r)for r in predictions})!=len(predictions):raise ValueError('duplicate line identity')
    by={key(p):p for p in predictions}
    if set(by)!={key(g)for g in gold}:raise ValueError('prediction/gold membership differs')
    counts={r:dict(tp=0,fp=0,fn=0)for r in ROLES};confusion=Counter();abstentions=Counter();unknown=0;raw_emitted=0;metadata_emitted=0;exact=0;reviewed=0
    for g in gold:
        p=by[key(g)];roles=set(p['roles'])if not p.get('abstention')and not p.get('error')else set();raw_emitted+=bool(roles);metadata_emitted+=bool(roles&set(enabled));abstentions[p.get('abstention')or('error'if p.get('error')else'diagnostic-emitted')]+=1
        if g.get('role_disputed')or g.get('label_status')not in('agent-reviewed','adjudicated'):unknown+=1;continue
        reviewed+=1;truth=set(g['roles']);exact+=roles==truth
        for r in ROLES:
            counts[r]['tp']+=int(r in roles and r in truth);counts[r]['fp']+=int(r in roles and r not in truth);counts[r]['fn']+=int(r in truth and r not in roles)
        for predicted in roles-truth:
            for expected in truth:confusion[expected+' -> '+predicted]+=1
    metrics={}
    for r,c in counts.items():
        tp,fp,fn=c['tp'],c['fp'],c['fn'];metrics[r]={**c,'precision':tp/(tp+fp)if tp+fp else None,'recall':tp/(tp+fn)if tp+fn else None,'f1':2*tp/(2*tp+fp+fn)if 2*tp+fp+fn else None,'expected_positive_lines':tp+fn,'predicted_positive_lines':tp+fp}
    return {'schema_version':1,'postfreeze_diagnostic_only':True,'requested_lines':len(gold),'reviewed_nondisputed_lines':reviewed,'unresolved_role_lines':unknown,'raw_all_11_role_emitted_lines':raw_emitted,'raw_classifier_abstention_lines':len(gold)-raw_emitted,'raw_abstention_reasons':dict(abstentions),'enabled_metadata_emitted_lines':metadata_emitted,'no_enabled_metadata_emission_lines':len(gold)-metadata_emitted,'enabled_metadata_roles':list(enabled),'exact_multilabel_lines_reviewed':exact,'all_role_metrics':metrics,'incorrect_role_cooccurrence':dict(confusion.most_common()),'confusion_note':'Multilabel false-role cooccurrences, not an exclusive single-label confusion matrix; a line may contribute multiple pairs.','macro_f1_all_11_roles':sum(m['f1']or 0 for m in metrics.values())/len(ROLES),'primary_scores_unchanged':True}

def replay(gold,predictions,scorer,model):
    payload=''.join(json.dumps({k:r[k]for k in('line','prev','next')},ensure_ascii=False)+'\n'for r in gold);output=subprocess.check_output([str(scorer),'--model',str(model)],input=payload,text=True)
    actual=[json.loads(s)for s in output.split('\n')if s];by={key(p):p for p in predictions};max_error=0.;differences=[]
    if len(actual)!=len(gold):raise ValueError('replay coverage differs')
    for g,a in zip(gold,actual):
        p=by[key(g)]
        if any(a.get(k)!=p.get(k)for k in('roles','abstention','known_features','total_features','model_id'))or set(a['scores'])!=set(p['scores']):differences.append(key(g))
        for r,v in a['scores'].items():max_error=max(max_error,abs(v-p['scores'][r]))
    return {'replayed_lines':len(actual),'structural_differences':len(differences),'maximum_absolute_score_difference':max_error,'pass':not differences and max_error<1e-12,'model_sha256':hashlib.sha256(Path(model).read_bytes()).hexdigest(),'scorer_sha256':hashlib.sha256(Path(scorer).read_bytes()).hexdigest()}
def main():
    p=argparse.ArgumentParser();p.add_argument('gold',type=Path);p.add_argument('predictions',type=Path);p.add_argument('model_freeze',type=Path);p.add_argument('output',type=Path);p.add_argument('--scorer',type=Path);p.add_argument('--model',type=Path);a=p.parse_args();gold=read_rows(a.gold);pred=read_rows(a.predictions);freeze=json.loads(a.model_freeze.read_text());result=diagnose(gold,pred,freeze['enabled_metadata_roles']);result['input_sha256']={path.name:hashlib.sha256(path.read_bytes()).hexdigest()for path in(a.gold,a.predictions,a.model_freeze)}
    if a.scorer and a.model:result['Go_replay_parity']=replay(gold,pred,a.scorer,a.model)
    output=require_external(a.output)
    with output.open('x')as stream:stream.write(json.dumps(result,sort_keys=True)+'\n')
    print(json.dumps({'output':str(a.output),'sha256':hashlib.sha256(a.output.read_bytes()).hexdigest(),'parity':result.get('Go_replay_parity')}))
if __name__=='__main__':main()
