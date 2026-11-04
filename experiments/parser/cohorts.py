"""Explicit, budget-only Astra expansion; default remains the frozen common40."""
import hashlib
import json
from pathlib import Path
from decimal import Decimal
from .budget import micros


def test_cohort(cases,manifest,model,cohort,ledger,labels_path,prompt,stage_label,policy_path=None,forecast_path=None):
    selected=cohort or ('common40' if model=='gpt-6-astra' else 'full200')
    if selected=='common40':
        ids=set(manifest['astra_ids']);return [c for c in cases if c['id'] in ids]
    if model!='gpt-6-astra':return cases
    if not policy_path or not forecast_path:raise ValueError('Astra full200 requires frozen expansion policy and approved budget forecast')
    policy=json.loads(Path(policy_path).read_text());forecast=json.loads(Path(forecast_path).read_text())
    if policy.get('model')!=model or policy.get('minimum_remaining_reserve_usd')!=5 or policy.get('cost_padding_multiplier')!=1.25 or policy.get('decision_basis')!='budget/time only; no quality threshold':raise ValueError('unrecognized expansion policy')
    ids={c['id'] for c in cases};extra=ids-set(manifest['astra_ids'])
    if len(ids)!=200 or len(extra)!=160 or any(c.get('provenance',{}).get('kind')=='synthetic' for c in cases):raise ValueError('policy applies only to frozen real200')
    if forecast.get('approved')is not True or forecast.get('required_core_completed')is not True:raise ValueError('required core must complete before optional expansion')
    checks={'policy_sha256':hashlib.sha256(Path(policy_path).read_bytes()).hexdigest(),'frozen_labels_sha256':hashlib.sha256(Path(labels_path).read_bytes()).hexdigest(),'prompt_sha256':hashlib.sha256(prompt.encode()).hexdigest()}
    if any(forecast.get(k)!=v for k,v in checks.items()) or set(forecast.get('extra_case_ids',[]))!=extra:raise ValueError('forecast inputs differ from frozen run')
    average=Decimal(str(forecast['measured_mean_cost_usd']));padded=Decimal(str(forecast['padded_additional_ceiling_usd']))
    if not average.is_finite() or not padded.is_finite() or average<0 or padded<average*len(extra)*Decimal('1.25'):raise ValueError('forecast must pad measured cost by at least25percent')
    summary=ledger.summary()
    if summary['statuses'].get('reserved',0) or micros(summary['cap_usd'])-micros(summary['charged_usd'])-micros(padded)<micros(5):raise ValueError('expansion must retain5dollars after padded forecast and no other inflight work')
    with ledger.connect() as db:used=db.execute('SELECT coalesce(sum(charged),0) FROM calls WHERE label=?',(stage_label,)).fetchone()[0]
    cap=(used+micros(padded))/1e6
    ledger.allocate_stage(stage_label,cap,'user-authorized optional realAstra200 expansion; policy '+checks['policy_sha256'])
    return cases
