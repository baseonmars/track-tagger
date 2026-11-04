"""Comparison-only whitespace normalization; frozen gold and raw outputs stay intact."""
from .scoring import score as score_v2

SCORER_VERSION=3

def comparison_view(value):
    result={**value}
    def normalized(text):return ' '.join(text.split()) if isinstance(text,str) else text
    for field in ('artist','date','venue','city'):
        if field in result:result[field]=normalized(result[field])
    if 'titles' in result:result['titles']={k:normalized(v) for k,v in result['titles'].items()}
    if 'mappings' in result:result['mappings']={k:{**v,'title':normalized(v['title'])} for k,v in result['mappings'].items()}
    return result

def score(output,oracle,files):
    result=score_v2(comparison_view(output),comparison_view(oracle),files)
    result['score_version']=SCORER_VERSION
    return result
