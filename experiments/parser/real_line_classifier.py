"""Partial-positive-only line classification from source-cited annotations.

All output, including line text and model, must remain outside Git.
"""
import argparse
import hashlib
from collections import Counter, defaultdict
import json
from pathlib import Path
import re
from .runner import read_cases
from .source_review import source_hash

def examples(cases, drafts, reviews):
    counts=Counter(); rows=[]; group_split={}
    if len({r['id'] for r in drafts}) != len(drafts) or len({r['id'] for r in reviews}) != len(reviews): raise ValueError('duplicate annotation IDs')
    drafts={r['id']:r for r in drafts}; reviews={r['id']:r for r in reviews}
    for c in cases:
        split=c['split']; group=c['group']
        if group in group_split and group_split[group]!=split: raise ValueError('group crosses splits')
        group_split[group]=split
        counts[split+'_cases']+=1
        counts[split+'_total_lines']+=sum(len(n['text'].splitlines()) for n in c.get('notes',[]))
        label=(drafts if split=='train' else reviews).get(c['id'])
        if not label: counts[split+'_missing_annotation']+=1; continue
        if split=='train':
            if label.get('label_status')!='draft': counts['train_annotation_error']+=1; continue
            if label.get('source_sha256')!=source_hash(c): counts['stale_or_invalid_drafts']+=1; continue
        else:
            if label.get('label_status') not in ('agent-reviewed','adjudicated'): counts[split+'_not_reviewed']+=1; continue
            if label.get('annotation_review',{}).get('source_sha256')!=source_hash(c): counts['stale_or_invalid_reviews']+=1; continue
        counts[split+'_annotated_cases']+=1
        line_map={n['name']:n['text'].splitlines() for n in c['notes']}; candidates=defaultdict(set)
        if len(line_map)!=len(c['notes']): counts['duplicate_note_names']+=1;continue
        for ev in label.get('oracle',{}).get('evidence',[]):
            counts['citations_seen']+=1; field=ev.get('field',''); category='title' if re.fullmatch(r'titles\.[1-9]\d*:[1-9]\d*',field) else field
            if category not in ('artist','date','venue','city','title'): counts['unsupported_field']+=1; continue
            oracle=label.get('oracle',{})
            populated=oracle.get('titles',{}).get(field[len('titles.'):]) if category=='title' else oracle.get(category)
            if not populated: counts['uncited_oracle_field']+=1; continue
            quote=ev.get('quote','')
            if ev.get('note') not in line_map: counts['wrong_note_reference']+=1; continue
            lines=line_map[ev['note']]
            if not quote or '\n' in quote or '\r' in quote: counts['empty_or_multiline_quote']+=1;continue
            hits=[i for i,line in enumerate(lines) if quote in line]
            if not hits and any(quote in other for name,other_lines in line_map.items() if name!=ev['note'] for other in other_lines):
                counts['wrong_note_reference']+=1;continue
            if len(hits)!=1: counts['missing_or_ambiguous_quote']+=1;continue
            candidates[(ev['note'],hits[0])].add(category)
        for (note,i),categories in candidates.items():
            if len(categories)!=1: counts['conflicting_line_labels']+=1;continue
            lines=line_map[note];rows.append({'id':c['id'],'group':group,'split':split,'note':note,'line_index':i,'line':lines[i],'prev':lines[i-1] if i else '', 'next':lines[i+1] if i+1<len(lines) else '', 'label':next(iter(categories))})
            counts[split+'_lines']+=1
    return rows,dict(counts)

def train(rows):
    from sklearn.feature_extraction.text import CountVectorizer
    from sklearn.linear_model import LogisticRegression
    from sklearn.metrics import classification_report, confusion_matrix
    from scipy.sparse import hstack
    train=[r for r in rows if r['split']=='train']; classes={r['label'] for r in train}
    if len(classes)<2: raise ValueError('at least two cited training classes required')
    vecs=[CountVectorizer(ngram_range=(1,2),token_pattern=r'(?u)\b\w+\b'),CountVectorizer(analyzer='char',ngram_range=(2,4),max_features=12000),CountVectorizer(ngram_range=(1,2),token_pattern=r'(?u)\b\w+\b')]
    def strings(r): return r['line'],r['line'], 'previous '+r['prev']+' next '+r['next']
    x=hstack([v.fit_transform([strings(r)[i] for r in train]) for i,v in enumerate(vecs)]).tocsr(); model=LogisticRegression(max_iter=300,random_state=42).fit(x,[r['label'] for r in train])
    majority=sorted(Counter(r['label'] for r in train).items(),key=lambda x:(-x[1],x[0]))[0][0]
    report={'scope':'partial positive source-cited lines only; uncited/background lines excluded; no false positive or technical-line generalization estimate','training_majority_class':majority,'splits':{}}
    for split in ('train','validation','test'):
        rs=[r for r in rows if r['split']==split]
        if not rs: report['splits'][split]={'lines':0};continue
        x=hstack([v.transform([strings(r)[i] for r in rs]) for i,v in enumerate(vecs)]).tocsr();truth=[r['label'] for r in rs];pred=model.predict(x);labels=sorted(set(truth)|set(pred))
        report['splits'][split]={'lines':len(rs),'support':dict(Counter(truth)),'majority_class_accuracy':sum(x==majority for x in truth)/len(truth),'classification_report':classification_report(truth,pred,output_dict=True,zero_division=0),'confusion_labels':labels,'confusion':confusion_matrix(truth,pred,labels=labels).tolist()}
    return report,{'vectorizers':vecs,'model':model,'training_majority_class':majority}

def main():
    p=argparse.ArgumentParser();p.add_argument('--frozen',type=Path,required=True);p.add_argument('--drafts',type=Path,required=True);p.add_argument('--reviews',type=Path,required=True);p.add_argument('--output-dir',type=Path,required=True);p.add_argument('--labels-freeze',type=Path);p.add_argument('--evaluate-frozen-model',type=Path);p.add_argument('--training-freeze',type=Path);p.add_argument('--partition',type=Path);p.add_argument('--refs',type=Path);args=p.parse_args()
    # Require a fresh external directory so raw note lines cannot enter repository or overwrite prior runs.
    from .classifier_refs import require_external
    out=require_external(args.output_dir)
    out.mkdir(parents=True,exist_ok=False);cases=read_cases(args.frozen)
    if args.partition:
        partition=json.loads(args.partition.read_text())
        if partition.get('source_sha256')!=hashlib.sha256(args.frozen.read_bytes()).hexdigest(): raise SystemExit('partition source mismatch')
        cases=[c for c in cases if c['id'] not in partition['exclusions']]
    if not args.evaluate_frozen_model: cases=[c for c in cases if c['split']!='test']
    if args.labels_freeze:
        frozen_labels=json.loads(args.labels_freeze.read_text())
        if not frozen_labels.get('labels_frozen') or frozen_labels.get('source_sha256')!=hashlib.sha256(args.frozen.read_bytes()).hexdigest(): raise SystemExit('labels are not frozen against this source')
        reviewed=read_cases(args.labels_freeze)
    else:
        cases=[c for c in cases if c['split']!='test']
        reviewed=[r for r in read_cases(args.reviews) if r['split']!='test']
    if args.refs:
        if not args.partition: raise SystemExit('reference replay requires partition')
        from .classifier_refs import rehydrate,references,sha_file
        refs=read_cases(args.refs)
        expected={"train":sha_file(args.drafts),"validation":sha_file(args.labels_freeze or args.reviews),"test":sha_file(args.labels_freeze or args.reviews)}
        if any(r.get('annotation_artifact_sha256')!=expected[r['split']] for r in refs): raise ValueError('reference annotation artifact mismatch')
        if not args.evaluate_frozen_model and any(r['split']=='test' for r in refs): raise ValueError('test lines cannot enter fitting')
        if args.evaluate_frozen_model and any(r['split']!='test' for r in refs): raise ValueError('test evaluation requires test-only references')
        draft_rows=read_cases(args.drafts)
        expected_rows,counts=examples(cases,draft_rows,reviewed)
        if args.evaluate_frozen_model: expected_rows=[r for r in expected_rows if r['split']=='test']
        hashes={'train':sha_file(args.drafts),'validation':sha_file(args.labels_freeze or args.reviews),'test':sha_file(args.labels_freeze or args.reviews)}
        authoritative=references(expected_rows,cases,draft_rows+reviewed,hashes)
        rows=rehydrate(refs,cases,partition,authoritative);counts['replayed_references']=len(rows)
    else:
        rows,counts=examples(cases,read_cases(args.drafts),reviewed)
    (out/'lines.jsonl').write_text(''.join(json.dumps(r,ensure_ascii=False)+'\n' for r in rows));report={'coverage':counts,'label_status':'train model drafts; validation/test independent source reviews'}
    try:
        import joblib
        if args.evaluate_frozen_model:
            if not args.labels_freeze or not args.training_freeze: raise ValueError('frozen model evaluation requires label and training freezes')
            manifest=json.loads(args.training_freeze.read_text())
            if manifest.get('model_sha256')!=hashlib.sha256(args.evaluate_frozen_model.read_bytes()).hexdigest(): raise ValueError('classifier model hash differs from training freeze')
            if not args.partition or manifest.get('partition_sha256')!=hashlib.sha256(args.partition.read_bytes()).hexdigest(): raise ValueError('classifier partition differs from training freeze')
            if manifest.get('draft_snapshot_sha256')!=hashlib.sha256(args.drafts.read_bytes()).hexdigest(): raise ValueError('classifier drafts differ from training freeze')
            artifact=joblib.load(args.evaluate_frozen_model);vecs=artifact['vectorizers'];model=artifact['model'];majority=artifact['training_majority_class'];test=[r for r in rows if r['split']=='test']
            if not test: raise ValueError('no eligible test citations')
            from scipy.sparse import hstack
            from sklearn.metrics import classification_report,confusion_matrix
            xs=[(r['line'],r['line'],'previous '+r['prev']+' next '+r['next']) for r in test]
            x=hstack([v.transform([a[i] for a in xs]) for i,v in enumerate(vecs)]).tocsr();truth=[r['label'] for r in test];pred=model.predict(x);labels=sorted(set(truth)|set(pred))
            report.update({'scope':'frozen model; partial positive source-cited test lines only; no background false-positive estimate','training_majority_class':majority,'splits':{'test':{'lines':len(test),'support':dict(Counter(truth)),'majority_class_accuracy':sum(x==majority for x in truth)/len(truth),'classification_report':classification_report(truth,pred,output_dict=True,zero_division=0),'confusion_labels':labels,'confusion':confusion_matrix(truth,pred,labels=labels).tolist()}}})
        else:
            metrics,model=train(rows);report.update(metrics);joblib.dump(model,out/'classifier.joblib')
    except ValueError as exc: report['training_unavailable']=str(exc)
    (out/'report.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
    print(json.dumps({'coverage':counts,'training_unavailable':report.get('training_unavailable')}))
if __name__=='__main__':main()
