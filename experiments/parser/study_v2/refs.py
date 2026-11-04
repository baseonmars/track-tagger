"""Text-free full-line labels and hash-checked source/context rehydration."""
import argparse
import hashlib
import json
from pathlib import Path
from ..hosted import canonical
from ..path_guard import require_external
from .labels import ROLES,lines,source_hash

def sha(value):return hashlib.sha256(value.encode()).hexdigest()
def read(path):return[json.loads(l)for l in Path(path).read_text().split('\n')if l.strip()]

def references(rows):
    out=[];seen=set()
    for row in rows:
        key=(row['id'],row['note'],row['line_index'])
        if key in seen:raise ValueError('duplicate line reference')
        seen.add(key)
        if len(set(row['roles']))!=len(row['roles'])or set(row['roles'])-set(ROLES)or type(row['role_disputed'])is not bool:raise ValueError('invalid reference roles')
        ref={k:row[k]for k in ('id','group','split','note','line_index','roles','label_status','source_input_sha256','uncertainty','conflict','mixed_scope','role_disputed')}
        ref.update(line_sha256=sha(row['line']),prev_sha256=sha(row['prev']),next_sha256=sha(row['next']))
        ref['spans']=[{'start':s['start'],'end':s['end'],'roles':s['roles'],'quote_sha256':sha(row['line'][s['start']:s['end']])}for s in row.get('spans',[])]
        out.append(ref)
    return out

def rehydrate(refs,cases):
    by_id={c['id']:c for c in cases};rows=[];seen=set()
    if len(by_id)!=len(cases):raise ValueError('duplicate replay source IDs')
    for ref in refs:
        key=(ref['id'],ref['note'],ref['line_index'])
        if key in seen:raise ValueError('duplicate reference')
        seen.add(key);case=by_id.get(ref['id'])
        if not case or(ref['group'],ref['split'],ref['source_input_sha256'])!=(case['group'],case['split'],source_hash(case)):raise ValueError('reference/source membership mismatch')
        notes={n['name']:n for n in case['notes']}
        if ref['note']not in notes or type(ref['line_index'])is not int:raise ValueError('unknown source line')
        note=lines(notes[ref['note']]['text']);i=ref['line_index']
        if not 0<=i<len(note):raise ValueError('source line out of bounds')
        line=note[i];prev=note[i-1]if i else'';next=note[i+1]if i+1<len(note)else''
        if(sha(line),sha(prev),sha(next))!=(ref['line_sha256'],ref['prev_sha256'],ref['next_sha256']):raise ValueError('source/context bytes changed')
        spans=[]
        for s in ref['spans']:
            if any(type(s[x])is not int for x in ('start','end'))or not 0<=s['start']<s['end']<=len(line)or sha(line[s['start']:s['end']])!=s['quote_sha256']:raise ValueError('source span bytes changed')
            spans.append({'line_index':i,'start':s['start'],'end':s['end'],'roles':s['roles']})
        rows.append({k:v for k,v in ref.items()if k not in ('line_sha256','prev_sha256','next_sha256','spans')}|{'line':line,'prev':prev,'next':next,'spans':spans})
    return rows

def main():
    p=argparse.ArgumentParser();sub=p.add_subparsers(dest='command',required=True)
    e=sub.add_parser('export');e.add_argument('rows',type=Path);e.add_argument('output',type=Path)
    r=sub.add_parser('rehydrate');r.add_argument('refs',type=Path);r.add_argument('sources',type=Path);r.add_argument('output',type=Path);r.add_argument('--expected-sha256',required=True)
    a=p.parse_args()
    if a.command=='export':rows=references(read(a.rows))
    else:
        require_external(a.output)
        if hashlib.sha256(a.refs.read_bytes()).hexdigest()!=a.expected_sha256:raise ValueError('trusted reference digest differs')
        value=json.loads(a.sources.read_text())if a.sources.suffix=='.json'else read(a.sources);cases=value['cases']if isinstance(value,dict)else value
        rows=rehydrate(read(a.refs),cases)
    if a.output.exists():raise ValueError('output exists')
    a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(''.join(canonical(r)+'\n'for r in rows))
    print(canonical({'rows':len(rows),'sha256':hashlib.sha256(a.output.read_bytes()).hexdigest(),'trust':'Published reference digest binds semantic labels; source/context hashes verify bytes, not label meaning.'}))

if __name__=='__main__':main()
