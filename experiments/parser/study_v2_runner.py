"""Offline three-arm runner. Test dispatch requires source/model/execution freeze.

All build trees, raw outputs and source inputs stay outside Git. No media library,
network provider, or writer endpoint is used. The catalog adapter scans disposable
placeholder files only; assisted proposals never mutate catalog output.
"""
import argparse
import copy
import hashlib
import json
import math
import queue
import re
import subprocess
import tarfile
import tempfile
import threading
import time
from pathlib import Path

from .path_guard import require_external
from .study_v2_evaluation import (BASELINE_COMMIT, FIXED_COMMIT, FIELDS, digest,
                                  freeze_protocol, indexed)

ADAPTER_COMMIT = 'aa2fcb576f546de93ec574e766babf58c194a812'
MAX_SCORING_BYTES = 2 << 20
MAX_LINE_RUNES = 16384
HEADER_LIMIT = 8
# These are source-only eligibility rules, fixed before held-out predictions.
INELIGIBLE = re.compile(r'(?i)\b(?:ltp|last time played|previous(?:ly)?|formerly|personnel|guest|featur\w*|taper|lineage|transfer\w*|upload\w*|seeded|source|mastered|recorded by|taped by|mixed by|probably|possibly|perhaps|unsure|uncertain|unknown|provisional|working title)\b|\bw/|\bwith\b')
LABEL = re.compile(r'(?i)^(artist|performer|band|concert date|date|venue|city)\s*:\s*')
STRUCTURE = re.compile(r'(?i)^(?:set\s+(?:\d+|[ivx]+|one|two)|disc\s|disk\s|cd\s|track\s*list\b|set\s*list\b|source\s*:|lineage\s*:|transfer\b|personnel\s*:|recording\b|recordist\b|taper\b|mastering\b|notes\s*:|musicians\s*:|credits\s*:|checksums?\b|ffp\b|md5\b|\d{1,3}\s*[.)_-]\s*\S|d\d+t\d+)')

DATE_HELPER = '''
// ExperimentDate uses the pinned production date validator for source suggestions.
func ExperimentDate(text string) (string, []string) {
 r := Recording{}
 dates := map[string]bool{}
 parseDates(&r, text, dates)
 if len(dates) != 1 || len(r.ReviewReasons) != 0 { return "", r.ReviewReasons }
 for date := range dates { return date, nil }
 return "", r.ReviewReasons
}
'''
DATE_CLI = '''//go:build parser_experiment
package main
import("encoding/json";"fmt";"os";"track-tagger/internal/catalog")
func init(){
 if len(os.Args)!=2 || os.Args[1]!="--date" {return}
 var text string
 if err:=json.NewDecoder(os.Stdin).Decode(&text);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
 value,reasons:=catalog.ExperimentDate(text)
 if err:=json.NewEncoder(os.Stdout).Encode(map[string]any{"date":value,"review_reasons":reasons});err!=nil{os.Exit(1)}
 os.Exit(0)
}
'''


def file_sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def normalized(text):
    return text.removeprefix('\ufeff').replace('\r\n', '\n').replace('\r', '\n')


def source_lines(text):
    text = normalized(text)
    rows = text.split('\n')
    return rows[:-1] if text.endswith('\n') else rows


def source_hash(case):
    return digest({'notes': [{'name': n['name'], 'text': normalized(n['text'])} for n in case['notes']],
                   'files': case['files']})


def git_file(repo, commit, path):
    return subprocess.run(['git', '-C', str(repo), 'show', commit+':'+path],
                          capture_output=True, check=True).stdout


def build_catalogs(repo, destination):
    destination = require_external(destination)
    destination.mkdir(parents=True, exist_ok=True)
    manifest_path = destination/'build-manifest.json'
    if manifest_path.exists():
        manifest = json.loads(manifest_path.read_text())
        for row in manifest['binaries'].values():
            if file_sha(row['path']) != row['sha256']:
                raise ValueError('cached binary changed')
        if manifest['recipe_sha256'] != digest({'adapter': ADAPTER_COMMIT, 'date_helper': DATE_HELPER, 'date_cli': DATE_CLI}):
            raise ValueError('cached build recipe changed; choose a fresh build root')
        return manifest
    binaries = {}
    for name, commit in [('baseline', BASELINE_COMMIT), ('fixed', FIXED_COMMIT)]:
        work = Path(tempfile.mkdtemp(prefix=name+'-', dir=destination))
        archive = work/'source.tar'
        subprocess.run(['git', '-C', str(repo), 'archive', '--format=tar', '--output='+str(archive), commit], check=True)
        tree = work/'source'
        tree.mkdir()
        with tarfile.open(archive) as tar:
            for member in tar.getmembers():
                if Path(member.name).is_absolute() or '..' in Path(member.name).parts or not (member.isfile() or member.isdir()):
                    raise ValueError('unexpected archive member')
            tar.extractall(tree)
        adapter = tree/'experiments/parser/adapter'
        adapter.mkdir(parents=True)
        (adapter/'main.go').write_bytes(git_file(repo, ADAPTER_COMMIT, 'experiments/parser/adapter/main.go'))
        (adapter/'date.go').write_text(DATE_CLI)
        helper = git_file(repo, ADAPTER_COMMIT, 'internal/catalog/parser_experiment.go').decode()
        (tree/'internal/catalog/parser_experiment.go').write_text(helper+DATE_HELPER)
        binary = destination/('catalog-'+name)
        subprocess.run(['go', 'build', '-trimpath', '-tags', 'parser_experiment', '-o', str(binary), './experiments/parser/adapter'], cwd=tree, check=True)
        binaries[name] = {'application_commit': commit, 'path': str(binary), 'sha256': file_sha(binary)}
    manifest = {'schema_version': 1, 'binaries': binaries, 'adapter_source_commit': ADAPTER_COMMIT,
                'recipe_sha256': digest({'adapter': ADAPTER_COMMIT, 'date_helper': DATE_HELPER, 'date_cli': DATE_CLI}),
                'go_version': subprocess.run(['go', 'version'], text=True, capture_output=True, check=True).stdout.strip()}
    manifest_path.write_text(json.dumps(manifest, indent=2)+'\n')
    return manifest


def execution_manifest(build_path, scorer):
    builds = json.loads(Path(build_path).read_text())
    for name, commit in [('baseline', BASELINE_COMMIT), ('fixed', FIXED_COMMIT)]:
        b = builds['binaries'][name]
        if b['application_commit'] != commit or file_sha(b['path']) != b['sha256']:
            raise ValueError('catalog executable identity mismatch')
    return {'runner_sha256': file_sha(__file__), 'build_manifest_sha256': file_sha(build_path),
            'baseline_binary_sha256': builds['binaries']['baseline']['sha256'],
            'fixed_binary_sha256': builds['binaries']['fixed']['sha256'],
            'classifier_binary_sha256': file_sha(scorer)}


class Scorer:
    """One worker retains the passive model; one request/reply per source line."""
    def __init__(self, binary, model):
        self.process = subprocess.Popen([str(binary), '--model', str(model)], stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                        text=True, encoding='utf-8', bufsize=1)
        self.responses = queue.Queue()
        def reader():
            try:
                for row in self.process.stdout:
                    self.responses.put(row)
            finally:
                self.responses.put(None)
        self.thread = threading.Thread(target=reader, daemon=True)
        self.thread.start()
        started = time.perf_counter()
        try:
            self.score('', '', '')  # Force model load/validation before source inference.
        except Exception:
            self.close()
            raise
        self.startup_seconds = time.perf_counter()-started

    def score(self, line, previous, following):
        self.process.stdin.write(json.dumps({'line': line, 'prev': previous, 'next': following}, ensure_ascii=False)+'\n')
        self.process.stdin.flush()
        raw = self.responses.get(timeout=30)
        if raw is None:
            raise ValueError('classifier worker exited')
        result = json.loads(raw)
        if (not isinstance(result.get('roles'), list) or not all(isinstance(r, str) for r in result['roles'])
            or not isinstance(result.get('scores'), dict)
            or not all(isinstance(v, (int, float)) and math.isfinite(v) and 0 <= v <= 1 for v in result['scores'].values())):
            raise ValueError('malformed classifier output')
        return result

    def close(self):
        if self.process.poll() is None:
            try:
                self.process.stdin.close()
            except BrokenPipeError:
                pass
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        self.thread.join(timeout=5)
        self.process.stdout.close()


def catalog_run(binary, case):
    payload = {'id': case['id'], 'notes': [{'name': n['name'], 'text': normalized(n['text']), 'encoding': 'utf-8'} for n in case['notes']], 'files': case['files']}
    result = subprocess.run([str(binary)], input=json.dumps(payload, ensure_ascii=False)+'\n',
                            text=True, encoding='utf-8', capture_output=True, timeout=120, check=True)
    raw = json.loads(result.stdout)
    if raw.get('id') != case['id'] or type(raw.get('review')) is not bool:
        raise ValueError('malformed catalog response')
    metadata = {f: raw[f] for f in FIELDS}
    if any(not isinstance(v, str) for v in metadata.values()):
        raise ValueError('malformed catalog metadata')
    return {'metadata': metadata, 'ready': not raw['review'], 'status': raw['status'],
            'review_reasons': raw.get('review_reasons') or [], 'titles': raw['titles'], 'mappings': raw['mappings'],
            'write_plan': {'mode': 'research-preview-only', 'writes': []}}


def date_value(binary, value):
    result = subprocess.run([str(binary), '--date'], input=json.dumps(value)+'\n', text=True,
                            encoding='utf-8', capture_output=True, check=True, timeout=30)
    out = json.loads(result.stdout)
    if not isinstance(out.get('date'), str) or not isinstance(out.get('review_reasons'), (list, type(None))):
        raise ValueError('malformed date validator output')
    return out['date'] if not out['review_reasons'] else ''


def proposals(case, predictions, fixed_output, parse_date):
    """Select one unambiguous source-backed header proposal per metadata role.

    This function has no access to gold labels, source applicability gold flags,
    reviewer decisions, or Archive reference metadata.
    """
    if not fixed_output or fixed_output['ready']:
        return []
    by_line = {(p['note'], p['line_index']): p for p in predictions}
    found = {f: [] for f in FIELDS}
    for note in case['notes']:
        rows = source_lines(note['text'])
        nonempty = 0
        started = False
        for i, raw in enumerate(rows):
            value = raw.strip()
            if not value:
                if started:
                    break
                continue
            started = True
            nonempty += 1
            is_numeric_date = re.fullmatch(r'\d{1,2}[/-]\d{1,2}[/-]\d{4}', value) is not None
            if nonempty > HEADER_LIMIT or (STRUCTURE.search(value) and not is_numeric_date):
                break
            if INELIGIBLE.search(value):
                continue
            pred = by_line.get((note['name'], i), {})
            if pred.get('error') or pred.get('abstention') or len(pred.get('roles', [])) != 1:
                continue
            role = pred['roles'][0]
            if role not in FIELDS or any(mark in value for mark in (' @ ', ' > ', ' | ', '?')):
                continue
            label = LABEL.match(value)
            if label:
                declared = label[1].lower()
                declared = 'artist' if declared in ('performer', 'band') else 'date' if declared == 'concert date' else declared
                if declared != role:
                    continue
                value = value[label.end():].strip()
            elif ':' in value or '\t' in value:
                continue
            if not value:
                continue
            start = raw.find(value)
            end = start+len(value)
            if start < 0:
                continue
            proposed = parse_date(value) if role == 'date' else value
            if not proposed:
                continue
            found[role].append({'field': role, 'value': proposed, 'note': note['name'],
                                'note_sha256': hashlib.sha256(normalized(note['text']).encode()).hexdigest(),
                                'line_index': i, 'start': start, 'end': end, 'quote': value})
    result = []
    for role in FIELDS:
        candidates = found[role]
        if len({p['value'] for p in candidates}) == 1 and candidates[0]['value'] != fixed_output['metadata'][role]:
            result.append(candidates[0])
    return result


def validate_dispatch(cases, split, model_path, execution, protocol=None, recording_gold=None, line_gold=None):
    selected = [c for c in cases if c['split'] == split]
    by_id = indexed(selected)
    for case in selected:
        if case.get('source_input_sha256') != source_hash(case):
            raise ValueError('input source hash mismatch')
    if split == 'test':
        if protocol is None or recording_gold is None or line_gold is None:
            raise ValueError('reviewed source and execution freeze required before test dispatch')
        expected = freeze_protocol(protocol['sources'], recording_gold, line_gold, protocol['enabled_roles'],
                                   classifier_model_sha256=file_sha(model_path), execution_manifest=execution)
        if protocol != expected or set(by_id) != {s['id'] for s in protocol['sources']}:
            raise ValueError('test protocol/executable/input identity mismatch')
        for src in protocol['sources']:
            case = by_id[src['id']]
            if (src['source_input_sha256'] != source_hash(case) or src.get('files') != case['files']
                or src['notes'] != {n['name']: len(source_lines(n['text'])) for n in case['notes']}):
                raise ValueError('test source inventory mismatch')
        actual_lines = {(c['id'], n['name'], i): line for c in selected for n in c['notes']
                        for i, line in enumerate(source_lines(n['text']))}
        for row in line_gold:
            if actual_lines.get((row['id'], row['note'], row['line_index'])) != row['line']:
                raise ValueError('line gold text does not match frozen source bytes')
        return selected, digest(protocol)
    if split not in ('train', 'validation') or protocol is not None:
        raise ValueError('invalid development dispatch')
    return selected, digest({'mode': 'development-'+split, 'sources': [(c['id'], source_hash(c)) for c in selected],
                             'model_sha256': file_sha(model_path), 'execution_manifest': execution})


def run_cases(cases, build, scorer_binary, model, freeze_hash, output_root):
    output_root = require_external(output_root)
    output_root.mkdir(parents=True, exist_ok=True)
    model_hash = file_sha(model)
    rows = {name: [] for name in ('baseline', 'fixed', 'assisted')}
    predictions = []
    worker_failures = 0
    worker = Scorer(scorer_binary, model)
    try:
        for case in cases:
            cache_key = digest({'id': case['id'], 'source': source_hash(case), 'freeze': freeze_hash, 'runner': file_sha(__file__)})
            path = output_root/(cache_key+'.json')
            if path.exists():
                cached = json.loads(path.read_text())
            else:
                binding = {'id': case['id'], 'source_input_sha256': source_hash(case), 'freeze_sha256': freeze_hash}
                arm_rows = {}
                for name in ('baseline', 'fixed'):
                    started = time.perf_counter()
                    try:
                        arm_rows[name] = {**binding, 'output': catalog_run(build['binaries'][name]['path'], case)}
                    except (ValueError, KeyError, subprocess.SubprocessError) as exc:
                        arm_rows[name] = {**binding, 'error': type(exc).__name__}
                    arm_rows[name]['wall_seconds'] = time.perf_counter()-started
                arm_rows['assisted'] = copy.deepcopy(arm_rows['fixed'])
                line_rows = []
                started = time.perf_counter()
                oversized = sum(len(normalized(n['text']).encode()) for n in case['notes']) > MAX_SCORING_BYTES
                for note in case['notes']:
                    note_lines = source_lines(note['text'])
                    for i, line in enumerate(note_lines):
                        previous = note_lines[i-1] if i else ''
                        following = note_lines[i+1] if i+1 < len(note_lines) else ''
                        row = {**binding, 'model_sha256': model_hash, 'note': note['name'], 'line_index': i}
                        if oversized or any(len(s) > MAX_LINE_RUNES for s in (line, previous, following)):
                            row.update(roles=[], abstention='oversized_source', scores={})
                        elif worker_failures >= 3:
                            row.update(roles=[], error='classifier_circuit_open')
                        else:
                            try:
                                row.update(worker.score(line, previous, following))
                            except (ValueError, OSError, queue.Empty) as exc:
                                row.update(roles=[], error=type(exc).__name__)
                                worker_failures += 1
                                worker.close()
                                if worker_failures < 3:
                                    worker = Scorer(scorer_binary, model)
                        line_rows.append(row)
                try:
                    arm_rows['assisted']['proposals'] = proposals(case, line_rows, arm_rows['fixed'].get('output'),
                                                                  lambda value: date_value(build['binaries']['fixed']['path'], value))
                except (ValueError, subprocess.SubprocessError):
                    arm_rows['assisted']['proposals'] = []
                    arm_rows['assisted']['proposal_error'] = 'date_validation_failed'
                arm_rows['assisted']['classifier_wall_seconds'] = time.perf_counter()-started
                cached = {'arms': arm_rows, 'line_predictions': line_rows}
                # Atomic checkpoint; cache key includes source, execution freeze, and runner.
                temporary = path.with_suffix('.tmp')
                temporary.write_text(json.dumps(cached, ensure_ascii=False, allow_nan=False))
                temporary.replace(path)
            for name in rows:
                rows[name].append(cached['arms'][name])
            predictions.extend(cached['line_predictions'])
    finally:
        worker.close()
    arms = {name: {'application_commit': BASELINE_COMMIT if name == 'baseline' else FIXED_COMMIT,
                   **({'model_sha256': model_hash} if name == 'assisted' else {}), 'rows': values}
            for name, values in rows.items()}
    return arms, predictions


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    build = sub.add_parser('build')
    build.add_argument('--repo', required=True)
    build.add_argument('--output-root', required=True)
    manifest = sub.add_parser('execution-manifest')
    manifest.add_argument('--build-manifest', required=True)
    manifest.add_argument('--scorer', required=True)
    manifest.add_argument('--output', required=True)
    run = sub.add_parser('run')
    run.add_argument('--cases', required=True)
    run.add_argument('--split', choices=['train', 'validation', 'test'], required=True)
    run.add_argument('--build-manifest', required=True)
    run.add_argument('--scorer', required=True)
    run.add_argument('--model', required=True)
    run.add_argument('--output-root', required=True)
    run.add_argument('--protocol')
    run.add_argument('--recording-gold')
    run.add_argument('--line-gold')
    args = parser.parse_args()
    if args.command == 'build':
        print(json.dumps(build_catalogs(args.repo, args.output_root), indent=2))
        return
    execution = execution_manifest(args.build_manifest, args.scorer)
    if args.command == 'execution-manifest':
        with require_external(args.output).open('x') as output:
            json.dump(execution, output, indent=2)
        return
    def gold(path):
        return [json.loads(line) for line in Path(path).read_text().splitlines() if line.strip()] if path else None
    protocol = json.loads(Path(args.protocol).read_text()) if args.protocol else None
    cases = json.loads(Path(args.cases).read_text())['cases']
    selected, frozen = validate_dispatch(cases, args.split, args.model, execution, protocol,
                                         gold(args.recording_gold), gold(args.line_gold))
    if not selected:
        raise ValueError('empty selected cohort')
    output_root = require_external(args.output_root)
    output_root.mkdir(parents=True, exist_ok=True)
    prior_manifest = output_root/'run-manifest.json'
    if prior_manifest.exists() and json.loads(prior_manifest.read_text()).get('freeze_sha256') != frozen:
        raise ValueError('output root belongs to a different frozen run')
    arms, predictions = run_cases(selected, json.loads(Path(args.build_manifest).read_text()), args.scorer,
                                   args.model, frozen, output_root/'case-cache')
    (output_root/'arms.json').write_text(json.dumps(arms, ensure_ascii=False, allow_nan=False))
    (output_root/'line-predictions.jsonl').write_text(''.join(json.dumps(r, ensure_ascii=False, allow_nan=False)+'\n' for r in predictions))
    (output_root/'run-manifest.json').write_text(json.dumps({'mode': args.split, 'freeze_sha256': frozen,
        'execution_manifest': execution, 'requested': len(selected), 'model_sha256': file_sha(args.model),
        'timing_scope': 'Per-case classifier wall time includes feature/scoring IPC and proposal/date validation; persistent model load is outside per-case timing. Catalog timings include fresh process and disposable filesystem scan. Not per-recording CPU time.',
        'write_plan_scope': 'Research preview sentinel with zero writes; no app writer or apply operation is invoked.'}, indent=2))
    print(json.dumps({'completed': len(selected), 'mode': args.split, 'freeze_sha256': frozen}))


if __name__ == '__main__':
    main()
