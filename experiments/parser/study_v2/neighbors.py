"""Bounded source-only nearest-neighbor audit candidates, never split decisions.

Exact Jaccard comparisons rank residual text/template similarities not covered by
component keys. Scores are leads for source review, not semantic independence.
Output contains no original text or uploader identities.
"""
import argparse
import hashlib
import json
import re
from pathlib import Path
from ..hosted import canonical
from .labels import normalize, source_hash

MAX_CASES=2500
MAX_TEXT_BYTES=4*1024*1024
MAX_SHINGLES=20000
MAX_TOTAL_SHINGLES=2000000

def shingles(case,technical=False):
    value='\n'.join(normalize(n['text']) for n in case['notes'])
    if len(value.encode())>MAX_TEXT_BYTES:raise ValueError('source text exceeds audit bound')
    if technical:
        value='\n'.join(line for line in value.splitlines() if re.search(r'(?i)source|taper|transfer|record|master|lineage|compil|seed|upload|microphone|\.com|\.org',line))
    words=re.findall(r'[^\W_]+',value.casefold(),re.UNICODE)
    result={' '.join(words[i:i+5]) for i in range(max(0,len(words)-4))}
    # Deterministic digest sampling bounds memory while retaining broad coverage.
    if len(result)>MAX_SHINGLES:result=set(sorted(result,key=lambda x:hashlib.sha256(x.encode()).digest())[:MAX_SHINGLES])
    return result

def audit(fresh,old,top_k=3):
    if not 1<=top_k<=10:raise ValueError('top_k must be1..10')
    if len(fresh)+len(old)>MAX_CASES:raise ValueError('too many source cases')
    all_cases=[{**c,'audit_origin':'fresh'} for c in fresh]+[{**c,'audit_origin':'old'} for c in old]
    if len({c['id'] for c in all_cases})!=len(all_cases):raise ValueError('duplicate source IDs')
    feature=[];total=0
    for c in all_cases:
        pair=(shingles(c),shingles(c,True));total+=sum(map(len,pair))
        if total>MAX_TOTAL_SHINGLES:raise ValueError('total source features exceed audit bound')
        feature.append(pair)
    hashes={c['id']:source_hash(c) for c in all_cases}
    pairs={}
    for i,left in enumerate(all_cases[:len(fresh)]):
        for mode in (0,1):
            ranked=[];a=feature[i][mode]
            if not a:continue
            for j,right in enumerate(all_cases):
                if i==j or (right['audit_origin']=='fresh' and left['split']==right['split']):continue
                b=feature[j][mode];shared=len(a&b)
                if not shared:continue
                score=shared/len(a|b)
                ranked.append((score,shared,right['id'],j))
            for score,shared,_,j in sorted(ranked,key=lambda x:(-x[0],-x[1],x[2]))[:top_k]:
                right=all_cases[j];key=(min(left['id'],right['id']),max(left['id'],right['id']),mode)
                pairs[key]={'left_id':left['id'],'left_split':left['split'],'left_source_sha256':hashes[left['id']],'right_id':right['id'],'right_origin':right['audit_origin'],'right_split':right.get('split','old-development'),'right_source_sha256':hashes[right['id']],'feature':'technical-five-word-shingles' if mode else 'full-note-five-word-shingles','jaccard':score,'shared_shingles':shared,'manual_source_review_required':True}
    return {'schema_version':1,'fresh_cases':len(fresh),'old_cases':len(old),'top_k_per_case_per_feature':top_k,'max_shingles_per_feature':MAX_SHINGLES,'pairs':sorted(pairs.values(),key=lambda r:(-r['jaccard'],r['left_id'],r['right_id'],r['feature'])),'limitations':'Ranks source-only leads, not proof of template isolation. Paraphrases, short templates and deterministic sampling can evade similarity. Review top neighbors against original notes before labels; no automatic split changes.'}

def read(path):
    value=json.loads(path.read_text());return value['cases'] if isinstance(value,dict) else value

def main():
    p=argparse.ArgumentParser();p.add_argument('fresh',type=Path);p.add_argument('old',type=Path);p.add_argument('output',type=Path);p.add_argument('--top-k',type=int,default=3);a=p.parse_args()
    report=audit(read(a.fresh),read(a.old),a.top_k)
    report['input_artifacts']={str(path):hashlib.sha256(path.read_bytes()).hexdigest() for path in (a.fresh,a.old)}
    a.output.write_text(canonical(report)+'\n')
if __name__=='__main__':main()
