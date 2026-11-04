"""Strict response validation and conservative filename reconciliation."""
import re
import datetime

SCORER_VERSION = 2

def semantic_mapping(value):
    return {'disc':0,'number':0,'title':''} if not value.get('title') else value

FIELDS = {'artist', 'date', 'venue', 'city', 'titles', 'mappings', 'review', 'unresolved', 'evidence'}

def validate(value):
    if not isinstance(value, dict) or set(value) - FIELDS:
        raise ValueError('response must contain only schema fields')
    for field in ('artist', 'date', 'venue', 'city'):
        if field in value and not isinstance(value[field], str):
            raise ValueError('metadata fields must be strings')
    if not isinstance(value.get('review'), bool):
        raise ValueError('review must be boolean')
    if not isinstance(value.get('titles'), dict) or not isinstance(value.get('mappings'), dict):
        raise ValueError('titles and mappings required')
    for key, title in value['titles'].items():
        if not isinstance(key,str) or not isinstance(title, str):
            raise ValueError('invalid title identifier')
    for path, entry in value['mappings'].items():
        if not isinstance(path, str) or not isinstance(entry, dict) or set(entry) != {'disc', 'number', 'title'}:
            raise ValueError('invalid mapping')
        if any(type(entry[k]) is not int or entry[k] < 0 for k in ('disc', 'number')) or not isinstance(entry['title'], str):
            raise ValueError('invalid mapping value')
    if not isinstance(value.get('unresolved', []), list) or any(not isinstance(x, str) for x in value.get('unresolved', [])):
        raise ValueError('invalid unresolved identifiers')
    if not isinstance(value.get('evidence', []), list):
        raise ValueError('evidence must be an array')
    for item in value.get('evidence', []):
        if not isinstance(item, dict) or set(item) != {'field','note','quote'} or any(not isinstance(v,str) for v in item.values()):
            raise ValueError('invalid source evidence')
    return value

def identifier(path):
    name = path.rsplit('/', 1)[-1]
    match = re.search(r'(?:^|[._ -])(?:d|s)(\d+)t(\d+)(?=[._ -]|$)', name, re.I)
    if match:
        return tuple(map(int, match.groups()))
    if re.match(r'\d{4}[-_.]\d{2}[-_.]\d{2}', name):
        return None
    match = re.match(r'(?:(\d+)-)?(\d+)(?=[ ._-])', name)
    if match:
        return int(match[1] or 1), int(match[2])
    match = re.search(r'(?:^|[._ -])t(\d+)(?=[._ -]|$)', name, re.I)
    return (1, int(match[1])) if match else None

def reconcile(output, files, identifiers=None):
    validate(output)
    result = {**output, 'mappings': {}, 'unresolved': list(output.get('unresolved', []))}
    if output.get('unresolved'):result['review']=True
    invalid_titles={k for k in output['titles'] if not re.fullmatch(r'[1-9]\d*:[1-9]\d*',k)}
    if invalid_titles:
        result['titles']={k:v for k,v in output['titles'].items() if k not in invalid_titles}
        result['review']=True
    date = output.get('date', '')
    if date and not valid_date(date):
        result['date'] = ''
        result['review'] = True
    if any(not path.lower().endswith('.flac') for path in files) or not files or not output.get('artist') or not output.get('date') or not output.get('venue'):
        result['review'] = True
    ids = [identifier(path) for path in files] if identifiers is None else [tuple(x) if x and x[1]>0 else None for x in identifiers]
    ids = [None if re.match(r'\d{4}[-_.]\d{2}[-_.]\d{2}',path.rsplit('/',1)[-1]) and not re.search(r'[ds]\d+t\d+|(?:^|[._ -])t\d+',path,re.I) else ident for path,ident in zip(files,ids)]
    for path, ident in zip(files, ids):
        key = '%s:%s' % ident if ident else ''
        title = output['titles'].get(key, '')
        if not ident or ids.count(ident) != 1 or not title:
            result['mappings'][path] = {'disc': 0, 'number': 0, 'title': ''}
            result['review'] = True
        else:
            result['mappings'][path] = {'disc': ident[0], 'number': ident[1], 'title': title}
    if set(output['titles']) - {('%s:%s' % i) for i in ids if i}:
        result['review'] = True
    return result

def score(output, oracle, files):
    expected = {k:semantic_mapping(v) for k,v in oracle.get('mappings', {}).items()}
    got = {k:semantic_mapping(v) for k,v in output.get('mappings', {}).items()}
    wrong = sum(bool(v.get('title')) and v != expected.get(k) for k, v in got.items())
    titles = oracle.get('titles', {})
    known = set(oracle) & (FIELDS - {'evidence','unresolved','mappings'})
    exact = all(output.get(k, '' if k in ('artist','date','venue','city') else []) == oracle[k] for k in known)
    if 'mappings' in oracle:exact=exact and got==expected
    # Free-text explanations are not semantic labels; compare canonical unresolved track IDs only.
    expected_unresolved={x for x in oracle.get('unresolved',[]) if re.fullmatch(r'[1-9]\d*:[1-9]\d*',x)}
    actual_unresolved={x for x in output.get('unresolved',[]) if re.fullmatch(r'[1-9]\d*:[1-9]\d*',x)}
    fields = {k:output.get(k,'') == oracle[k] for k in ('artist','date','venue','city') if k in oracle}
    invented = sum(bool(output.get(k)) and output.get(k) != oracle.get(k) for k in ('artist','date','venue','city') if k in oracle)
    invented += sum(bool(v) and oracle.get('titles',{}).get(k) != v for k,v in output.get('titles',{}).items())
    return {'score_version':SCORER_VERSION,'unresolved_ids_match':expected_unresolved==actual_unresolved,'exact':exact, 'metadata_fields':fields, 'invented_info':invented,
            'correct_titles': sum(output.get('titles', {}).get(k) == v for k,v in titles.items()), 'expected_titles': len(titles),
            'correct_mappings': sum(got.get(k) == v for k,v in expected.items()), 'expected_mappings': len(expected),
            'wrong_mappings': wrong, 'incorrect_accepted_mappings': wrong if not output.get('review', True) else 0,
            'false_ready': not output.get('review', True) and oracle.get('review', False),
            'missed_ambiguity':not output.get('review', True) and oracle.get('review',False), 'review': output.get('review', True)}



def evidence_score(output, notes):
    sources = {n['name']: n['text'] for n in notes}
    entries = output.get('evidence', [])
    supported = [e for e in entries if e.get('quote') and e.get('quote') in sources.get(e.get('note'), '')]
    populated = {k for k in ('artist','date','venue','city') if output.get(k)}
    populated |= {'titles.' + k for k,v in output.get('titles',{}).items() if v}
    supported_fields = {e['field'] for e in supported}
    return {'evidence_entries':len(entries),'invalid_quotes':len(entries)-len(supported),
            'unsupported_populated_fields':len(populated-supported_fields)}


def valid_date(value):
    if not isinstance(value,str) or not re.fullmatch(r'\d{4}(?:-\d{2}(?:-\d{2})?)?',value):
        return False
    try:
        datetime.date.fromisoformat(value + ('-01-01' if len(value)==4 else '-01' if len(value)==7 else ''))
        return True
    except ValueError:
        return False
