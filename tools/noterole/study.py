"""Fresh-study train-only candidates and validation-only selection.

No test input is accepted. Source-line features are produced exclusively by Go.
Candidate JSON and sparse matrices remain external until privacy/artifact review.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
from train import ROLES, VERSION

PROTOCOL={'version':1,'C':[.1,1.,10.],'class_weight':'balanced','solver':'liblinear','random_state':42,'max_iter':1000,'threshold_grid':[.5,.6,.7,.8,.9,.95,.99],'metadata_roles':['artist','date','venue','city'],'enabled_role_min_predicted_lines':10,'enabled_role_min_predicted_records':3,'enabled_role_precision':.95,'threshold_selection':'maximum recall satisfying support and precision; ties higher threshold','model_selection':'maximum validation macro-F1 all roles, then metadata micro-precision, then smaller C','test_not_used':True,'runtime_margin':'min(0.05, threshold/2, (1-threshold)/2)','threshold_search':'per-role emitted-positive including margin and source OOV; final global runtime mask recalculated without retuning','final_role_eligibility':'actual emitted precision/support after all global abstention; disable affects proposal eligibility only','model_selection_score':'actual Go-equivalent emitted roles, not raw probabilities'}
def canonical(v):return json.dumps(v,sort_keys=True,ensure_ascii=False,separators=(',',':'),allow_nan=False)
def digest(v):return hashlib.sha256(canonical(v).encode()).hexdigest()
def write(path,v):Path(path).write_text(canonical(v)+'\n')
def read_rows(path):return [json.loads(s)for s in Path(path).read_text().splitlines()if s]
def validate(rows,split):
    if not rows or any(r.get('split')!=split for r in rows):raise ValueError('only expected split permitted')
    identities=set()
    for r in rows:
        key=(r['id'],r['note'],r['line_index'])
        if key in identities:raise ValueError('duplicate source line')
        identities.add(key)
        if not r.get('group')or len(r.get('source_input_sha256',''))!=64:raise ValueError('source provenance required')
        if not r.get('role_disputed')and(not r.get('roles')or not set(r['roles'])<=set(ROLES)):raise ValueError('explicit complete role targets required')
        if any(len(r[k])>16384 for k in ('line','prev','next')):raise ValueError('source line exceeds Go input bound')
def features(rows,extractor):
    payload=''.join(canonical({k:r[k]for k in ('line','prev','next')})+'\n'for r in rows)
    p=subprocess.run([str(extractor)],input=payload,text=True,capture_output=True,check=True)
    result=[json.loads(s)for s in p.stdout.splitlines()]
    if len(result)!=len(rows)or any(f['feature_version']!=VERSION for f in result):raise ValueError('Go feature contract mismatch')
    return [f['features']for f in result]
def matrix(features,vocabulary):
    from scipy.sparse import csr_matrix
    index={k:i for i,k in enumerate(vocabulary)};data=[];cols=[];ptr=[0]
    for f in features:
        for k,v in sorted(f.items()):
            if k in index:cols.append(index[k]);data.append(v)
        ptr.append(len(data))
    return csr_matrix((data,cols,ptr),shape=(len(features),len(vocabulary)),dtype='float64')
def fit(rows,extractor,out,source_artifact_sha256=None):
    from sklearn.linear_model import LogisticRegression
    import numpy as np
    validate(rows,'train');out=Path(out);out.mkdir(parents=True,exist_ok=True)
    if(out/'training-manifest.json').exists():raise ValueError('training already sealed')
    selected=[r for r in rows if not r.get('role_disputed')];fs=features(selected,extractor);counts={}
    for f in fs:
        for k,v in f.items():
            if len(k.encode())<=4096:counts[k]=counts.get(k,0)+v
    vocabulary=sorted(sorted(counts,key=lambda k:(-counts[k],k))[:30000]);x=matrix(fs,vocabulary);train_hash=digest(rows);support={};candidates=[]
    for C in PROTOCOL['C']:
        heads=[]
        for role in ROLES:
            y=np.array([int(role in r['roles'])for r in selected]);support[role]=int(y.sum())
            if len(set(y))!=2:raise ValueError('role lacks positive/negative training support: '+role)
            model=LogisticRegression(C=C,class_weight='balanced',solver='liblinear',max_iter=1000,random_state=42).fit(x,y)
            heads.append({'role':role,'weights':[0. if v==0 else float(v)for v in model.coef_[0]],'intercept':0. if model.intercept_[0]==0 else float(model.intercept_[0]),'threshold':.99,'margin':.05})
        artifact={'schema_version':1,'feature_version':VERSION,'model_id':'study-v2-candidate-C'+str(C),'training_sha256':train_hash,'vocabulary':vocabulary,'heads':heads};path=out/('candidate-C'+str(C)+'.json');write(path,artifact)
        subprocess.run([str(extractor),'--model',str(path)],input=canonical({k:selected[0][k]for k in ('line','prev','next')})+'\n',text=True,capture_output=True,check=True)
        candidates.append({'C':C,'path':path.name,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()})
    manifest={'protocol':PROTOCOL,'protocol_sha256':digest(PROTOCOL),'training_rows_sha256':train_hash,'training_source_artifact_sha256':source_artifact_sha256,'training_records':len({r['id']for r in rows}),'total_lines':len(rows),'eligible_lines':len(selected),'role_disputed_lines':len(rows)-len(selected),'role_positive_lines':support,'training_groups':sorted({r['group']for r in rows}),'extractor_sha256':hashlib.sha256(Path(extractor).read_bytes()).hexdigest(),'candidates':candidates,'feature_version':VERSION}
    write(out/'training-manifest.json',manifest);return manifest
def select(validation,extractor,out):
    import numpy as np
    from scipy.special import expit
    validate(validation,'validation');out=Path(out);manifest=json.loads((out/'training-manifest.json').read_text())
    if manifest['protocol_sha256']!=digest(PROTOCOL):raise ValueError('selection protocol changed')
    if hashlib.sha256(Path(extractor).read_bytes()).hexdigest()!=manifest['extractor_sha256']:raise ValueError('extractor changed')
    if set(r['group']for r in validation)&set(manifest['training_groups']):raise ValueError('training/validation group leakage')
    if(out/'model-freeze.json').exists():raise ValueError('model already frozen')
    rows=[r for r in validation if not r.get('role_disputed')];fs=features(rows,extractor);results=[]
    for candidate in manifest['candidates']:
        path=out/candidate['path']
        if hashlib.sha256(path.read_bytes()).hexdigest()!=candidate['sha256']:raise ValueError('candidate changed')
        artifact=json.loads(path.read_text());x=matrix(fs,artifact['vocabulary']);role_stats={};enabled=[];f1s=[];micro_tp=0;micro_fp=0
        probabilities={}
        vocabulary_set=set(artifact['vocabulary'])
        source_known=np.array([any((k.startswith('w:')or k.startswith('c:'))and k in vocabulary_set for k in f)for f in fs])
        for head in artifact['heads']:
            role=head['role'];truth=np.array([role in r['roles']for r in rows]);prob=expit(x.dot(np.array(head['weights']))+head['intercept']);probabilities[role]=prob;options=[]
            for threshold in PROTOCOL['threshold_grid']:
                margin=min(.05,threshold/2,(1-threshold)/2)
                pred=(prob>=threshold)&(np.abs(prob-threshold)>=margin)&source_known
                tp=int(sum(pred&truth));fp=int(sum(pred&~truth));fn=int(sum(~pred&truth));records=len({r['id']for r,p in zip(rows,pred)if p});precision=tp/max(1,tp+fp);recall=tp/max(1,tp+fn)
                if tp+fp>=10 and records>=3 and precision>=.95:options.append((recall,threshold))
            threshold=max(options)[1]if options else .99
            head['threshold']=threshold;head['margin']=min(.05,threshold/2,(1-threshold)/2)
        # Reproduce all Go abstention before reporting selection metrics. Disabled
        # metadata roles remain scored: disabling only controls proposal use.
        uncertain=np.zeros(len(rows),dtype=bool);votes={}
        for head in artifact['heads']:
            p=probabilities[head['role']];uncertain|=np.abs(p-head['threshold'])<head['margin'];votes[head['role']]=p>=head['threshold']
        vote_count=sum(votes.values());incompatible=(votes['other']|votes['blank'])&(vote_count>1)
        emitted=source_known&~uncertain&~incompatible
        for head in artifact['heads']:
            role=head['role'];truth=np.array([role in r['roles']for r in rows]);pred=votes[role]&emitted;tp=int(sum(pred&truth));fp=int(sum(pred&~truth));fn=int(sum(~pred&truth));precision=tp/max(1,tp+fp);recall=tp/max(1,tp+fn);f1=2*tp/max(1,2*tp+fp+fn);f1s.append(f1)
            records=len({r['id']for r,p in zip(rows,pred)if p});qualified=tp+fp>=10 and records>=3 and precision>=.95
            if role in PROTOCOL['metadata_roles']:
                micro_tp+=tp;micro_fp+=fp
                if qualified:enabled.append(role)
            role_stats[role]={'threshold':head['threshold'],'margin':head['margin'],'validation_qualified':qualified,'predicted_positive_lines':tp+fp,'predicted_positive_records':records,'expected_positive_lines':int(sum(truth)),'true_positive':tp,'false_positive':fp,'false_negative':fn,'precision':precision,'recall':recall,'f1':f1,'metric':'actual Go-equivalent emission after all global abstention'}
        results.append({'C':candidate['C'],'artifact':artifact,'roles':role_stats,'enabled_metadata_roles':enabled,'macro_f1':sum(f1s)/len(f1s),'metadata_micro_precision':micro_tp/max(1,micro_tp+micro_fp)})
    winner=max(results,key=lambda r:(r['macro_f1'],r['metadata_micro_precision'],-r['C']));artifact=winner.pop('artifact');artifact['model_id']='study-v2-frozen-C'+str(winner['C']);model_path=out/'model.json';write(model_path,artifact)
    subprocess.run([str(extractor),'--model',str(model_path)],input=canonical({k:rows[0][k]for k in ('line','prev','next')})+'\n',text=True,capture_output=True,check=True)
    for result in results:result.pop('artifact',None)
    freeze={'schema_version':1,'protocol':PROTOCOL,'protocol_sha256':digest(PROTOCOL),'training_manifest_sha256':hashlib.sha256((out/'training-manifest.json').read_bytes()).hexdigest(),'validation_rows_sha256':digest(validation),'validation_requested_lines':len(validation),'validation_eligible_lines':len(rows),'validation_records':len({r['id']for r in validation}),'selection_results':results,'selected_C':winner['C'],'enabled_metadata_roles':winner['enabled_metadata_roles'],'model_sha256':hashlib.sha256(model_path.read_bytes()).hexdigest(),'extractor_sha256':manifest['extractor_sha256'],'feature_version':VERSION,'training_source_rows_sha256':manifest['training_rows_sha256'],'test_inspected':False,'score_convention':'Actual Go-equivalent emitted roles after source OOV, threshold margin and incompatible-role abstention determine model selection and metadata-role eligibility. Only qualified metadata roles may become proposals; role is not metadata applicability.'}
    write(out/'model-freeze.json',freeze);return freeze

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('mode',choices=['fit','select']);p.add_argument('rows',type=Path);p.add_argument('extractor',type=Path);p.add_argument('output',type=Path);a=p.parse_args();result=fit(read_rows(a.rows),a.extractor,a.output,hashlib.sha256(a.rows.read_bytes()).hexdigest()) if a.mode=='fit'else select(read_rows(a.rows),a.extractor,a.output);print(canonical(result))
