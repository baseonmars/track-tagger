"""Freeze fresh source membership before held-out annotation or inference."""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
from ..path_guard import require_external
import random
import sqlite3
from ..acquire import normalize_packages, atomic_json
from ..hosted import canonical
from .labels import normalized_case, source_hash
from .corpus import text,signatures
from .components import keys,verify_split,groups

def check_proposed_binding(proposed,manifest):
    manifest=dict(manifest);digest=manifest.pop('manifest_sha256')
    if hashlib.sha256(canonical(manifest).encode()).hexdigest()!=digest or proposed.get('manifest_sha256')!=digest:raise ValueError('proposed manifest changed')
    public={c['id']:c for c in manifest['cases']};private={c['id']:c for c in proposed['cases']}
    if len(public)!=len(manifest['cases'])or len(private)!=len(proposed['cases'])or set(public)!=set(private):raise ValueError('proposed source membership changed')
    for id,c in private.items():
        row=public[id]
        if (c['group'],c['split'],c.get('source_input_sha256'),c.get('_source_manifest_sha256'))!=(row['group'],row['split'],row['source_input_sha256'],digest)or source_hash(c)!=row['source_input_sha256']:raise ValueError('proposed partition or source changed')
        if c['files']!=row['files']or[(n['name'],hashlib.sha256(n['text'].encode()).hexdigest())for n in c['notes']]!=[(n['name'],n['normalized_text_sha256'])for n in row['notes']]:raise ValueError('proposed note or inventory changed')
    return digest

def freeze(root,old_root,old_manifest,propose=False):
    root=require_external(root)
    output=root/('proposed-corpus.json'if propose else 'frozen-corpus.json')
    previous=None
    if output.exists():
        if not propose or(root/'frozen-corpus.json').exists():raise ValueError('source split already frozen')
        database=root/'run/budget.sqlite'
        if database.exists():
            with sqlite3.connect(database.as_uri()+'?mode=ro',uri=True)as db:
                if db.execute("SELECT count(*)FROM calls WHERE label LIKE 'annotation-v2-independent-%'").fetchone()[0]:raise ValueError('cannot revise membership after heldout source annotations')
        previous=json.loads(output.read_text());old_digest=previous['manifest_sha256'];history=root/'source-proposal-history'/old_digest;history.mkdir(parents=True,exist_ok=False)
        for name in ('proposed-corpus.json','proposed-source-split-manifest.json','proposed-component-audit.json'):(root/name).rename(history/name)
        atomic_json(root/'source-proposal-revision.json',{'previous_private':str(history/'proposed-corpus.json'),'reason':'independent source-only audit exclusions before heldout labels/predictions','prior_manifest_sha256':old_digest})
    elif propose and(root/'source-proposal-revision.json').exists():
        previous=json.loads(Path(json.loads((root/'source-proposal-revision.json').read_text())['previous_private']).read_text())
    if not propose and (root/'proposed-corpus.json').exists():
        proposed=json.loads((root/'proposed-corpus.json').read_text())
        manifest=json.loads((root/'proposed-source-split-manifest.json').read_text())
        digest=check_proposed_binding(proposed,manifest)
        approval=json.loads((root/'source-audit-approval.json').read_text())
        if approval.get('approved')is not True or approval.get('source_manifest_sha256')!=digest:raise ValueError('exact proposal has not passed independent source audit')
        current={c['id']:c for c in normalize_packages(root)if c['id']in{r['id']for r in proposed['cases']}}
        for c in proposed['cases']:
            if source_hash(c)!=source_hash(current[c['id']]) or c.get('_source_manifest_sha256')!=digest:raise ValueError('source changed after proposed audit')
        pinned_path=root/'train-sources-pinned.json'
        if pinned_path.exists():
            pinned=json.loads(pinned_path.read_text())
            binding=[{'id':c['id'],'group':c['group'],'split':c['split'],'source_input_sha256':source_hash(c)}for c in proposed['cases']if c['split']=='train']
            if hashlib.sha256(canonical(binding).encode()).hexdigest()!=pinned['training_membership_sha256']:raise ValueError('training membership changed after annotation dispatch')
        atomic_json(output,proposed);atomic_json(root/'source-split-manifest.json',manifest)
        for split in ('train','validation','test'):atomic_json(root/(split+'-sources.json'),{'source_manifest_sha256':digest,'cases':[c for c in proposed['cases']if c['split']==split]})
        atomic_json(root/'component-audit.json',manifest['component_audit'])
        return {'counts':manifest['split_counts'],'source_manifest_sha256':digest,'independent_audit_approved':True}
    strict=json.loads((root/'acquisition-state.json').read_text())['accepted']
    pilot=json.loads((root/'pilot20-training.json').read_text())['cases'];pilot_ids={c['id']for c in pilot}
    if len(set(strict)-pilot_ids)<250:raise ValueError('need250 strict fresh held-out packages plus pilot20')
    exclusions_path=root/'source-audit-exclusions.json'
    source_audit=json.loads(exclusions_path.read_text())if exclusions_path.exists()else {}
    exclusions=source_audit.get('excluded_ids',[]);manual_edges=source_audit.get('manual_edges',[])
    def load(id,where):
        package=json.loads((where/'packages'/id/'package.json').read_text());return package,text(package,where)
    strict_records={id:load(id,root)for id in strict}
    old_ids={json.loads(l)['package_id']for l in old_manifest.read_text().splitlines()if l.strip()}
    old_records={id:load(id,old_root)for id in old_ids}
    old_keys=set()
    for package,value in old_records.values():old_keys.update(keys(package,value))
    component_pool=groups(strict_records,manual_edges);members={}
    for id,group in component_pool.items():members.setdefault(group,[]).append(id)
    rejected_groups=set()
    for id,(package,value)in strict_records.items():
        if id in pilot_ids or id in exclusions or keys(package,value)&old_keys:rejected_groups.add(component_pool[id])
    available=[group for group in sorted(members)if group not in rejected_groups]
    random.Random(2026110402).shuffle(available)
    eligible=sum(len(members[group])for group in available)
    atomic_json(root/'source-selection-progress.json',{'strict_pool':len(strict),'eligible_heldout_cases':eligible,'needed':250,'source_audit_exclusions':len(exclusions),'excluded_or_training_groups':len(rejected_groups)})
    if eligible<250:raise ValueError('need250 group-isolated fresh heldout after old/source audit exclusions')
    def select(target,pool):
        if target==0:return []
        reachable={0:[]}
        for group in pool:
            size=len(members[group])
            for amount,chosen in sorted(list(reachable.items()),reverse=True):
                if amount+size<=target and amount+size not in reachable:reachable[amount+size]=chosen+[group]
            if target in reachable:return reachable[target]
        raise ValueError('cannot achieve exact split sizes with whole components')
    preserved={'test':[],'validation':[]}
    if previous:
        prior_by_group={}
        for c in previous['cases']:
            group=component_pool.get(c['id'])
            if c['split']in preserved and group in available:prior_by_group.setdefault(group,set()).add(c['split'])
        for group,splits in prior_by_group.items():
            if len(splits)==1:preserved[next(iter(splits))].append(group)
    kept=set(preserved['test']+preserved['validation'])
    test_groups=preserved['test']+select(200-sum(len(members[g])for g in preserved['test']),[g for g in available if g not in kept])
    validation_groups=preserved['validation']+select(50-sum(len(members[g])for g in preserved['validation']),[g for g in available if g not in set(test_groups+preserved['validation'])])
    assignment={id:'train'for id in pilot_ids}
    assignment.update({id:'test'for group in test_groups for id in members[group]})
    assignment.update({id:'validation'for group in validation_groups for id in members[group]})
    records={id:strict_records[id]for id in assignment}
    holdout_keys=set()
    for id,(package,value)in records.items():
        if assignment[id]!='train':holdout_keys.update(keys(package,value))
    fills=list(dict.fromkeys(json.loads((root/'training-fill-state.json').read_text())['accepted']+exclusions))
    training_only=[];rejected={}
    for id in fills:
        if len(training_only)>=230:break
        if id in old_ids or id in records:continue
        package,value=load(id,root)
        if keys(package,value)&holdout_keys:rejected[id]='edge_to_fresh_holdout';continue
        records[id]=(package,value);assignment[id]='train';training_only.append(id)
    if len(training_only)!=230:raise ValueError('need230 nonoverlapping training-only fills')
    component,audit=verify_split(records,assignment,old_records,training_only,manual_edges)
    selected=set(records);cases={c['id']:normalized_case(c)for c in normalize_packages(root)if c['id']in selected}
    for c in pilot:
        if source_hash(c)!=source_hash(cases[c['id']]):raise ValueError('pilot source changed')
    rows=[];public=[]
    for id in sorted(cases):
        c=cases[id];c.update(split=assignment[id],group=component[id]);c['source_input_sha256']=source_hash(c);rows.append(c)
        public.append({'id':id,'group':c['group'],'split':c['split'],'training_only_old_uploader':id in training_only,'source_input_sha256':c['source_input_sha256'],'files':c['files'],'notes':[{'name':n['name'],'normalized_text_sha256':hashlib.sha256(n['text'].encode()).hexdigest(),'source_encoding':n['source_encoding']}for n in c['notes']],'features':c['features'],'source':c['provenance']['source']})
    counts=dict(Counter(c['split']for c in rows))
    if counts!={'train':250,'validation':50,'test':200}:raise ValueError('unexpected split counts')
    audit.update(split_counts=counts,old_source_cases=len(old_records),selected_source_cases=len(records),rejected_training_fill=rejected,manual_nearest_neighbor_audit_required_before_labels=True,source_only_manual_edges=manual_edges,source_audit_exclusions_sha256=hashlib.sha256(exclusions_path.read_bytes()).hexdigest()if exclusions_path.exists()else None)
    manifest={'schema_version':2,'created_before_heldout_annotations_and_predictions':True,'split_counts':counts,'pilot_training_ids':sorted(pilot_ids),'training_only_ids':training_only,'cases':public,'component_audit':audit,'selection':'Frozen seeded source-only split. Heldout250 exclude old1200 relatives and remain component-disjoint from every training recording. New recordings from old uploaders are training-only. Heuristics do not prove all semantic families unseen.'}
    digest=hashlib.sha256(canonical(manifest).encode()).hexdigest()
    if not propose:
        approval=json.loads((root/'source-audit-approval.json').read_text())
        if approval.get('approved')is not True or approval.get('source_manifest_sha256')!=digest:raise ValueError('source audit has not approved exact proposed source membership')
    for c in rows:c['_source_manifest_sha256']=digest
    atomic_json(output,{'schema_version':2,'manifest_sha256':digest,'cases':rows})
    prefix='proposed-'if propose else ''
    atomic_json(root/(prefix+'source-split-manifest.json'),{**manifest,'manifest_sha256':digest})
    if not propose:
        for split in counts:atomic_json(root/(split+'-sources.json'),{'source_manifest_sha256':digest,'cases':[c for c in rows if c['split']==split]})
    atomic_json(root/(prefix+'component-audit.json'),audit)
    return {'counts':counts,'source_manifest_sha256':digest,'audit':audit}

def main():
    p=argparse.ArgumentParser();p.add_argument('root',type=Path);p.add_argument('--old-root',type=Path,required=True);p.add_argument('--old-manifest',type=Path,required=True);p.add_argument('--propose',action='store_true');a=p.parse_args();print(canonical(freeze(a.root,a.old_root,a.old_manifest,a.propose)))

if __name__=='__main__':main()
