"""Source-only component checks used before any fresh held-out labels."""
from collections import defaultdict
import hashlib
import re
from .corpus import uploader,performance,signatures,aliases

def keys(package,value):
    own={('note',hashlib.sha256(value.casefold().encode()).hexdigest())}
    u=uploader(package)
    if u:own.add(('uploader',u))
    own.update(('performance',p)for p in performance(package))
    own.update(('technical-template',p)for p in signatures(value))
    own.update(('source-email-sha256',hashlib.sha256(email.casefold().encode()).hexdigest())for email in re.findall(r'[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}',value))
    return own

def groups(records,manual_edges=()):
    parent={id:id for id in records};bykey={}
    def find(id):
        while parent[id]!=id:parent[id]=parent[parent[id]];id=parent[id]
        return id
    def union(a,b):
        a,b=find(a),find(b)
        if a!=b:parent[max(a,b)]=min(a,b)
    for id,(package,value)in sorted(records.items()):
        for key in keys(package,value):
            if key in bykey:union(id,bykey[key])
            else:bykey[key]=id
    for a,b in manual_edges:
        if a in records and b in records:union(a,b)
    members=defaultdict(list)
    for id in records:members[find(id)].append(id)
    result={}
    for ids in members.values():
        group='component-'+hashlib.sha256('\0'.join(sorted(ids)).encode()).hexdigest()[:20]
        for id in ids:result[id]=group
    return result

def verify_split(records,assignment,old_records,training_only,manual_edges=()):
    if set(records)!=set(assignment):raise ValueError('source assignment mismatch')
    if set(training_only)-set(records):raise ValueError('training-only source absent')
    if any(assignment[id]!='train'for id in training_only):raise ValueError('training-only source in held-out split')
    component=groups(records,manual_edges);splits=defaultdict(set)
    for id,group in component.items():splits[group].add(assignment[id])
    if any(len(x)>1 for x in splits.values()):raise ValueError('source component crosses split')
    old_keys=set()
    for p,value in old_records.values():old_keys.update(keys(p,value))
    old_overlap=[]
    for id,(p,value)in records.items():
        if assignment[id]!='train'and keys(p,value)&old_keys:old_overlap.append(id)
    if old_overlap:raise ValueError('held-out source related to old corpus')
    artists=defaultdict(int)
    for package,_ in records.values():
        for artist in aliases(package['artist']):artists[artist]+=1
    if max(artists.values(),default=0)>len(records)*.05:raise ValueError('artist exceeds five-percent cap')
    return component,{'source_components':len(set(component.values())),'cross_split_components':0,'heldout_old_component_overlaps':0,'training_only_cases':len(training_only),'largest_artist_alias_cases':max(artists.values(),default=0),'component_predicates':['same uploader','same normalized Unicode artist alias and exact date','same casefolded full notes','shared technical-source line (at least seven words) or twelve-word technical shingle','same lower-case source email SHA256','source-only manually reviewed family edges'],'limitations':'Source predicates are conservative heuristics; uploader is provenance not proof of author identity; source emails can include shared band contacts rather than authors; paraphrased distinctive templates may evade detection.'}
