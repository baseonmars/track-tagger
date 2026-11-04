"""Versioned, source-only mechanical normalization before label freezing.

Original reviews and quote bytes remain untouched. Only uniquely verifiable note
references and a verdict inconsistent with returned annotation fields may change.
"""
import argparse
import copy
from pathlib import Path
from .hosted import canonical
from .runner import read_cases
from .source_review import FIELDS, sha, validate_review

VERSION='citation-reference-normalization-v1'

def repair(row,draft):
    result=copy.deepcopy(row)
    oracle=result.get('oracle')
    audit=result.get('annotation_review') or {}
    if not isinstance(oracle,dict):return result,{'id':row['id'],'repairs':[],'blocked':['no-oracle']}
    changes=[]
    notes={n['name']:n['text'] for n in row['notes']}
    for index,item in enumerate(oracle.get('evidence',[])):
        quote=item['quote']
        if quote and quote in notes.get(item['note'],''):continue
        matches=[name for name,text in notes.items() if quote and quote in text]
        if len(matches)==1:
            changes.append({'field':'evidence['+str(index)+'].note','before':item['note'],'after':matches[0],'quote_sha256':__import__('hashlib').sha256(quote.encode()).hexdigest()})
            item['note']=matches[0]
    verdict=audit.get('verdict')
    changed_fields=[k for k in FIELDS-{'evidence'} if oracle.get(k)!=draft['oracle'].get(k)]
    if verdict=='accepted' and changed_fields:
        verdict='corrected'
        changes.append({'field':'verdict','before':'accepted','after':'corrected','changed_annotation_fields':sorted(changed_fields)})
    response={'verdict':verdict,'oracle':oracle,'reasons':[]}
    issues=validate_review(result,draft,response)
    if changes:
        audit.update(original_verdict=row['annotation_review'].get('verdict'),verdict=verdict,
                     representation_normalization={'version':VERSION,'original_row_sha256':sha(row),'draft_sha256':sha(draft),'repairs':changes},
                     promotion_blockers=issues,repaired_response_sha256=sha(response),not_human_certified=True)
        result['annotation_review']=audit
    if verdict in ('accepted','corrected') and not issues:result['label_status']='agent-reviewed'
    else:result['label_status']='disputed' if issues or verdict=='disputed' else 'unresolved'
    return result,{'id':row['id'],'version':VERSION,'original_sha256':sha(row),'normalized_sha256':sha(result),'repairs':changes,'blocked':issues,'label_status':result['label_status']}

def normalize(reviews,drafts,output,audit_output):
    if Path(output).exists() or Path(audit_output).exists():raise ValueError('normalization outputs already exist')
    draft_by_id={r['id']:r for r in read_cases(drafts) if r.get('label_status')=='draft'}
    results=[];audits=[]
    for row in read_cases(reviews):
        if row['id'] not in draft_by_id:raise ValueError('missing annotation draft')
        result,audit=repair(row,draft_by_id[row['id']]);results.append(result);audits.append(audit)
    Path(output).write_text(''.join(canonical(r)+'\n' for r in results))
    Path(audit_output).write_text(''.join(canonical(r)+'\n' for r in audits))
    return {'cases':len(results),'repaired_cases':sum(bool(a['repairs']) for a in audits),'repaired_fields':sum(len(a['repairs']) for a in audits),'eligible':sum(r['label_status']=='agent-reviewed' for r in results),'version':VERSION}

def main():
    p=argparse.ArgumentParser();p.add_argument('reviews');p.add_argument('drafts');p.add_argument('output');p.add_argument('audit');a=p.parse_args()
    print(canonical(normalize(a.reviews,a.drafts,a.output,a.audit)))
if __name__=='__main__':main()
