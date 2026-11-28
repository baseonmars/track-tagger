"""Read-only accounting export; usage estimates and uncertain ceilings stay separate."""
import argparse
from collections import Counter,defaultdict
from decimal import Decimal
import json
from pathlib import Path
import sqlite3
import time
from .hosted import canonical,usage_cost,nonbillable_admission

RATES={'gpt-6-luna':(.1,.5),'gpt-6.1-sol':(2,10),'gpt-6-astra':(10,50)}

def report(state,require_drained=False,supplemental_outcomes=()):
    state=Path(state)
    with sqlite3.connect('file:'+str((state/'budget.sqlite').resolve())+'?mode=ro',uri=True) as db:
        db.row_factory=sqlite3.Row
        db.execute('BEGIN')
        calls=[dict(row) for row in db.execute('SELECT * FROM calls')]
        cap=db.execute('SELECT cap FROM config WHERE id=1').fetchone()[0]
        tables={row[0]for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        proofs={row['call_id']:json.loads(row['proof'])for row in db.execute('SELECT call_id,proof FROM billing_corrections')}if'billing_corrections'in tables else{}
        amendments=[{'old_total_cap_usd':row['old_cap']/1e6,'new_total_cap_usd':row['new_cap']/1e6,'authorized_epoch':row['authorized_at'],'applied_epoch':row['applied_at']}for row in db.execute('SELECT * FROM budget_amendments')]if'budget_amendments'in tables else[]
    if require_drained and any(c['status']=='reserved'for c in calls):raise ValueError('active provider reservations must drain before final accounting')
    outcomes={}
    for path in [*(state/'cache').glob('*.json'),*map(Path,supplemental_outcomes)]:
        value=json.loads(path.read_text());key=value.get('reservation_id')
        if key and (value.get('usage')or value.get('failure')or value.get('http_status')):
            if key in outcomes and outcomes[key]!=value:raise ValueError('ambiguous reservation outcome')
            outcomes[key]=value
    buckets=defaultdict(Decimal);counts=Counter();phases=defaultdict(lambda:{'calls':0,'statuses':Counter(),'ledger_footprint_usd':Decimal(0),'usage_derived_list_price_usd':Decimal(0)})
    for call in calls:
        charged=Decimal(call['charged'])/1000000;status=call['status'];phase=phases[call['label']];phase['calls']+=1;phase['statuses'][status]+=1;phase['ledger_footprint_usd']+=charged
        if status in ('uncertain','reserved','overrun'):
            bucket={'uncertain':'retained_unknown_maxima','reserved':'active_reservations','overrun':'overrun_charges'}[status];buckets[bucket]+=charged;counts[bucket]+=1;continue
        if status!='settled':raise ValueError('unknown ledger state')
        outcome=outcomes.get(call['id'],{});proof=proofs.get(call['id'],{});usage=outcome.get('usage')or proof.get('usage');model=next((m for m in RATES if call['label'].endswith(m)),None)
        if (nonbillable_admission(outcome.get('failure')or outcome)or nonbillable_admission(proof)) and charged==0:
            counts['known_nonbillable_admissions']+=1;continue
        if not usage or not model:
            buckets['settled_without_retained_usage_conservative']+=charged;counts['settled_without_retained_usage_conservative']+=1;continue
        inp,out=RATES[model];config={'model':model,'input_per_million':inp,'output_per_million':out}
        if outcome.get('cost_method')=='conservative-12h-all-input':config['cache_ttl']='12h'
        cost,method=usage_cost(usage,config)
        if cost>charged:raise ValueError('usage estimate exceeds accounted charge')
        bucket='usage_derived_conservative_estimates'if method.startswith('conservative-')else'known_usage_gross_list_price'
        buckets[bucket]+=cost;counts[bucket]+=1;buckets['ledger_rounding_or_retention_excess']+=charged-cost;phase['usage_derived_list_price_usd']+=cost
    total=Decimal(sum(c['charged']for c in calls))/1000000
    if sum(buckets.values())!=total:raise ValueError('accounting categories do not reconcile')
    for key in ('known_usage_gross_list_price','usage_derived_conservative_estimates','settled_without_retained_usage_conservative','ledger_rounding_or_retention_excess','retained_unknown_maxima','active_reservations','overrun_charges'):buckets[key]+=0
    return {'snapshot_epoch':time.time(),'final_drained_accounting':bool(require_drained),'total_budget_cap_usd':cap/1e6,'budget_footprint_usd':float(total),'budget_headroom_usd':float(Decimal(cap)/1000000-total),
            'categories_usd':{key:float(value)for key,value in buckets.items()},'category_call_counts':dict(counts),'ledger_status_counts':dict(Counter(c['status']for c in calls)),
            'by_ledger_phase':{label:{**value,'statuses':dict(value['statuses']),'ledger_footprint_usd':float(value['ledger_footprint_usd']),'usage_derived_list_price_usd':float(value['usage_derived_list_price_usd'])}for label,value in sorted(phases.items())},
            'explicit_total_cap_amendments':amendments,'historical_correction_rows':len(proofs),'supplemental_outcome_sha256':{Path(p).name:__import__('hashlib').sha256(Path(p).read_bytes()).hexdigest()for p in supplemental_outcomes},'firecrawl_calls':0,'firecrawl_gross_cost_usd':0,
            'net_invoice_or_free_credit_consumption_usd':None,'interpretation':'gross usage list-price estimates, separate conservative estimates and retained uncertain maxima; combined footprint is not confirmed spend or an invoice; cache reuse is not debited twice; local compute and Codex agent costs are not metered here'}

def main():
    p=argparse.ArgumentParser();p.add_argument('state');p.add_argument('--require-drained',action='store_true');p.add_argument('--outcome',action='append',default=[]);a=p.parse_args();print(canonical(report(a.state,a.require_drained,a.outcome)))
if __name__=='__main__':main()
