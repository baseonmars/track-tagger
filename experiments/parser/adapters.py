"""Subprocess baseline and review-only hybrid adapters."""
import json
import subprocess
import time
from .scoring import FIELDS

class BaselineAdapter:
    def __init__(self, executable):
        self.executable=str(executable)
        self.config={'kind':'catalog-baseline','executable':self.executable}
    def parse(self,case,prompt,phase):
        start=time.monotonic()
        result=subprocess.run([self.executable],input=json.dumps(case)+'\n',text=True,capture_output=True,timeout=120,check=True)
        value=json.loads(result.stdout)
        output={k:v for k,v in value.items() if k in FIELDS}
        output.setdefault('unresolved',[]);output.setdefault('evidence',[])
        return {'output':output,'usage':{},'cache_hit':False,'latency_seconds':time.monotonic()-start,
                'provenance':{'metadata':'catalog-validated','reasons':value.get('review_reasons',[])}}

class HybridAdapter:
    def __init__(self,baseline,fallback):
        self.baseline,self.fallback=baseline,fallback
        self.config={'kind':'review-fallback','baseline':baseline.config,'fallback':fallback.config,**({'identifier_executable':fallback.config['identifier_executable']} if 'identifier_executable' in fallback.config else {})}
    def parse(self,case,prompt,phase):
        first=self.baseline.parse(case,prompt,phase)
        if not first['output']['review']:
            first['hybrid_used_fallback']=False
            return first
        result=self.fallback.parse(case,prompt,phase+'-review-fallback')
        result['hybrid_used_fallback']=True
        result['baseline_latency_seconds']=first['latency_seconds']
        return result


def catalog_identifiers(executable,files):
    result=subprocess.run([str(executable),'--identifiers'],input=json.dumps(files)+'\n',capture_output=True,text=True,timeout=30,check=True)
    rows=json.loads(result.stdout)
    if len(rows)!=len(files):raise ValueError('identifier count mismatch')
    return [(r['disc'],r['number']) for r in rows]
