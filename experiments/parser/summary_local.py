"""Text-free local run summary with eligible all-FLAC strata."""
import argparse
import json
import math
from collections import Counter
from pathlib import Path
from .runner import read_cases,oracle
from .scorer_v3 import score

def main():
    p=argparse.ArgumentParser();p.add_argument('results',type=Path);p.add_argument('labels',type=Path);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
    cases={c['id']:c for c in read_cases(a.labels)};rows=read_cases(a.results);groups={'all':Counter(),'all_flac':Counter()}; resource=[];errors=Counter()
    for row in rows:
        c=cases[row['id']]; eligible=bool(row.get('accuracy_eligible')); isflac=bool(c['files']) and all(x.lower().endswith('.flac') for x in c['files'])
        if row.get('error'): errors[row['error']]+=1
        if (row.get('resources') or {}).get('server_cpu_seconds') is not None: resource.append(row['resources']['server_cpu_seconds'])
        for group in ('all','all_flac'):
            if group=='all_flac' and not isflac: continue
            k=groups[group];k['requested']+=1;k['errors']+=bool(row.get('error'))
            if not eligible:continue
            k['eligible']+=1
            for stage in ('raw','validated'):
                out=row.get(stage,{'review':True});metrics=score(out,oracle(c),c['files'])
                if row.get('error'):metrics['exact']=False
                for field in ('exact','correct_titles','expected_titles','correct_mappings','expected_mappings','wrong_mappings','incorrect_accepted_mappings','false_ready','review'):
                    k[stage+'_'+field]+=int(metrics[field])
                if not out.get('review',True) and not metrics['exact']:k[stage+'_accepted_nonexact']+=1
    result={'strata':{k:dict(v) for k,v in groups.items()},'error_types':dict(errors),'resource_samples':len(resource),'server_cpu_p50_seconds':sorted(resource)[math.ceil(.50*len(resource))-1] if resource else None,'server_cpu_p95_seconds':sorted(resource)[math.ceil(.95*len(resource))-1] if resource else None,'note':'Missing/error requests remain in eligible denominators. ValueError is not classified further without independent evidence. Accepted nonexact is not proof of incorrect mapping.'}
    if a.output.exists():raise SystemExit('refusing to overwrite output')
    a.output.write_text(json.dumps(result,indent=2,sort_keys=True)+'\n');print(json.dumps(result,sort_keys=True))
if __name__=='__main__':main()
