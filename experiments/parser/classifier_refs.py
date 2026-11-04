"""Export and replay text-free cited-line references for classifier reproduction."""
import argparse
import hashlib
import json
import re
from pathlib import Path
from .real_line_classifier import examples
from .runner import read_cases
from .source_review import source_hash
from .path_guard import require_external

def sha_text(text):return hashlib.sha256(text.encode('utf-8')).hexdigest()
def sha_file(path):return hashlib.sha256(Path(path).read_bytes()).hexdigest()

def references(rows,cases,annotations,annotation_hashes):
    by_case={c['id']:c for c in cases};by_annotation={r['id']:r for r in annotations};out=[];seen=set()
    for row in rows:
        key=(row['id'],row['note'],row['line_index'])
        if key in seen:raise ValueError('duplicate line reference')
        seen.add(key);c=by_case[row['id']];a=by_annotation[row['id']]
        out.append({k:row[k] for k in ('id','group','split','note','line_index','label')}|{
            'source_input_sha256':source_hash(c),'line_sha256':sha_text(row['line']),'prev_sha256':sha_text(row['prev']),'next_sha256':sha_text(row['next']),
            'annotation_status':a['label_status'],'annotation_artifact_sha256':annotation_hashes[row['split']]})
    return out

def rehydrate(refs,cases,partition,expected_refs):
    canonical=lambda r:json.dumps(r,sort_keys=True,separators=(',',':'))
    if sorted(map(canonical,refs))!=sorted(map(canonical,expected_refs)):raise ValueError('reference selection or labels differ from authoritative annotations')
    by_case={c['id']:c for c in cases};rows=[];seen=set()
    for ref in refs:
        key=(ref['id'],ref['note'],ref['line_index'])
        if key in seen:raise ValueError('duplicate line reference')
        seen.add(key);c=by_case.get(ref['id'])
        if not c or c['id'] in partition['exclusions'] or any(c[k]!=ref[k] for k in ('group','split')) or source_hash(c)!=ref['source_input_sha256']: raise ValueError('case identity, partition or source hash mismatch')
        notes=[n for n in c['notes'] if n['name']==ref['note']]
        if len(notes)!=1 or type(ref['line_index']) is not int or ref['line_index']<0: raise ValueError('invalid note or line index')
        lines=notes[0]['text'].splitlines();i=ref['line_index']
        if i>=len(lines):raise ValueError('line index outside source')
        line=lines[i];prev=lines[i-1] if i else '';nxt=lines[i+1] if i+1<len(lines) else ''
        if any(sha_text(value)!=ref[k] for k,value in (('line_sha256',line),('prev_sha256',prev),('next_sha256',nxt))):raise ValueError('line context hash mismatch')
        if ref['label'] not in ('artist','date','venue','city','title'): raise ValueError('invalid line label')
        rows.append({**{k:ref[k] for k in ('id','group','split','note','line_index','label')},'line':line,'prev':prev,'next':nxt})
    return rows

def verify_public_source(cases,manifest):
    entries=manifest.get('cases',[])
    by_id={c['id']:c for c in cases};expected={c['id']:c for c in entries}
    if len(by_id)!=len(cases) or len(expected)!=len(entries) or set(by_id)!=set(expected):raise ValueError('public source case IDs differ')
    for id,c in by_id.items():
        e=expected[id]
        if any(c[k]!=e[k] for k in ('group','split')) or source_hash(c)!=e['input_sha256']:raise ValueError('public source identity or content differs')

def main():
    p=argparse.ArgumentParser();sub=p.add_subparsers(dest='command',required=True)
    for name in ('export','replay','train-replay','evaluate-replay'):
        q=sub.add_parser(name);q.add_argument('--frozen',type=Path,required=True);q.add_argument('--partition',type=Path,required=True);q.add_argument('--output',type=Path,required=True)
        if name=='export':
            q.add_argument('--drafts',type=Path,required=True);q.add_argument('--reviews',type=Path,required=True);q.add_argument('--stage',choices=('train-validation','test'),required=True);q.add_argument('--model',type=Path);q.add_argument('--training-freeze',type=Path)
        else:
            q.add_argument('--refs',type=Path,required=True)
            if name=='replay':q.add_argument('--expected-sha256',required=True)
            else:
                q.add_argument('--refs-manifest',type=Path,required=True);q.add_argument('--source-manifest',type=Path,required=True)
                if name=='evaluate-replay':q.add_argument('--model',type=Path,required=True);q.add_argument('--training-freeze',type=Path,required=True)
    a=p.parse_args();source=read_cases(a.frozen);part=json.loads(a.partition.read_text())
    if a.command in ('train-replay','evaluate-replay'):
        verify_public_source(source,json.loads(a.source_manifest.read_text()))
    elif part['source_sha256']!=sha_file(a.frozen):raise ValueError('partition source mismatch')
    if a.output.exists():raise ValueError('output already exists')
    if a.command=='export':
        if a.stage=='test':
            if not a.model or not a.training_freeze:raise ValueError('test refs require frozen model')
            freeze=json.loads(a.training_freeze.read_text())
            if freeze.get('model_sha256')!=sha_file(a.model) or freeze.get('partition_sha256')!=sha_file(a.partition):raise ValueError('model or partition differs from training freeze')
        eligible=[c for c in source if c['id'] not in part['exclusions'] and ((c['split']=='test') if a.stage=='test' else (c['split']!='test'))]
        obj=json.loads(a.reviews.read_text())
        if not obj.get('labels_frozen') or obj.get('source_sha256')!=sha_file(a.frozen):raise ValueError('review labels not frozen to source')
        drafts=read_cases(a.drafts);reviews=read_cases(a.reviews);rows,coverage=examples(eligible,drafts,reviews)
        annotation_hashes={'train':sha_file(a.drafts),'validation':sha_file(a.reviews),'test':sha_file(a.reviews)}
        refs=references(rows,eligible,drafts+reviews,annotation_hashes);a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(''.join(json.dumps(r,sort_keys=True)+'\n' for r in refs));print(json.dumps({'references':len(refs),'coverage':coverage,'output_sha256':sha_file(a.output)}))
    else:
        require_external(a.output)
        if a.command=='replay':
            if sha_file(a.refs)!=a.expected_sha256:raise ValueError('references differ from frozen manifest hash')
        else:
            manifest=json.loads(a.refs_manifest.read_text())
            stage='test' if a.command=='evaluate-replay' else 'train-validation'
            if manifest.get('stage')!=stage or manifest.get('refs_sha256')!=sha_file(a.refs) or manifest.get('source_manifest_sha256')!=sha_file(a.source_manifest) or manifest.get('source_sha256')!=part['source_sha256'] or manifest.get('partition_sha256')!=sha_file(a.partition):raise ValueError('trusted reference manifest mismatch')
        frozen_refs=read_cases(a.refs);rows=rehydrate(frozen_refs,source,part,frozen_refs)
        if a.command=='replay':
            a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(''.join(json.dumps(r,ensure_ascii=False)+'\n' for r in rows));print(json.dumps({'rows':len(rows)}));return
        if a.command=='train-replay':
            if any(r['split']=='test' for r in rows):raise ValueError('test lines cannot enter fitting')
            from .real_line_classifier import train
            import joblib
            report,artifact=train(rows);a.output.mkdir(parents=True,exist_ok=False);model_path=a.output/'classifier.joblib';joblib.dump(artifact,model_path);(a.output/'report.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n');freeze={'model_sha256':sha_file(model_path),'partition_sha256':sha_file(a.partition),'train_refs_sha256':sha_file(a.refs),'source_manifest_sha256':sha_file(a.source_manifest),'training_majority_class':artifact['training_majority_class'],'reference_model_sha256':manifest.get('model_sha256')};(a.output/'training-freeze.json').write_text(json.dumps(freeze,indent=2,sort_keys=True)+'\n');print(json.dumps({'rows':len(rows),'model_sha256':freeze['model_sha256']}));return
        if any(r['split']!='test' for r in rows):raise ValueError('evaluation requires test-only references')
        freeze=json.loads(a.training_freeze.read_text())
        if freeze.get('model_sha256')!=sha_file(a.model) or freeze.get('partition_sha256')!=sha_file(a.partition) or freeze.get('source_manifest_sha256')!=sha_file(a.source_manifest):raise ValueError('model, partition or source manifest differs from training freeze')
        manifest_model=manifest.get('model_sha256'); reference=freeze.get('reference_model_sha256')
        allowed={freeze.get('model_sha256')}
        if isinstance(reference,str) and re.fullmatch(r'[0-9a-f]{64}',reference):allowed.add(reference)
        if not isinstance(manifest_model,str) or not re.fullmatch(r'[0-9a-f]{64}',manifest_model) or manifest.get('training_refs_sha256')!=freeze.get('train_refs_sha256') or manifest_model not in allowed:raise ValueError('test references not bound to frozen training')
        import joblib
        from scipy.sparse import hstack
        from sklearn.metrics import classification_report,confusion_matrix
        from collections import Counter
        artifact=joblib.load(a.model);vecs=artifact['vectorizers'];model=artifact['model'];majority=artifact['training_majority_class'];xs=[(r['line'],r['line'],'previous '+r['prev']+' next '+r['next']) for r in rows]
        if not rows:raise ValueError('no test references')
        x=hstack([v.transform([a[i] for a in xs]) for i,v in enumerate(vecs)]).tocsr();truth=[r['label'] for r in rows];pred=model.predict(x);labels=sorted(set(truth)|set(pred))
        report={'scope':'classification among cited positive test lines only; no background false-positive estimate','lines':len(rows),'support':dict(Counter(truth)),'training_majority_class':majority,'majority_class_accuracy':sum(x==majority for x in truth)/len(truth),'classification_report':classification_report(truth,pred,output_dict=True,zero_division=0),'confusion_labels':labels,'confusion':confusion_matrix(truth,pred,labels=labels).tolist()}
        a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(json.dumps(report,indent=2,sort_keys=True)+'\n');print(json.dumps({'rows':len(rows)}))
if __name__=='__main__':main()
