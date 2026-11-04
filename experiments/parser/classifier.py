#!/usr/bin/env python3
"""Train a CPU-only line classifier and report group-held-out line metrics."""
import argparse
import json
import time
import resource
import joblib
from collections import Counter
from pathlib import Path
from sklearn.feature_extraction import DictVectorizer
from sklearn.feature_extraction.text import CountVectorizer
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import classification_report, confusion_matrix
from scipy.sparse import hstack

def lines(cases, split):
    rows=[]; labels=[]
    for c in cases:
        if c['split']!=split: continue
        text=c['notes'][0]['text'].splitlines(); assert len(text)==len(c['line_labels'])
        for i, (line,label) in enumerate(zip(text,c['line_labels'])):
            prev=text[i-1] if i else ''; nxt=text[i+1] if i+1<len(text) else ''
            rows.append({'current':line,'prev':prev,'next':nxt,'position':str(i),'starts_digit':str(bool(line.lstrip()[:1].isdigit()))})
            labels.append(label)
    return rows,labels

def main():
    p=argparse.ArgumentParser();p.add_argument('input',type=Path);p.add_argument('--output',type=Path,required=True);p.add_argument('--model',type=Path);args=p.parse_args()
    cases=[json.loads(line) for line in args.input.open(encoding='utf-8')]
    groups={s:{c['group'] for c in cases if c['split']==s} for s in ('train','validation','test')}
    assert not groups['train'] & groups['validation'] and not groups['train'] & groups['test'] and not groups['validation'] & groups['test']
    data={s:lines(cases,s) for s in groups}
    word=CountVectorizer(ngram_range=(1,2),min_df=2,max_features=10000,token_pattern=r'(?u)\b\w+\b')
    char=CountVectorizer(analyzer='char',ngram_range=(2,4),min_df=2,max_features=12000)
    neighbor=CountVectorizer(ngram_range=(1,2),min_df=2,max_features=5000,token_pattern=r'(?u)\b\w+\b')
    features=DictVectorizer()
    def texts(rows): return [r['current'] for r in rows]
    def neighbors(rows): return ['previous '+r['prev']+' next '+r['next'] for r in rows]
    def meta(rows): return [{'position':r['position'],'starts_digit':r['starts_digit']} for r in rows]
    train,y=data['train']; X=hstack([word.fit_transform(texts(train)),char.fit_transform(texts(train)),neighbor.fit_transform(neighbors(train)),features.fit_transform(meta(train))]).tocsr()
    model=LogisticRegression(max_iter=300,random_state=42); start=time.perf_counter(); model.fit(X,y); train_seconds=time.perf_counter()-start
    if args.model: joblib.dump({'word':word,'char':char,'neighbor':neighbor,'features':features,'model':model},args.model)
    result={'method':'word and character ngrams, neighboring lines, and position; logistic regression','groups':{s:sorted(g) for s,g in groups.items()},'training_seconds':train_seconds,'peak_rss_platform_units':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,'splits':{}}
    for split,(rows,truth) in data.items():
        X=hstack([word.transform(texts(rows)),char.transform(texts(rows)),neighbor.transform(neighbors(rows)),features.transform(meta(rows))]).tocsr(); start=time.perf_counter(); pred=model.predict(X); prediction_seconds=time.perf_counter()-start
        labels=sorted(set(truth)|set(pred)); result['splits'][split]={'lines':len(truth),'prediction_seconds':prediction_seconds,'support':dict(Counter(truth)),'report':classification_report(truth,pred,output_dict=True,zero_division=0),'confusion_labels':labels,'confusion':confusion_matrix(truth,pred,labels=labels).tolist()}
    args.output.write_text(json.dumps(result,indent=2,sort_keys=True)+'\n'); print(json.dumps({s:result['splits'][s]['report']['accuracy'] for s in groups}))
if __name__=='__main__': main()
