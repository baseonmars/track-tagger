"""Verify replayed inputs, join dequoted gold, and reproduce public metric aggregates."""
import argparse
import hashlib
from pathlib import Path
from .hosted import canonical
from .runner import read_cases,report,oracle
from .scorer_v3 import score

def sha(value):return hashlib.sha256(canonical(value).encode()).hexdigest()

def join(replayed,gold,output):
    output=Path(output).resolve()
    if any((parent/'.git').exists() for parent in [output.parent,*output.parents]):raise ValueError('joined original notes require an external output path')
    if output.exists():raise ValueError('joined artifact already exists')
    sources=read_cases(replayed);labels=read_cases(gold)
    if len({r['id'] for r in sources})!=len(sources) or len({r['id'] for r in labels})!=len(labels):raise ValueError('duplicate IDs')
    by_id={r['id']:r for r in sources};cases=[]
    for label in labels:
        case=by_id.get(label['id'])
        if case is None or sha({'notes':case['notes'],'files':case['files']})!=label['source_input_sha256']:raise ValueError('replayed input hash differs from frozen gold')
        if any(case.get(k)!=label.get(k) for k in ('split','group')):raise ValueError('replayed split/group differs')
        cases.append({**case,'oracle':label.get('oracle'),'label_status':label['label_status'],'public_gold_redactions':label.get('public_export_redactions',[])})
    value={'labels_frozen':True,'public_gold_replay':True,'label_sha256':sha(cases),'public_gold_sha256':hashlib.sha256(Path(gold).read_bytes()).hexdigest(),
           'astra_ids':[r['id'] for r in labels if r.get('selected_common40')],'pilot_ids':[r['id'] for r in labels if r.get('selected_pilot30')],'cases':cases}
    output.parent.mkdir(parents=True,exist_ok=True);output.write_text(canonical(value)+'\n')
    return {'joined':len(cases),'sha256':hashlib.sha256(output.read_bytes()).hexdigest()}

def prediction_rows(path):
    rows=read_cases(path)
    if len({r.get('id') for r in rows})!=len(rows):raise ValueError('duplicate prediction IDs')
    return rows

def aggregate(path):
    prediction_rows(path)
    return report(path)

def check(predictions,joined):
    cases={c['id']:c for c in read_cases(joined)};rows=prediction_rows(predictions);excluded=[];mismatches=[];checked=0
    for row in rows:
        case=cases.get(row['id'])
        if case is None:raise ValueError('prediction ID absent from joined gold')
        if row.get('public_export_redactions') or case.get('public_gold_redactions'):
            excluded.append(row['id']);continue
        if not row.get('accuracy_eligible'):continue
        for stage in ('raw','validated'):
            calculated=score(row.get(stage,{'review':True}),oracle(case),case['files'])
            if 'error'in row:calculated['exact']=False
            saved=row[stage+'_score']
            if calculated!=saved:mismatches.append({'id':row['id'],'stage':stage,'differing_metrics':sorted(k for k in set(calculated)|set(saved) if calculated.get(k)!=saved.get(k))})
        checked+=1
    return {'stored_metric_aggregate':aggregate(predictions),'unredacted_eligible_rows_semantically_checked':checked,'redacted_rows_excluded_from_semantic_check':excluded,'semantic_metric_mismatches':mismatches,
            'aggregate_method':'retained per-case scores reproduce full published aggregate; redacted values cannot establish byte-exact rescoring'}

def main():
    p=argparse.ArgumentParser();sub=p.add_subparsers(dest='command',required=True)
    j=sub.add_parser('join');j.add_argument('replayed');j.add_argument('gold');j.add_argument('external_output')
    c=sub.add_parser('check');c.add_argument('predictions');c.add_argument('joined')
    a=sub.add_parser('aggregate');a.add_argument('predictions')
    args=p.parse_args()
    result=join(args.replayed,args.gold,args.external_output) if args.command=='join' else check(args.predictions,args.joined) if args.command=='check' else aggregate(args.predictions)
    print(canonical(result))
    if result.get('semantic_metric_mismatches'):raise SystemExit(1)
if __name__=='__main__':main()
