#!/usr/bin/env python3
"""Generate structured, independently labelled synthetic recordings with Hypothesis."""
import argparse
import json
import random
from pathlib import Path
from hypothesis import given, settings, HealthCheck, strategies as st

FAMILIES = ['plain', 'colon', 'dash', 'parenthesis', 'dot', 'padded', 'set-heading', 'duration', 'indented', 'annotated']
WORDS = ['River', 'Moon', 'Stone', 'Winter', 'Garden', 'Silver', 'Morning', 'Echo', 'Harbor', 'Distant', 'Blue', 'Light', 'Rain', 'Wild', 'Golden', 'Road']

def make_case(seed):
    rng = random.Random(seed)
    family_idx = seed % len(FAMILIES)
    family = FAMILIES[family_idx]
    split = 'train' if family_idx < 6 else 'validation' if family_idx < 8 else 'test'
    scenario = (seed // 10) % 8
    artist = 'The ' + rng.choice(WORDS) + ' Ensemble'
    date = '%04d-%02d-%02d' % (rng.randrange(1970, 2024), rng.randrange(1, 13), rng.randrange(1, 28))
    venue = rng.choice(['Civic Hall', 'The Pavilion', 'River Theater', 'Town Auditorium'])
    city = rng.choice(['Portland, OR', 'Boston, MA', 'Austin, TX', 'Seattle, WA'])
    titles = [rng.choice(WORDS) + ' ' + rng.choice(WORDS) + ' ' + str(i) for i in range(1, 5)]
    if family == 'annotated': titles = [t + ' [with guest]' for t in titles]
    if scenario == 7: titles[-1] += ' and the Long Road'
    if scenario == 5: titles.append('Encore ' + rng.choice(WORDS))
    # This data model is the source of the expected answers; rendering never consults a parser.
    discs = [1, 1, 2, 2] if scenario == 3 else [1] * len(titles)
    nums = [1, 2, 1, 2] if scenario == 3 else list(range(1, len(titles)+1))
    files = ['d%dt%02d.flac' % (d, n) for d, n in zip(discs, nums)]
    mappings = {f: {'disc': d, 'number': n, 'title': t} for f, d, n, t in zip(files, discs, nums, titles)}
    expected = {'%d:%d' % (d, n): t for d, n, t in zip(discs, nums, titles)}
    unresolved = []
    review = scenario in (1, 2, 4, 7)
    features = [family]
    lines = [(artist, 'artist'), (date, 'date'), (venue, 'venue'), (city, 'city'), ('Setlist:', 'heading')]
    for i, (d, n, title) in enumerate(zip(discs, nums, titles)):
        if scenario == 3 and i in (0, 2): lines.append(('Disc %d:' % d, 'heading'))
        if scenario == 5 and i == 4: lines.append(('Encore:', 'heading'))
        prefix = ['%d ' % n, '%d: ' % n, '%d - ' % n, '%d) ' % n, '%d. ' % n, '%02d. ' % n, '%d. ' % n, '%02d. ' % n, '  %d. ' % n, '%d. ' % n][family_idx]
        suffix = ' [05:21]' if family == 'duration' else ''
        if scenario == 4 and i == 1: continue
        rendered_title = title.removesuffix(' and the Long Road') if scenario == 7 and i == 3 else title
        lines.append((prefix + rendered_title + suffix, 'track'))
        if scenario == 7 and i == 3: lines.append(('  and the Long Road', 'continuation'))
    if scenario == 1:
        lines.append(('2. Contradictory Song', 'track')); expected.pop('1:2'); unresolved.append('1:2'); mappings[files[1]]['title'] = ''; features.append('duplicate_number')
    if scenario == 2:
        lines.append(('Date: 2004-06-18', 'date')); features.append('conflicting_dates'); date = ''
    if scenario == 3: features.append('multidisc')
    if scenario == 4:
        expected.pop('1:2'); unresolved.append('1:2'); mappings[files[1]]['title'] = ''; features.append('missing_number')
    if scenario == 5: lines.append(('Thanks for listening.', 'other')); features.append('encore')
    if scenario == 6: lines.extend([('Transfer Notes:', 'heading'), ('Transferred 2017-09-20 using equipment.', 'technical')]); features.append('technical')
    if scenario == 7: features.extend(['wrapped_title', 'ambiguous_continuation'])
    notes = [{'name':'show.txt', 'text':'\n'.join(x for x, _ in lines)+'\n', 'encoding':'utf-8'}]
    if seed % 31 == 0:
        notes.append({'name':'info.txt', 'text':'Source: café audience recording\n', 'encoding':'windows-1252'}); features.append('multiple_notes_encoding')
    return {'id':'synthetic-%06d' % seed, 'group':'template-'+family, 'split':split, 'notes':notes, 'files':files,
            'oracle':{'artist':artist,'date':date,'venue':venue,'city':city,'titles':expected,'mappings':mappings,'review':review,'unresolved':unresolved},
            'features':features, 'provenance':{'kind':'synthetic','label_status':'generated_not_agent_reviewed','seed':seed,'family':family},
            'line_labels': [label for _, label in lines]}

def generate(path, count):
    cases=[]
    @settings(max_examples=count, derandomize=True, database=None, deadline=None, suppress_health_check=list(HealthCheck))
    @given(st.integers(min_value=0,max_value=2**31-1))
    def collect(seed): cases.append(make_case(seed))
    collect()
    with path.open('w', encoding='utf-8') as out:
        for case in cases: out.write(json.dumps(case, ensure_ascii=False, sort_keys=True)+'\n')
    print(json.dumps({'cases':len(cases),'splits':{s:sum(c['split']==s for c in cases) for s in ('train','validation','test')},'families':{f:sum(c['group']=='template-'+f for c in cases) for f in FAMILIES}},sort_keys=True))
if __name__=='__main__':
    p=argparse.ArgumentParser(); p.add_argument('--output',type=Path,required=True); p.add_argument('--count',type=int,default=5000); args=p.parse_args(); generate(args.output,args.count)
