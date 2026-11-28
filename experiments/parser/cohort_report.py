"""Coverage and ready/accepted-exact metrics on identical frozen ID cohorts."""
import argparse
import hashlib
import random
import math
from pathlib import Path
from .runner import read_cases,wilson
from .hosted import canonical


def summarize(predictions,labels,ids=None,scope='all'):
    cases=read_cases(labels);requested=set(ids) if ids is not None else {c['id'] for c in cases}
    cases=[c for c in cases if c['id'] in requested]
    if scope=='all-flac':cases=[c for c in cases if c['files'] and all(p.lower().endswith('.flac') for p in c['files'])]
    elif scope=='supported-audio':cases=[c for c in cases if c['files'] and all(Path(p).suffix.lower() in {'.flac','.wav','.mp3','.ogg','.oga','.opus'} for p in c['files'])]
    by_id={r['id']:r for r in read_cases(predictions)};eligible=[c for c in cases if c.get('label_status') in ('agent-reviewed','adjudicated','synthetic') and c.get('oracle')]
    reference_unresolved=[c for c in cases if c not in eligible]
    result={'requested':len(cases),'requested_ids':[c['id'] for c in cases],'eligible_reference_cases':len(eligible),'eligible_ids':[c['id'] for c in eligible],
            'attempted':sum(c['id'] in by_id for c in cases),'errored':sum('error'in by_id.get(c['id'],{}) for c in cases),'unattempted':sum(c['id']not in by_id for c in cases),
            'scope':scope,'predictions_sha256':hashlib.sha256(Path(predictions).read_bytes()).hexdigest()}
    for stage in ('raw','validated'):
        metrics={'exact':0,'ready':0,'accepted_exact':0,'accepted_nonexact_proxy':0,'review_outputs':0,'false_ready':0,'strict_incorrect_accepted_mappings':0,'correct_titles':0,'expected_titles':0,'correct_mappings':0,'expected_mappings':0}
        for case in eligible:
            row=by_id.get(case['id'],{});value=row.get(stage);score=row.get(stage+'_score',{})
            ready=value is not None and not value.get('review',True)
            for key in ('exact','false_ready','correct_titles','correct_mappings'):metrics[key]+=int(score.get(key,0))
            metrics['strict_incorrect_accepted_mappings']+=score.get('incorrect_accepted_mappings',0)
            metrics['ready']+=ready;metrics['accepted_exact']+=ready and bool(score.get('exact'));metrics['accepted_nonexact_proxy']+=ready and not score.get('exact',False)
            metrics['review_outputs']+=value is not None and value.get('review',True)
        # Every eligible requested reference contributes its expected fields, even
        # when the prediction is missing or failed. Correct counts remain observed.
        metrics['expected_titles']=sum(len(c['oracle'].get('titles',{})) for c in eligible)
        metrics['expected_mappings']=sum(len(c['oracle'].get('mappings',{})) for c in eligible)
        metrics['metadata_fields']={}
        for field in ('artist','date','venue','city'):
            known=[c for c in eligible if field in c['oracle']]
            emitted=[c for c in known if stage in by_id.get(c['id'],{}) and 'error'not in by_id[c['id']]]
            matched=sum(bool(by_id[c['id']].get(stage+'_score',{}).get('metadata_fields',{}).get(field)) for c in emitted)
            metrics['metadata_fields'][field]={'emitted_exact_matches':matched,'eligible_reference_cases':len(known),'emitted_outputs':len(emitted),'missing_or_error_outputs':len(known)-len(emitted),'comparison':'whitespace-normalized exact; errors score zero even when reference empty'}
        metrics['exact_wilson95']=wilson(metrics['exact'],len(eligible))
        metrics['acceptance_with_unresolved_reference']=sum(by_id.get(c['id'],{}).get(stage) is not None and not by_id[c['id']][stage].get('review',True) for c in reference_unresolved)
        result[stage]=metrics
    timings=sorted(r['latency_seconds'] for c in cases for r in [by_id.get(c['id'],{})] if isinstance(r.get('latency_seconds'),(int,float)) and 'error'not in r)
    result['successful_output_latency_seconds']={'n':len(timings),'p50':timings[len(timings)//2] if timings else None,'p95':timings[min(len(timings)-1,int(len(timings)*.95))] if timings else None,'method':'runner empirical upper-index quantiles; cached outputs retain original inference timing'}
    rates={'gpt-6-luna':(.1,.5),'gpt-6.1-sol':(2,10),'gpt-6-astra':(10,50)};logical=0;incremental=0
    from .hosted import usage_cost
    for c in cases:
        row=by_id.get(c['id'],{});actual=row.get('actual_model') or '';model=next((m for m in rates if actual.startswith(m)),None)
        if model and row.get('usage'):
            try:cost,_=usage_cost(row['usage'],{'model':model,'input_per_million':rates[model][0],'output_per_million':rates[model][1]})
            except ValueError:continue
            logical+=float(cost)
            if not row.get('cache_hit'):incremental+=float(cost)
    result['logical_single_pass_known_usage_cost_usd']=logical
    result['checkpoint_edition_uncached_known_usage_cost_usd']=incremental
    result['cost_scope']='usage-list-price estimates; logical cost includes reused outputs, not a ledger debit or invoice; do not sum repeated checkpoint editions'
    return result


def paired_exact(left,right,ids,stage='validated',seed=20261104,repetitions=10000):
    a={r['id']:r for r in read_cases(left)};b={r['id']:r for r in read_cases(right)}
    if any(i not in a or i not in b for i in ids):raise ValueError('paired interval requires every same eligible ID attempted')
    differences=[int(bool(a[i][stage+'_score']['exact']))-int(bool(b[i][stage+'_score']['exact'])) for i in ids]
    if not differences:return None
    randomizer=random.Random(seed);n=len(differences);samples=sorted(sum(randomizer.choices(differences,k=n))/n for _ in range(repetitions))
    return {'eligible_pairs':n,'left_minus_right_exact_fraction':sum(differences)/n,'paired_bootstrap95':[samples[int(.025*repetitions)],samples[min(repetitions-1,int(.975*repetitions))]],'seed':seed,'repetitions':repetitions,'interpretation':'conditional case-resampling variability on this fixed nonrandom benchmark; does not include reference or selection uncertainty'}


def main():
    import json
    p=argparse.ArgumentParser();p.add_argument('predictions');p.add_argument('labels');p.add_argument('--cohort',choices=['all','common40'],default='all');p.add_argument('--scope',choices=['all','all-flac','supported-audio'],default='all');a=p.parse_args()
    manifest=json.loads(Path(a.labels).read_text()) if a.cohort=='common40' else None
    print(canonical(summarize(a.predictions,a.labels,manifest['astra_ids'] if manifest else None,a.scope)))
if __name__=='__main__':main()
