#!/usr/bin/env python3
"""Acquire complete public Archive.org note packages without downloading audio."""
import argparse
import concurrent.futures
import collections
import datetime
import hashlib
import json
from pathlib import Path, PurePosixPath
import random
import re
import time
import urllib.parse
import urllib.request

SCHEMA_VERSION = 1
AUDIO_EXT = {'.flac', '.shn', '.wav', '.aif', '.aiff', '.ape', '.mp3', '.ogg', '.m4a', '.wma', '.oga', '.opus'}
NOTE_EXT = {'.txt', '.text', '.nfo', '.md'}
MAX_NOTE = 4 * 1024 * 1024
MAX_PACKAGE = 16 * 1024 * 1024
USER_AGENT = 'TrackTaggerParserResearch/1.0 (metadata and text only; no audio)'


def utcnow():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def safe_relative(name):
    p = PurePosixPath(name)
    if not name or p.is_absolute() or '..' in p.parts or '\\' in name or '\x00' in name:
        raise ValueError('unsafe relative path')
    return str(p)


def fetch(url, limit=32 * 1024 * 1024, retries=3):
    for attempt in range(retries):
        try:
            request = urllib.request.Request(url, headers={'User-Agent': USER_AGENT})
            with urllib.request.urlopen(request, timeout=35) as response:
                data = response.read(limit + 1)
                if len(data) > limit:
                    raise ValueError('response exceeds size limit')
                return data
        except Exception:
            if attempt + 1 == retries:
                raise
            time.sleep(0.5 * (2 ** attempt))


def decode_note(data):
    if b'\x00' in data[:4096] and not data.startswith((b'\xff\xfe', b'\xfe\xff')):
        raise ValueError('binary note')
    for encoding in ('utf-8-sig', 'utf-16') if data.startswith((b'\xff\xfe', b'\xfe\xff')) else ('utf-8-sig', 'cp1252', 'latin-1'):
        try:
            return data.decode(encoding), encoding
        except UnicodeError:
            pass
    raise ValueError('cannot decode note')


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + '.tmp')
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    temporary.replace(path)


def select_files(metadata):
    originals = [f for f in metadata.get('files', []) if f.get('source') == 'original']
    notes, audio = [], []
    for f in originals:
        name = safe_relative(f['name'])
        ext = PurePosixPath(name).suffix.lower()
        if ext in NOTE_EXT:
            if int(f.get('size', 0)) > MAX_NOTE:
                raise ValueError('oversize original note')
            notes.append(f)
        if ext in AUDIO_EXT:
            audio.append(f)
    if not notes or not audio:
        raise ValueError('missing original notes or original audio file listing')
    if sum(int(n.get('size', 0)) for n in notes) > MAX_PACKAGE:
        raise ValueError('oversize original note package')
    return sorted(notes, key=lambda f:f['name']), sorted(audio, key=lambda f:f['name'])


def scalar(value):
    return '; '.join(map(str, value)) if isinstance(value, list) else str(value or '')


def acquire(identifier, root):
    if not re.fullmatch(r'[A-Za-z0-9_.-]+', identifier):
        raise ValueError('unsafe identifier')
    package_dir = root / 'packages' / identifier
    existing = package_dir / 'package.json'
    if existing.exists():
        package = json.loads(existing.read_text())
        metadata_bytes = (package_dir / 'archive-metadata.json').read_bytes()
        if digest(metadata_bytes) != package['archive_metadata_sha256']:
            raise ValueError('cached metadata hash mismatch')
        expected_notes, expected_audio = select_files(json.loads(metadata_bytes))
        if not package.get('complete') or {n['path'] for n in package['notes']} != {n['name'] for n in expected_notes} or {a['path'] for a in package['audio_files']} != {a['name'] for a in expected_audio}:
            raise ValueError('cached original inventory mismatch')
        if sum(n['size'] for n in package['notes']) > MAX_PACKAGE:
            raise ValueError('cached note package exceeds size limit')
        for note in package['notes']:
            if digest((package_dir / note['local_path']).read_bytes()) != note['sha256']:
                raise ValueError('cached note hash mismatch')
        return package
    metadata_url = 'https://archive.org/metadata/' + urllib.parse.quote(identifier, safe='')
    metadata_bytes = fetch(metadata_url)
    archive = json.loads(metadata_bytes)
    notes, audio = select_files(archive)
    meta = archive.get('metadata', {})
    downloaded = []
    for note in notes:
        name = note['name']
        url = 'https://archive.org/download/' + urllib.parse.quote(identifier, safe='') + '/' + urllib.parse.quote(name, safe='/')
        data = fetch(url, MAX_NOTE)
        if len(data) + sum(n['size'] for n in downloaded) > MAX_PACKAGE:
            raise ValueError('oversize downloaded note package')
        if 'size' in note and len(data) != int(note['size']):
            raise ValueError('note size mismatch')
        if note.get('md5') and hashlib.md5(data).hexdigest() != note['md5']:
            raise ValueError('note md5 mismatch')
        text, encoding = decode_note(data)
        if not text.strip():
            raise ValueError('empty original note')
        local = 'notes/' + safe_relative(name)
        path = package_dir / local
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        downloaded.append({'path':name,'local_path':local,'sha256':digest(data),'encoding':encoding,'size':len(data),'source_url':url,'archive_md5':note.get('md5'), 'acquired_at':utcnow()})
    package_dir.mkdir(parents=True, exist_ok=True)
    (package_dir / 'archive-metadata.json').write_bytes(metadata_bytes)
    package = {'schema_version':SCHEMA_VERSION,'package_id':identifier,'artist':scalar(meta.get('creator')),'date':scalar(meta.get('date')),'title':scalar(meta.get('title')),
        'source':{'provider':'archive.org','identifier':identifier,'url':'https://archive.org/details/' + identifier,'metadata_url':metadata_url,'acquired_at':utcnow(),'rights':{k:meta[k] for k in ('licenseurl','rights','publicdate') if k in meta}},
        'notes':downloaded,'audio_files':[{'path':f['name'],'format':f.get('format'),'size':int(f.get('size',0)),'md5':f.get('md5')} for f in audio],
        'reference_metadata':meta,'archive_metadata_sha256':digest(metadata_bytes),'complete':True}
    atomic_json(existing, package)
    return package


def discover(root, rows=2000):
    cache = root / 'discovery.json'
    if cache.exists():
        return json.loads(cache.read_text())
    docs = {}
    # Stratification provides historical coverage before per-artist balancing.
    for decade in range(1960, 2030, 10):
        query = f'collection:etree AND date:[{decade}-01-01T00:00:00Z TO {decade+9}-12-31T23:59:59Z]'
        params = {'q':query,'fl[]':['identifier','creator','date','downloads'],'sort[]':'downloads desc','rows':rows,'output':'json'}
        result = json.loads(fetch('https://archive.org/advancedsearch.php?' + urllib.parse.urlencode(params,doseq=True)))
        for doc in result['response']['docs']:
            docs[doc['identifier']] = doc
        print(json.dumps({'discovered_decade':decade,'returned':len(result['response']['docs'])}), flush=True)
    result = list(docs.values())
    atomic_json(cache,result)
    return result


def balanced_candidates(docs, artist_limit):
    buckets = collections.defaultdict(list)
    for doc in docs:
        artist = scalar(doc.get('creator')).strip()
        if artist:
            buckets[artist.casefold()].append(doc)
    rng = random.Random(20261103)
    for bucket in buckets.values():
        # Seeded ordering with early decades first within each artist.
        rng.shuffle(bucket)
        bucket.sort(key=lambda d:scalar(d.get('date'))[:3])
    artists = sorted(buckets)
    rng.shuffle(artists)
    ordered = []
    for i in range(artist_limit):
        for artist in artists:
            if i < len(buckets[artist]):
                ordered.append(buckets[artist][i])
    return ordered


def public_record(package):
    # Original text and free-form metadata never enter the Git-safe manifest.
    return {k:package[k] for k in ('schema_version','package_id','artist','date','source','notes','audio_files','archive_metadata_sha256','complete')}


def report(packages, failures, target):
    artists = collections.Counter(p['artist'] for p in packages)
    decades = collections.Counter((p['date'][:3] + '0s') if re.match(r'\d{4}',p['date']) else 'unknown' for p in packages)
    uploaders = collections.Counter(scalar(p['reference_metadata'].get('uploader')) for p in packages)
    formats = collections.Counter(PurePosixPath(f['path']).suffix.lower() for p in packages for f in p['audio_files'])
    encodings = collections.Counter(n['encoding'] for p in packages for n in p['notes'])
    features = collections.Counter()
    for p in packages:
        extensions = {PurePosixPath(a['path']).suffix.lower() for a in p['audio_files']}
        note_extensions = {PurePosixPath(n['path']).suffix.lower() for n in p['notes']}
        features['multiple-original-notes'] += len(p['notes']) > 1
        features['nested-original-audio-paths'] += any('/' in a['path'] for a in p['audio_files'])
        features['mixed-original-audio-formats'] += len(extensions) > 1
        features['non-flac-original-audio'] += extensions != {'.flac'}
        features['unsupported-original-audio'] += bool(extensions - {'.flac','.wav','.mp3','.ogg','.oga','.opus'})
        features['unsupported-note-name'] += bool(note_extensions - {'.txt','.nfo'})
        features['non-utf8-notes'] += any(n['encoding'] != 'utf-8-sig' for n in p['notes'])

    return {'schema_version':1,'generated_at':utcnow(),'target_packages':target,'complete_packages':len(packages),'target_met':len(packages)>=target,'artists':len(artists),'largest_artist_count':max(artists.values(),default=0),'largest_artist_fraction':max(artists.values(),default=0)/max(1,len(packages)),'artist_counts':dict(artists.most_common()),'decade_counts':dict(sorted(decades.items())),'uploader_count':len(uploaders),'largest_uploader_count':max(uploaders.values(),default=0),'audio_format_file_counts':dict(formats),'note_encoding_counts':dict(encodings),'package_feature_counts':dict(features),'original_note_files':sum(len(p['notes']) for p in packages),'note_bytes':sum(n['size'] for p in packages for n in p['notes']),'failed_attempts':len(failures),'failure_reasons':dict(collections.Counter(f['reason'] for f in failures)),'limitations':['Public availability and Archive metadata do not establish permission to redistribute original notes.','Original audio file inventory and metadata are acquired; audio bytes are never fetched.','Complete means every original text file with .txt/.text/.nfo/.md suffix plus original audio inventory; unrecognized document suffixes are not interpreted.','Discovery is downloads-ranked within decade strata and artist-balanced; it is not a random sample of all live recordings.']}


def normalize_packages(root, manifest=None):
    """Yield decoded candidate inputs. Reference metadata is never model input.

    All candidates receive UTF-8 text, while source encoding and byte hashes stay
    in provenance. The original package filenames are retained without filtering.
    """
    paths = sorted((root / 'packages').glob('*/package.json'))
    allowed = None
    if manifest is not None:
        allowed = {json.loads(line)['package_id'] for line in Path(manifest).read_text().splitlines() if line.strip()}
    for path in paths:
        package = json.loads(path.read_text())
        if allowed is not None and package['package_id'] not in allowed:
            continue
        notes = []
        for note in package['notes']:
            data = (path.parent / note['local_path']).read_bytes()
            if digest(data) != note['sha256']:
                raise ValueError('raw package hash changed: ' + package['package_id'])
            text, encoding = decode_note(data)
            notes.append({'name':note['path'],'text':text,'encoding':'utf-8','source_encoding':encoding})
        files = [f['path'] for f in package['audio_files']]
        flags = []
        if any(PurePosixPath(f).suffix.lower() != '.flac' for f in files):
            flags.append('non-flac-original-audio')
        if any(PurePosixPath(f).suffix.lower() not in {'.flac','.wav','.mp3','.ogg','.oga','.opus'} for f in files):
            flags.append('unsupported-original-audio')
        if any(PurePosixPath(n['name']).suffix.lower() not in {'.txt','.nfo'} for n in notes):
            flags.append('unsupported-note-name')
        if any(n['source_encoding'] not in {'utf-8-sig','cp1252'} for n in notes):
            flags.append('transcoded-source-encoding')
        yield {'id':package['package_id'],'notes':notes,'files':files,'oracle':None,'features':flags,
               'provenance':{'package_path':str(path),'artist':package['artist'],'date':package['date'],
                             'source':package['source'],'notes':package['notes'],
                             'archive_metadata_sha256':package['archive_metadata_sha256']}}


def group_packages(cases):
    """Merge alternate performances, exact notes and close note/template variants.

    Uses only acquisition data before any labels or predictions. Full-text and
    template word shingles are compared at >=0.85 and >=0.90 Jaccard respectively;
    Short notes require at least five shared shingles plus the same line structure;
    longer notes require at least 30 shared shingles.
    This conservative heuristic can miss paraphrased templates; frozen audits
    report its thresholds instead of claiming semantic deduplication.
    """
    parent = list(range(len(cases)))
    def find(i):
        while parent[i] != i:
            parent[i] = parent[parent[i]]
            i = parent[i]
        return i
    def union(a,b):
        a,b = find(a),find(b)
        if a != b:
            parent[max(a,b)] = min(a,b)
    keys = {}
    text_sets, template_sets, structures = [], [], []
    for index,case in enumerate(cases):
        provenance = case.get('provenance',{})
        aliases={re.sub(r'[^a-z0-9]+','',re.sub(r'^the\s+','',name.strip().casefold())) for name in re.split(r'[;|]',str(provenance.get('artist','')))}- {''}
        date = str(provenance.get('date',''))[:10]
        text = '\n'.join(n['text'] for n in sorted(case['notes'],key=lambda n:n['name']))
        normalized = re.sub(r'\s+',' ',text.casefold()).strip()
        structure = re.sub(r'[a-zA-Z]+','W',text)
        structure = re.sub(r'\d+','N',structure)
        structures.append(tuple(re.sub(r'\s+',' ',line).strip() for line in structure.splitlines() if line.strip()))
        own_keys = [('text',digest(normalized.encode()))]
        exact_date=False
        if re.fullmatch(r'\d{4}-\d{2}-\d{2}',date):
            try:datetime.date.fromisoformat(date);exact_date=True
            except ValueError:pass
        if exact_date:
            own_keys.extend(('performance-alias',alias,date) for alias in sorted(aliases))
        for key in own_keys:
            if key in keys:
                union(index,keys[key])
            keys[key] = index
        words = re.findall(r'[a-z]+',normalized)
        text_sets.append({tuple(words[i:i+5]) for i in range(len(words)-4)})
        # Track-list lines are replaced by a common marker, dates/numbers erased.
        template = re.sub(r'(?m)^\s*(?:[dD]\d+[tT]\d+|\d{1,3}[.) -])[^\n]*',' TRACK ',text)
        template = re.sub(r'\b\d+[\d:/.-]*\b',' NUM ',template.casefold())
        words = re.findall(r'[a-z]+',template)
        template_sets.append({tuple(words[i:i+5]) for i in range(len(words)-4)})
    for sets,threshold in ((text_sets,.85),(template_sets,.90)):
        inverted = collections.defaultdict(list)
        # Shared-shingle posting lists generate every potentially matching pair.
        for index,shingles in enumerate(sets):
            candidates = set()
            for shingle in shingles:
                candidates.update(inverted[shingle])
            for other in candidates:
                if find(index) == find(other):
                    continue
                previous = sets[other]
                minimum = 30
                if min(len(shingles),len(previous)) < 30:
                    if structures[index] != structures[other]:
                        continue
                    minimum = 5
                if min(len(shingles),len(previous)) < minimum or min(len(shingles),len(previous)) / max(len(shingles),len(previous)) < threshold:
                    continue
                shared = len(shingles & previous)
                if shared >= minimum and shared / len(shingles | previous) >= threshold:
                    union(index,other)
            for shingle in shingles:
                inverted[shingle].append(index)
    groups = collections.defaultdict(list)
    for index,case in enumerate(cases):
        groups[find(index)].append(case['id'])
    for index,case in enumerate(cases):
        members = sorted(groups[find(index)])
        case['group'] = 'acq-' + digest('\n'.join(members).encode())[:20]
    return cases


def audit_grouping(cases):
    """Git-safe post-freeze overlap audit using source text only, never labels."""
    groups = collections.Counter(c['group'] for c in cases)
    group_splits = collections.defaultdict(set)
    note_sets,template_sets = [],[]
    for case in cases:
        group_splits[case['group']].add(case.get('split','unfrozen'))
        text = '\n'.join(n['text'] for n in sorted(case['notes'],key=lambda n:n['name']))
        words = re.findall(r'[a-z]+',text.casefold())
        note_sets.append({tuple(words[i:i+5]) for i in range(len(words)-4)})
        template = re.sub(r'(?m)^\s*(?:[dD]\d+[tT]\d+|\d{1,3}[.) -])[^\n]*',' TRACK ',text)
        template = re.sub(r'\b\d+[\d:/.-]*\b',' NUM ',template.casefold())
        words = re.findall(r'[a-z]+',template)
        template_sets.append({tuple(words[i:i+5]) for i in range(len(words)-4)})
    def pairs(sets,threshold):
        best = {'jaccard':0,'left':None,'right':None,'shared_shingles':0}
        high = []
        for index,left in enumerate(cases):
            for other in range(index):
                right = cases[other]
                if left.get('split') == right.get('split'):
                    continue
                a,b = sets[index],sets[other]
                if not a or not b:
                    continue
                shared = len(a & b)
                similarity = shared / len(a | b)
                row = {'jaccard':similarity,'left':left['id'],'right':right['id'],'shared_shingles':shared}
                if similarity > best['jaccard']:
                    best = row
                if similarity >= threshold:
                    high.append(row)
        return best,high
    note_best,note_high = pairs(note_sets,.85)
    template_best,template_high = pairs(template_sets,.90)
    return {'schema_version':1,'packages':len(cases),'groups':len(groups),
            'largest_group':max(groups.values(),default=0),'group_size_counts':dict(collections.Counter(groups.values())),
            'cross_partition_group_violations':sum(len(s)>1 for s in group_splits.values()),
            'max_cross_partition_note_similarity':note_best,
            'cross_partition_note_pairs_at_least_0_85':note_high,
            'max_cross_partition_template_similarity':template_best,
            'cross_partition_template_pairs_at_least_0_90':template_high,
            'similarity_metric':'casefolded ASCII word five-shingle Jaccard',
            'limitations':['Text similarity is heuristic; paraphrases, short notes and non-Latin text may evade grouping.','Whole-group partitioning does not prove independent authorship or eliminate broad taper-style overlap.']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,default=Path('/Users/dan/code/track-tagger-experiment-data'))
    parser.add_argument('--target',type=int,default=1200)
    parser.add_argument('--artist-limit',type=int,default=30)
    parser.add_argument('--workers',type=int,default=8)
    parser.add_argument('--discovery-rows',type=int,default=2000)
    parser.add_argument('--hours',type=float,default=4)
    parser.add_argument('--manifest',type=Path,default=Path('experiments/parser/acquisition-manifest.jsonl'))
    parser.add_argument('--report',type=Path,default=Path('experiments/parser/acquisition-report.json'))
    args = parser.parse_args()
    if not 1 <= args.workers <= 16 or args.artist_limit > args.target * .05:
        parser.error('workers must be 1..16 and artist limit at most 5% of target')
    args.root.mkdir(parents=True,exist_ok=True)
    candidates = balanced_candidates(discover(args.root,args.discovery_rows),args.artist_limit)
    packages, failures = [], []
    counts = collections.Counter()
    deadline = time.monotonic() + args.hours * 3600
    # Batches bound outstanding work and make the stop condition deterministic.
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as executor:
        for start in range(0,len(candidates),args.workers):
            if len(packages) >= args.target or time.monotonic() >= deadline:
                break
            batch = candidates[start:start+min(args.workers,args.target-len(packages))]
            jobs = [(d,executor.submit(acquire,d['identifier'],args.root)) for d in batch]
            for doc,job in jobs:
                try:
                    package = job.result()
                    artist = package['artist'].casefold().strip()
                    if counts[artist] >= args.artist_limit:
                        continue
                    packages.append(package)
                    counts[artist] += 1
                except Exception as exc:
                    failures.append({'identifier':doc['identifier'],'reason':type(exc).__name__+': '+str(exc)})
            if start % (args.workers*5) == 0 or len(packages)>=args.target:
                print(json.dumps({'complete':len(packages),'artists':len(counts),'attempted':start+len(batch),'failures':len(failures)}),flush=True)
                atomic_json(args.root/'acquisition-progress.json',report(packages,failures,args.target))
    args.manifest.parent.mkdir(parents=True,exist_ok=True)
    args.manifest.write_text(''.join(json.dumps(public_record(p),ensure_ascii=False)+'\n' for p in sorted(packages,key=lambda p:p['package_id'])))
    atomic_json(args.report,report(packages,failures,args.target))
    atomic_json(args.root/'acquisition-failures.json',failures)
    print(json.dumps(report(packages,failures,args.target)),flush=True)


if __name__ == '__main__':
    main()
