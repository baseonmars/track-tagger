"""Append-only historical billing correction, run while paid dispatch is halted."""
import argparse
import hashlib
import json
from pathlib import Path
import sqlite3
import time
from decimal import Decimal
from .budget import micros
from .hosted import canonical,usage_cost,CACHED_INPUT_RATES

RATES={'gpt-6-luna':(.1,.5),'gpt-6.1-sol':(2,10),'gpt-6-astra':(10,50)}
VERSION='cache-write-reconciliation-v1'

def plan(state):
    state=Path(state)
    with sqlite3.connect(state/'budget.sqlite') as db:
        if not db.execute('SELECT count(*) FROM halt').fetchone()[0]:raise ValueError('paid dispatch must be halted')
        if db.execute("SELECT count(*) FROM calls WHERE status='reserved'").fetchone()[0]:raise ValueError('inflight calls must drain')
        calls=[dict(zip(('id','label','reserved','charged','status','created'),row)) for row in db.execute('SELECT id,label,reserved,charged,status,created FROM calls')]
    matched={};cache_count=0
    for path in sorted((state/'cache').glob('*.json'),key=lambda p:p.stat().st_mtime):
        row=json.loads(path.read_text());usage=row.get('usage',{});actual=row.get('actual_model')
        model=next((m for m in RATES if actual and actual.startswith(m)),None)
        if model is None or type(usage.get('input_tokens'))is not int or type(usage.get('output_tokens'))is not int:continue
        inp,out=map(lambda x:Decimal(str(x)),RATES[model]);cached=usage.get('input_tokens_details',{}).get('cached_tokens',0)
        legacy=micros(((usage['input_tokens']-cached)*inp+cached*CACHED_INPUT_RATES[model]+usage['output_tokens']*out)/1_000_000)
        legacy_uncached=micros((usage['input_tokens']*inp+usage['output_tokens']*out)/1_000_000)
        start=path.stat().st_mtime-row['latency_seconds']
        candidates=[call for call in calls if call['id'] not in matched and call['status']=='settled' and call['label'].endswith(model) and call['charged'] in (legacy,legacy_uncached) and abs(call['created']-start)<2]
        if len(candidates)!=1:raise ValueError('historical cache linkage missing or ambiguous')
        call=candidates[0];new,method=usage_cost(usage,{'model':model,'input_per_million':inp,'output_per_million':out})
        matched[call['id']]={'new_charged':max(call['charged'],micros(new)),'method':method,'cache_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'timestamp_difference_seconds':abs(call['created']-start),'usage':usage}
        cache_count+=1
    adjustments=[]
    for call in calls:
        proof=matched.get(call['id'])
        if proof is None:
            if call['status'] not in ('settled','uncertain'):raise ValueError('unexpected historical call state')
            # Every historical request used documented default30m. Repricing the full
            # old total/ceiling1.25x also overprices output, so remains conservative.
            proof={'new_charged':int((Decimal(call['charged'])*Decimal('1.25')).to_integral_value(rounding='ROUND_CEILING')),'method':'conservative-default30m-historical-total-or-ceiling'}
        adjustments.append({**call,**proof,'new_reserved':max(call['reserved'],proof['new_charged'])})
    return {'version':VERSION,'calls':len(calls),'matched_usage_outcomes':cache_count,'legacy_settled_without_usage':sum(c['status']=='settled' and c['id']not in matched for c in calls),'uncertain_outcomes_retained':sum(c['status']=='uncertain' for c in calls),'old_footprint_usd':sum(c['charged'] for c in calls)/1e6,'new_footprint_usd':sum(c['new_charged'] for c in adjustments)/1e6,'adjustments':adjustments}

def apply(state,value):
    with sqlite3.connect(Path(state)/'budget.sqlite',timeout=30) as db:
        db.execute('BEGIN IMMEDIATE')
        if not db.execute('SELECT count(*) FROM halt').fetchone()[0]:raise ValueError('dispatch hold removed')
        db.execute('CREATE TABLE IF NOT EXISTS billing_corrections (version TEXT,call_id TEXT,old_charged INTEGER,new_charged INTEGER,old_reserved INTEGER,new_reserved INTEGER,proof TEXT,created REAL,PRIMARY KEY(version,call_id))')
        for row in value['adjustments']:
            current=db.execute('SELECT charged,reserved,status FROM calls WHERE id=?',(row['id'],)).fetchone()
            if current!=(row['charged'],row['reserved'],row['status']):raise ValueError('ledger changed since reconciliation plan')
            db.execute('INSERT INTO billing_corrections VALUES (?,?,?,?,?,?,?,?)',(VERSION,row['id'],row['charged'],row['new_charged'],row['reserved'],row['new_reserved'],canonical(row),time.time()))
            db.execute('UPDATE calls SET charged=?,reserved=? WHERE id=?',(row['new_charged'],row['new_reserved'],row['id']))

def main():
    p=argparse.ArgumentParser();p.add_argument('state');p.add_argument('audit');p.add_argument('--apply',action='store_true');a=p.parse_args()
    value=plan(a.state)
    if Path(a.audit).exists():raise ValueError('reconciliation audit exists')
    Path(a.audit).write_text(canonical(value)+'\n')
    if a.apply:apply(a.state,value)
    print(canonical({k:v for k,v in value.items() if k!='adjustments'}))
if __name__=='__main__':main()
