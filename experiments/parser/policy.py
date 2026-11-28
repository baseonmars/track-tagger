"""Pre-test selection and expansion policy; never selects on held-out scores."""
import hashlib
from pathlib import Path
from .hosted import canonical
from .runner import read_cases

POLICY = {'version':1,'common_real_test':'frozen astra_ids (40 requested)',
          'astra_real_test_limit':40,'hosted_expansion_valid_output_fraction':.8,
          'hosted_expansion_models':['gpt-6-luna','gpt-6.1-sol'],
          'hosted_expansion_target':'full frozen test split (~200)',
          'local_expansion':'common40 required if operational; full test only with time',
          'synthetic_final':'same preselected200 for surviving candidates; common40 optional otherwise',
          'selection_basis':'validation output completion only; accuracy and safety always reported',
          'prompt_revisions_maximum':2,'post_test_tuning':False,
          'hybrid':'simulate baseline-review routing using unchanged cached model predictions; no extra requests'}

def freeze_policy(path):
    path=Path(path)
    text=canonical(POLICY)+'\n'
    if path.exists() and path.read_text()!=text:raise ValueError('selection policy already frozen differently')
    if not path.exists():path.write_text(text)
    return hashlib.sha256(text.encode()).hexdigest()

def pilot_survives(path,expected_ids):
    rows=read_cases(path)
    selected={r['id']:r for r in rows if r['id'] in set(expected_ids)}
    valid=sum('error' not in r and 'raw' in r and 'validated' in r for r in selected.values())
    return {'requested':len(expected_ids),'valid_outputs':valid,
            'valid_output_fraction':valid/len(expected_ids) if expected_ids else 0,
            'expand':bool(expected_ids) and valid/len(expected_ids)>=.8}

def simulate_hybrid(cases,baseline_rows,model_rows):
    """Reuse identical predictions and usage; returned cost inputs include fallback cases only."""
    baseline={r['id']:r for r in baseline_rows};model={r['id']:r for r in model_rows}
    rows=[]
    for case in cases:
        first=baseline[case['id']]
        fallback=first.get('validated',{}).get('review',True)
        if fallback:
            chosen=model.get(case['id'])
            if chosen is None:
                from .runner import oracle
                from .scoring import score
                eligible=case.get('label_status') in ('adjudicated','agent-reviewed','synthetic') and bool(oracle(case))
                chosen={'id':case['id'],'error':'missing_model_prediction','accuracy_eligible':eligible,'features':case.get('features',[])}
                if eligible:
                    failed=score({'review':True},oracle(case),case['files']);failed['exact']=False
                    chosen.update(raw_score=failed,validated_score=failed)
            row={**chosen,'hybrid_used_fallback':True,'latency_seconds':(chosen.get('latency_seconds') or 0)+(first.get('latency_seconds') or 0),
                 'baseline_latency_seconds':first.get('latency_seconds'),'simulated_routing':True}
        else:
            row={**first,'usage':{},'hybrid_used_fallback':False,'simulated_routing':True}
        rows.append(row)
    return rows

def common_cohorts(results,requested_ids):
    """Expose same-ID head-to-head availability separately from label eligibility."""
    requested=set(requested_ids)
    by_model={name:{r['id']:r for r in rows} for name,rows in results.items()}
    attempted=requested.intersection(*(set(rows) for rows in by_model.values())) if by_model else set()
    eligible={identifier for identifier in requested if all(identifier in rows and rows[identifier].get('accuracy_eligible') for rows in by_model.values())}
    complete={identifier for identifier in attempted if all('error' not in rows[identifier] for rows in by_model.values())}
    return {'requested_ids':sorted(requested),'common_attempted_ids':sorted(attempted),
            'common_eligible_label_ids':sorted(eligible),'common_valid_output_ids':sorted(complete),
            'model_coverage':{name:{'requested':len(requested),'attempted':len(requested&set(rows)),
                'errors':sum(identifier in requested and 'error' in row for identifier,row in rows.items())} for name,rows in by_model.items()}}
